package plugin

import (
	"bufio"
	"bytes"
	"io"
	"sync"
	"time"
	"unicode/utf8"
)

// Bounds for retained plugin stderr diagnostics: the newest
// maxStderrBytes total and maxStderrLines logical lines per child, so a
// chatty or crashing plugin can never make diagnostics storage grow
// without limit. A single line larger than the byte budget keeps only
// its newest tail.
const (
	maxStderrBytes = 64 << 10
	maxStderrLines = 100
)

// stderrDrain consumes one plugin child's stderr for its whole life,
// retaining only the newest bounded tail. The child can never block on
// a full stderr pipe, and the retained tail stays within both bounds —
// including every backing allocation, which never exceeds the byte
// budget. Lines are the logical unit: arbitrary write chunk boundaries
// never split or duplicate them, and an unterminated or oversized line
// is bounded to the byte budget. Retained text is valid UTF-8 — invalid
// sequences are replaced with U+FFFD, never dropped or fatal.
type stderrDrain struct {
	mu    sync.Mutex
	buf   [maxStderrLines][]byte // complete lines, oldest at head
	head  int
	count int
	open  []byte // current unterminated line (raw, may split a rune)
	total int    // retained bytes: complete lines plus open
}

// run drains r until EOF or a read error. It is the stderr goroutine of
// one client and exits with the stream.
func (d *stderrDrain) run(r io.Reader) {
	reader := bufio.NewReader(r)
	for {
		chunk, err := reader.ReadSlice('\n')
		if err == nil {
			d.addLine(chunk) // chunk ends with the newline
			continue
		}
		if err == bufio.ErrBufferFull {
			d.append(chunk) // oversized line: keep consuming its tail
			continue
		}
		if len(chunk) > 0 {
			d.append(chunk) // final partial line before EOF
		}
		d.finish() // retain a trailing unterminated line, if any
		return
	}
}

// addLine appends a newline-terminated chunk and retains the completed
// line in one critical section, so a concurrent snapshot can never
// observe the delimiter.
func (d *stderrDrain) addLine(chunk []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.appendLocked(chunk)
	line := d.open
	d.open = nil
	d.total-- // the delimiter is not stored
	d.pushLine(d.sanitizeLocked(line[:len(line)-1]))
	d.trimLocked()
}

// append adds raw bytes to the open line, enforcing both retention
// bounds; the open line's backing allocation never exceeds the byte
// budget. Chunks that cannot fit drop the oldest bytes first, so only
// the newest survive.
func (d *stderrDrain) append(chunk []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.appendLocked(chunk)
	d.trimLinesLocked()
}

// appendLocked grows the open line within the byte budget without ever
// letting its backing allocation exceed it: oldest complete lines drop
// first, then the open line's head, then the chunk's head, and the
// array is resized to exactly what the newest bytes need. Callers hold
// d.mu.
func (d *stderrDrain) appendLocked(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	for d.count > 0 && d.total+len(chunk) > maxStderrBytes {
		d.total -= len(d.buf[d.head])
		d.buf[d.head] = nil
		d.head = (d.head + 1) % maxStderrLines
		d.count--
	}
	if len(d.open)+len(chunk) > maxStderrBytes {
		drop := len(d.open) + len(chunk) - maxStderrBytes
		if drop > len(d.open) {
			drop = len(d.open)
		}
		d.open = d.open[drop:]
		d.total -= drop
	}
	if fit := maxStderrBytes - d.total; fit < len(chunk) {
		chunk = chunk[len(chunk)-fit:]
	}
	if len(chunk) == 0 {
		return
	}
	need := len(d.open) + len(chunk)
	if cap(d.open) < need {
		merged := make([]byte, need)
		copy(merged, d.open)
		copy(merged[len(d.open):], chunk)
		d.open = merged
	} else {
		d.open = append(d.open, chunk...)
	}
	d.total += len(chunk)
}

// finish retains a trailing unterminated line at EOF or read error.
func (d *stderrDrain) finish() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.open) == 0 {
		return
	}
	line := d.open
	d.open = nil
	d.pushLine(d.sanitizeLocked(line))
	d.trimLocked()
}

// pushLine retains one complete line, dropping the oldest when the
// line count is full. The line's bytes are already counted in total.
func (d *stderrDrain) pushLine(line []byte) {
	if d.count == maxStderrLines {
		d.total -= len(d.buf[d.head])
		d.buf[d.head] = nil
		d.head = (d.head + 1) % maxStderrLines
		d.count--
	}
	d.buf[(d.head+d.count)%maxStderrLines] = line
	d.count++
}

