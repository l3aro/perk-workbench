package conformance

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/l3aro/perk-workbench/internal/database/plugin"
)

// maxFrameBytes is the wire bound of one protocol frame, including its
// trailing newline — the same bound the production client enforces.
const maxFrameBytes = plugin.MaxFrameBytes

// killGrace bounds the shutdown of a child that ignores EOF: after the
// grace the process is killed and reaped.
const killGrace = 5 * time.Second

// Frame is one validated response frame from the child. Exactly one of
// Result and Error is set; HasID reports whether the frame carried an
// unsigned numeric id.
type Frame struct {
	ID     uint64
	HasID  bool
	Result json.RawMessage
	Error  *ErrorData
}

// ErrorData is the JSON-RPC error object of one response.
type ErrorData struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Child is one plugin child under test, spoken to as raw NDJSON-RPC on
// stdio. Frames are never routed through the production Client, so the
// runner can send deliberately invalid frames; responses are parsed
// strictly — UTF-8, a single JSON object per LF-terminated frame within
// maxFrameBytes, jsonrpc "2.0", exactly one result or error, an
// unsigned numeric id, no duplicate or unknown responses, no stdout
// noise — and any violation terminates the child. stderr is drained
// concurrently with the same bounded intent as the host client (newest
// 64 KiB / 100 lines), so a chatty child can never block on a full
// stderr pipe or grow diagnostics without limit.
type Child struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	drain  *stderrDrain

	writeMu     sync.Mutex // serializes stdin writes
	mu          sync.Mutex
	outstanding map[uint64]bool // request ids awaiting a response
	answered    map[uint64]bool // request ids already answered
	unanswered  int             // frames sent that the child must not answer with an id
	err         *CaseError      // terminal protocol/stream error, if any
	exitCode    int             // -1 while running or signal-killed
	closeErr    error           // shutdown failure, if the child survives the kill
	frames      chan Frame      // validated responses, in order
	done        chan struct{}   // reader loop finished
	reaped      chan struct{}   // process reaped
	stop        chan struct{}   // Close asks the reader to stop
	closeOnce   sync.Once
	inputOnce   sync.Once
}

// spawnChild launches one plugin child and its stdout reader, stderr
// drain, and reaper goroutines. The child is always reaped: the reader
// kills it on protocol violation, Close kills it when it ignores EOF.
func spawnChild(path string) (*Child, error) {
	cmd := exec.Command(path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	child := &Child{
		cmd:         cmd,
		stdin:       stdin,
		reader:      bufio.NewReaderSize(stdout, maxFrameBytes),
		drain:       newStderrDrain(),
		outstanding: map[uint64]bool{},
		answered:    map[uint64]bool{},
		exitCode:    -1,
		frames:      make(chan Frame, 32),
		done:        make(chan struct{}),
		reaped:      make(chan struct{}),
		stop:        make(chan struct{}),
	}
	go child.drain.run(stderr)
	go child.readLoop()
	go child.reap()
	return child, nil
}

// readLoop parses the response stream until EOF, a protocol violation,
// or Close. Violations kill the child immediately so the case can never
// observe a response that followed one.
func (c *Child) readLoop() {
	defer close(c.done)
	var readErr error
	for {
		line, err := c.readFrame()
		if err != nil {
			readErr = err
			break
		}
		frame, err := parseFrame(line)
		if err != nil {
			readErr = err
			break
		}
		if err := c.checkID(frame); err != nil {
			readErr = err
			break
		}
		select {
		case c.frames <- frame:
		case <-c.stop:
			return
		}
	}
	if readErr != nil {
		c.mu.Lock()
		if c.err == nil {
			message := readErr.Error()
			if errors.Is(readErr, io.EOF) {
				message = "child closed the response stream (EOF)"
			}
			c.err = &CaseError{Category: CategoryProtocol, Message: message}
		}
		c.mu.Unlock()
		if !errors.Is(readErr, io.EOF) {
			// Protocol violation: stop the child before the case can
			// observe anything further.
			_ = c.stdin.Close()
			_ = c.cmd.Process.Kill()
		}
	}
}

// reap waits for the child and records its exit status. Every child is
// reaped exactly once, by this goroutine, so a reader stuck on a full
// channel or a blocked read can never delay or lose the reap.
func (c *Child) reap() {
	waitErr := c.cmd.Wait()
	c.mu.Lock()
	c.exitCode = exitStatus(waitErr)
	c.mu.Unlock()
	close(c.reaped)
}

// exitStatus maps a cmd.Wait error to the child's exit status: the exit
// code for a normal exit (0 for a clean one), -1 while unknown (not yet
// reaped) or when the child was killed by a signal.
func exitStatus(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// readFrame reads one LF-terminated frame, bounding it at maxFrameBytes
// including the newline. A frame that does not fit is oversized; io.EOF
// with no partial data is a clean stream close; a partial line at EOF is
// an unterminated frame.
func (c *Child) readFrame() ([]byte, error) {
	line, err := c.reader.ReadSlice('\n')
	if err == bufio.ErrBufferFull {
		return nil, fmt.Errorf("oversized response frame: line exceeds %d bytes without a newline", maxFrameBytes)
	}
	if err != nil {
		if len(line) > 0 {
			return nil, errors.New("unterminated response frame at EOF")
		}
		return nil, err
	}
	if len(line) > maxFrameBytes {
		return nil, fmt.Errorf("oversized response frame: %d bytes, want at most %d including the newline", len(line), maxFrameBytes)
	}
	return line, nil
}

// parseFrame strictly validates one response frame: valid UTF-8, a
// single JSON object in one LF-terminated line, jsonrpc "2.0", exactly
// one of result or error, and an unsigned numeric id when one is
// carried. An error object must carry an integer code and a string
// message, and its optional data must be the perk/v1 provenance object
// (an object whose kind/plugin/method members, when present, are
// strings; null is valid). Error messages are structural — raw frame
// content is never reported.
func parseFrame(line []byte) (Frame, error) {
	if !utf8.Valid(line) {
		return Frame{}, errors.New("response frame is not valid UTF-8")
	}
	if len(line) < 2 || line[0] != '{' {
		return Frame{}, errors.New("response frame is not a single JSON object")
	}
	if line[len(line)-2] == '\r' {
		return Frame{}, errors.New("response frame must end with a single LF")
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	var obj map[string]any
	if err := decoder.Decode(&obj); err != nil {
		return Frame{}, fmt.Errorf("malformed response frame: %v", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Frame{}, errors.New("response frame carries trailing data")
	}
	jsonrpc, _ := obj["jsonrpc"].(string)
	if jsonrpc != "2.0" {
		return Frame{}, fmt.Errorf("response with jsonrpc %q, want \"2.0\"", jsonrpc)
	}
	_, hasResult := obj["result"]
	_, hasError := obj["error"]
	if hasResult == hasError {
		return Frame{}, errors.New("response frame must carry exactly one of result or error")
	}
	frame := Frame{}
	idValue, idPresent := obj["id"]
	if !idPresent {
		// A response always echoes the request id; a frame without an
		// id member at all is stdout noise, never a JSON-RPC reply
		// (invalid-request replies carry an explicit "id": null).
		return Frame{}, errors.New("response frame without an id member")
	}
	if idValue != nil {
		number, ok := idValue.(json.Number)
		if !ok {
			return Frame{}, errors.New("response id must be an unsigned integer")
		}
		parsed, err := strconv.ParseUint(string(number), 10, 64)
		if err != nil {
			return Frame{}, fmt.Errorf("response id %q is not an unsigned integer", number.String())
		}
		frame.ID = parsed
		frame.HasID = true
	}
	if hasError {
		errObj, ok := obj["error"].(map[string]any)
		if !ok {
			return Frame{}, errors.New("response error must be a JSON-RPC error object")
		}
		codeNumber, ok := errObj["code"].(json.Number)
		if !ok {
			return Frame{}, errors.New("response error is missing an integer code")
		}
		code, err := strconv.Atoi(codeNumber.String())
		if err != nil {
			return Frame{}, fmt.Errorf("response error code %q is not an integer", codeNumber.String())
		}
		message, ok := errObj["message"].(string)
		if !ok {
			return Frame{}, errors.New("response error is missing a string message")
		}
		// The optional data object mirrors the perk/v1 provenance
		// shape: an object whose kind/plugin/method members — and the
		// optional hint/suggested_statement advisory strings — are
		// strings when present. Null is valid (the canonical
		// error-null-data frame); scalars, arrays, and non-string
		// members are not.
		var data json.RawMessage
		if dataValue, present := errObj["data"]; present && dataValue != nil {
			dataObject, ok := dataValue.(map[string]any)
			if !ok {
				return Frame{}, errors.New("response error data must be an object")
			}
			for _, key := range []string{"kind", "plugin", "method", "hint", "suggested_statement"} {
				if value, present := dataObject[key]; present && value != nil {
					if _, ok := value.(string); !ok {
						return Frame{}, fmt.Errorf("response error data %q must be a string", key)
					}
				}
			}
			raw, err := json.Marshal(dataValue)
			if err != nil {
				return Frame{}, fmt.Errorf("malformed response error data: %v", err)
			}
			data = raw
		}
		frame.Error = &ErrorData{Code: code, Message: message, Data: data}
	} else {
		frame.Result, _ = json.Marshal(obj["result"])
	}
	return frame, nil
}

// checkID enforces the response-id rules: every response with an id
// must answer an outstanding request exactly once; an id-less response
// is acceptable only as an error reply to a frame the child could not
// match (JSON-RPC invalid-request replies carry id null).
func (c *Child) checkID(frame Frame) error {
	if !frame.HasID {
		c.mu.Lock()
		unanswered := c.unanswered > 0
		c.mu.Unlock()
		if !unanswered || frame.Error == nil {
			return errors.New("response frame without a request id")
		}
		return nil
	}
	c.mu.Lock()
	switch {
	case c.answered[frame.ID]:
		c.mu.Unlock()
		return fmt.Errorf("duplicate response for request id %d", frame.ID)
	case !c.outstanding[frame.ID]:
		c.mu.Unlock()
		return fmt.Errorf("response for unknown request id %d", frame.ID)
	}
	delete(c.outstanding, frame.ID)
	c.answered[frame.ID] = true
	c.mu.Unlock()
	return nil
}

// SendFixture delivers one canonical fixture frame, registering its
// unsigned numeric id when it carries one so its response is expected.
// Frames without a parseable id — notifications and deliberately
// invalid requests — are sent unanswered.
func (c *Child) SendFixture(frame []byte) error {
	if id, ok := frameID(frame); ok {
		c.mu.Lock()
		c.outstanding[id] = true
		c.mu.Unlock()
	} else {
		c.mu.Lock()
		c.unanswered++
		c.mu.Unlock()
	}
	return c.write(frame)
}

// SendBatch delivers several request frames in one write, registering
// every id first so responses are expected regardless of arrival order.
func (c *Child) SendBatch(frames ...[]byte) error {
	c.mu.Lock()
	for _, frame := range frames {
		if id, ok := frameID(frame); ok {
			c.outstanding[id] = true
		} else {
			c.unanswered++
		}
	}
	c.mu.Unlock()
	buffer := make([]byte, 0, len(frames[0])+len(frames[1])+2)
	for _, frame := range frames {
		buffer = append(buffer, frame...)
	}
	return c.write(buffer)
}

// SendRaw writes one raw frame without registering any response —
// deliberately invalid frames and notifications.
func (c *Child) SendRaw(frame []byte) error {
	c.mu.Lock()
	c.unanswered++
	c.mu.Unlock()
	return c.write(frame)
}

// write delivers one complete frame to the child's stdin under the
// write mutex, so concurrent sends never interleave.
func (c *Child) write(frame []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	n, err := c.stdin.Write(frame)
	if err != nil {
		return &CaseError{Category: CategoryProtocol, Message: fmt.Sprintf("writing to the child: %v", err)}
	}
	if n != len(frame) {
		return &CaseError{Category: CategoryProtocol, Message: "short write to the child"}
	}
	return nil
}

// Expect returns the next validated response frame, failing when the
// child terminates first or the deadline passes.
func (c *Child) Expect(until time.Time) (Frame, error) {
	timer := time.NewTimer(time.Until(until))
	defer timer.Stop()
	select {
	case frame := <-c.frames:
		return frame, nil
	case <-c.done:
		// The reader finished; frames it delivered before that are
		// still buffered and take precedence over the terminal error.
		select {
		case frame := <-c.frames:
			return frame, nil
		default:
			return Frame{}, c.terminalError()
		}
	case <-timer.C:
		return Frame{}, &CaseError{Category: CategoryTimeout, Message: "no response within the case bound"}
	}
}

// terminalError returns the reader's terminal error, or a generic
// stream-closed error when none was recorded.
func (c *Child) terminalError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return &CaseError{Category: CategoryProtocol, Message: "child closed the response stream"}
}

// ExpectQuiet requires silence after a case's expected exchanges: any
// response frame or child termination within quiet fails. This catches
// duplicate responses, fabricated responses, and premature exit.
func (c *Child) ExpectQuiet(until time.Time, quiet time.Duration) error {
	if wait := time.Until(until); wait < quiet {
		quiet = wait
	}
	if quiet <= 0 {
		return nil
	}
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	select {
	case frame := <-c.frames:
		return &CaseError{Category: CategoryBehavior, Message: fmt.Sprintf("unexpected response for request id %d", frame.ID)}
	case <-c.done:
		return c.terminalError()
	case <-timer.C:
		return nil
	}
}

// ExpectSilent requires no response frame within quiet; child
// termination is expected (deliberately invalid input) and passes.
func (c *Child) ExpectSilent(until time.Time, quiet time.Duration) error {
	if wait := time.Until(until); wait < quiet {
		quiet = wait
	}
	if quiet <= 0 {
		return nil
	}
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	select {
	case frame := <-c.frames:
		return &CaseError{Category: CategoryBehavior, Message: fmt.Sprintf("unexpected response for request id %d", frame.ID)}
	case <-timer.C:
		return nil
	}
}

// ExpectExit requires the child to terminate (be reaped) within the
// bound, and fails when it answers after its expectations were
// complete.
func (c *Child) ExpectExit(until time.Time) error {
	timer := time.NewTimer(time.Until(until))
	defer timer.Stop()
	select {
	case <-c.reaped:
		return c.drainFrames()
	case <-timer.C:
		return &CaseError{Category: CategoryProtocol, Message: "child did not terminate within the case bound"}
	}
}

// drainFrames fails when any response frame is still buffered — the
// child answered after the case's expectations were complete.
func (c *Child) drainFrames() error {
	for {
		select {
		case frame := <-c.frames:
			return &CaseError{Category: CategoryBehavior, Message: fmt.Sprintf("unexpected response for request id %d", frame.ID)}
		default:
			return nil
		}
	}
}

// CloseInput closes the child's stdin (EOF) mid-case; the child is
// expected to terminate on its own.
func (c *Child) CloseInput() error {
	c.inputOnce.Do(func() { _ = c.stdin.Close() })
	return nil
}

// Close shuts the child down and reaps it: stdin closes (EOF), the
// process gets up to killGrace to exit, then is killed. Idempotent; a
// child is never left running or unreaped. Close returns an error only
// when the child survives the kill and cannot be reaped.
func (c *Child) Close() error {
	c.closeOnce.Do(func() {
		close(c.stop)
		_ = c.stdin.Close()
		select {
		case <-c.reaped:
		case <-time.After(killGrace):
			_ = c.cmd.Process.Kill()
			select {
			case <-c.reaped:
			case <-time.After(killGrace):
				c.mu.Lock()
				c.closeErr = errors.New("child survived the kill; process not reaped")
				c.mu.Unlock()
			}
		}
		// The drain reads a pipe the child's death closes, so it ends
		// shortly after the reap; wait for its final tail.
		select {
		case <-c.drain.done:
		case <-time.After(killGrace):
		}
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}

// ExitStatus returns the child's exit code once reaped; -1 while
// running or signal-killed.
func (c *Child) ExitStatus() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitCode
}

// stderrTail returns the bounded sanitized stderr tail, oldest first.
func (c *Child) stderrTail() []string {
	return c.drain.snapshot()
}

// Bounds for retained stderr diagnostics: the newest maxStderrBytes
// total and maxStderrLines logical lines per child, the same bounded
// intent as the host client, so a chatty child can never block on a
// full stderr pipe or grow diagnostics without limit. A single line
// larger than the byte budget keeps only its newest tail.
const (
	maxStderrBytes = 64 << 10
	maxStderrLines = 100
)

// stderrDrain consumes one child's stderr for its whole life,
// retaining only the newest bounded tail. Lines are the logical unit:
// arbitrary write chunk boundaries never split or duplicate them, and
// an unterminated or oversized line is bounded to the byte budget.
//
// Storage is deterministic and fixed-bound: a ring of exactly
// maxStderrLines slots for complete lines plus one open line whose
// backing allocation never exceeds the byte budget (growth is always an
// exact-size merge, never geometric append), so retained capacity —
// including every backing allocation — stays within the bounds.
//
// Retained text is stored sanitized to valid UTF-8: sanitization
// expansion is accounted into the byte budget at store time, and the
// snapshot re-enforces the bounds on the sanitized form, so the exposed
// tail never exceeds 64 KiB or 100 lines and is always rune-aligned.
type stderrDrain struct {
	mu    sync.Mutex
	buf   [maxStderrLines][]byte // complete lines, oldest at head
	head  int
	count int
	open  []byte // current unterminated line (raw, may split a rune)
	total int    // retained bytes: complete lines plus open
	done  chan struct{}
}

func newStderrDrain() *stderrDrain {
	return &stderrDrain{done: make(chan struct{})}
}

// run drains r until EOF or a read error. It is the stderr goroutine of
// one child and exits with the stream.
func (d *stderrDrain) run(r io.Reader) {
	defer close(d.done)
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
// The open line's bytes are already counted in total; sanitization
// expansion is the only adjustment, so the tail is never double-counted
// or trimmed away for it.
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
// line count is full. The line's bytes are already accounted in total
// by the caller; the dropped slot releases its backing.
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
// sync with the stored text, so sanitization expansion is bounded by
// the byte budget like any other retention growth.
func (d *stderrDrain) sanitizeLocked(line []byte) []byte {
	clean := sanitizeTail(line)
	d.total += len(clean) - len(line)
	return clean
}

// snapshot returns a fresh copy of the retained stderr tail, oldest
// first, including a trailing unterminated line when present. Every
// returned line is valid UTF-8, and the whole tail stays within the
// byte and line bounds even after sanitization expansion: oldest lines
// drop first, and the newest tail is capped to a rune boundary.
func (d *stderrDrain) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	lines := make([][]byte, 0, d.count+1)
	for i := range d.count {
		lines = append(lines, d.buf[(d.head+i)%maxStderrLines])
	}
	if len(d.open) > 0 {
		lines = append(lines, sanitizeTail(d.open))
	}
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	// Sanitization can expand invalid UTF-8 up to threefold, so the
	// sanitized form — not the raw accounting — is what the exposed
	// bounds must hold. Drop oldest lines first, then cap the newest
	// line's head; every boundary is a rune boundary.
	for len(lines) > 0 && (total > maxStderrBytes || len(lines) > maxStderrLines) {
		total -= len(lines[0])
		lines = lines[1:]
	}
	if total > maxStderrBytes && len(lines) > 0 {
		newest := lines[len(lines)-1]
		drop := total - maxStderrBytes
		if drop > len(newest) {
			drop = len(newest)
		}
		start := drop
		for start < len(newest) && !utf8.RuneStart(newest[start]) {
			start++
		}
		total -= start
		lines[len(lines)-1] = newest[start:]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, string(line))
	}
	return out
}