// trimLocked enforces both retention bounds after every mutation: the
// newest maxStderrBytes and newest maxStderrLines (the open line counts
// as one logical line).
func (d *stderrDrain) trimLocked() {
	for d.total > maxStderrBytes && d.count > 0 {
		d.total -= len(d.buf[d.head])
		d.buf[d.head] = nil
		d.head = (d.head + 1) % maxStderrLines
		d.count--
	}
	if d.total > maxStderrBytes && len(d.open) > 0 {
		excess := d.total - maxStderrBytes
		d.open = d.open[excess:]
		d.total -= excess
	}
	d.trimLinesLocked()
}

// trimLinesLocked enforces the line-count bound; the open line counts
// as one logical line.
func (d *stderrDrain) trimLinesLocked() {
	limit := maxStderrLines
	if len(d.open) > 0 {
		limit = maxStderrLines - 1
	}
	for d.count > limit {
		d.total -= len(d.buf[d.head])
		d.buf[d.head] = nil
		d.head = (d.head + 1) % maxStderrLines
		d.count--
	}
}

// sanitize replaces each run of invalid UTF-8 bytes in line with
// U+FFFD; valid input is returned unchanged. Lines are sanitized only
// when complete, so a rune split across write chunks survives.
func sanitize(line []byte) []byte {
	if utf8.Valid(line) {
		return line
	}
	return bytes.ToValidUTF8(line, []byte("\uFFFD"))
}

// sanitizeTail replaces runs of invalid UTF-8 bytes in line with
// U+FFFD and caps the result at the byte budget, keeping the newest
// tail aligned to a rune boundary so the result is always valid UTF-8.
// The returned slice never exceeds the budget in length or backing
// capacity (the replacement preallocation can overshoot, so the capped
// tail is copied into an exact-size array). Valid uncapped input is
// returned unchanged.
func sanitizeTail(line []byte) []byte {
	clean := sanitize(line)
	if len(clean) > maxStderrBytes {
		start := len(clean) - maxStderrBytes
		for start < len(clean) && !utf8.RuneStart(clean[start]) {
			start++
		}
		clean = clean[start:]
	}
	if cap(clean) > maxStderrBytes {
		exact := make([]byte, len(clean))
		copy(exact, clean)
		clean = exact
	}
	return clean
}

// sanitizeLocked sanitizes a line about to be stored and keeps total in
// sync with the stored text.
func (d *stderrDrain) sanitizeLocked(line []byte) []byte {
	clean := sanitizeTail(line)
	d.total += len(clean) - len(line)
	return clean
}

// snapshot returns a fresh copy of the retained stderr tail, oldest
// first, including a trailing unterminated line when present. Every
// returned line is valid UTF-8 and within the byte budget.
func (d *stderrDrain) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	lines := make([]string, 0, d.count+1)
	for i := range d.count {
		lines = append(lines, string(d.buf[(d.head+i)%maxStderrLines]))
	}
	if len(d.open) > 0 {
		lines = append(lines, string(sanitizeTail(d.open)))
	}
	return lines
}

// Snapshot is an immutable point-in-time view of one plugin child's
// diagnostics, suitable for CLI inspection. Snapshot() returns fresh
// copies; mutating the returned Stderr slice never affects the client.
// A snapshot exposes process and diagnostics state only — never
// protocol traffic: stdin/stdout frames, connection targets, form
// values, credentials, and statements are not retained anywhere.
type Snapshot struct {
	Path         string        `json:"path"`          // canonical executable path
	PID          int           `json:"pid"`           // child pid; 0 once reaped
	Plugin       string        `json:"plugin"`        // self-claimed name after the handshake
	InitDuration time.Duration `json:"init_duration"` // initialize RPC duration once Load completes
	InFlight     int           `json:"in_flight"`     // pending requests at snapshot time
	Error        string        `json:"error"`         // terminal protocol/process error text when present
	ExitStatus   int           `json:"exit_status"`   // exit code once reaped; -1 while running or signal-killed
	Running      bool          `json:"running"`       // child process not yet reaped
	Stderr       []string      `json:"stderr"`        // newest bounded diagnostics lines/tail
}

// Snapshot returns an immutable copy of the client's diagnostics. It
// never blocks on protocol I/O; the stderr tail is copied under the
// drain lock only.
func (c *Client) Snapshot() Snapshot {
	c.mu.Lock()
	snap := Snapshot{
		Path:         c.path,
		PID:          c.pid,
		Plugin:       c.plugin,
		InitDuration: c.initDuration,
		InFlight:     len(c.pending),
		ExitStatus:   c.exitStatus,
		Running:      c.running,
	}
	if c.err != nil {
		snap.Error = c.err.Error()
	}
	c.mu.Unlock()
	snap.Stderr = c.drain.snapshot()
	return snap
}

// setInitDuration records how long the Loader's initialize handshake
// took, on success or failure. The Loader calls it once per client,
// after the initialize call returns.
func (c *Client) setInitDuration(d time.Duration) {
	c.mu.Lock()
	c.initDuration = d
	c.mu.Unlock()
}
