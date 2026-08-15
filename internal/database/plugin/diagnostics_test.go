package plugin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// chunkedReader returns its payload in chunks of at most chunk bytes,
// exercising arbitrary write-boundary handling in the drain.
type chunkedReader struct {
	payload []byte
	chunk   int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if len(r.payload) == 0 {
		return 0, io.EOF
	}
	n := r.chunk
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.payload) {
		n = len(r.payload)
	}
	copy(p, r.payload[:n])
	r.payload = r.payload[n:]
	return n, nil
}

// drainLines feeds payload through a fresh drain in chunks of at most
// chunk bytes and returns the retained snapshot.
func drainLines(t *testing.T, payload string, chunk int) []string {
	t.Helper()
	drain := &stderrDrain{}
	drain.run(&chunkedReader{payload: []byte(payload), chunk: chunk})
	return drain.snapshot()
}

// TestDiagnostics_chunkedPartialLines: lines split at arbitrary write
// boundaries — every byte its own chunk — are reassembled exactly,
// never corrupted or duplicated, including a trailing unterminated
// line.
func TestDiagnostics_chunkedPartialLines(t *testing.T) {
	lines := drainLines(t, "one\ntwo\nthree\nfour", 1)
	want := []string{"one", "two", "three", "four"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines = %q, want %q", lines, want)
		}
	}
}

// TestDiagnostics_lineCountBound: only the newest 100 lines of a longer
// stream survive.
func TestDiagnostics_lineCountBound(t *testing.T) {
	var payload strings.Builder
	for i := range 150 {
		fmt.Fprintf(&payload, "line %d\n", i)
	}
	lines := drainLines(t, payload.String(), 4096)
	if len(lines) != maxStderrLines {
		t.Fatalf("retained %d lines, want %d", len(lines), maxStderrLines)
	}
	if lines[0] != "line 50" || lines[len(lines)-1] != "line 149" {
		t.Fatalf("first/last = %q/%q, want the newest 100 lines", lines[0], lines[len(lines)-1])
	}
}

// TestDiagnostics_byteBound: when 100 lines exceed the byte budget, the
// oldest lines drop until only the newest 64 KiB remain.
func TestDiagnostics_byteBound(t *testing.T) {
	const perLine = 701 // 4-digit index + ':' + 695 x's + newline
	var payload strings.Builder
	for i := range 100 {
		fmt.Fprintf(&payload, "%04d:%s\n", i, strings.Repeat("x", 695))
	}
	lines := drainLines(t, payload.String(), 4096)
	want := maxStderrBytes / perLine // 93 newest lines fit, 94 would not
	if len(lines) != want {
		t.Fatalf("retained %d lines, want %d", len(lines), want)
	}
	if lines[0] != fmt.Sprintf("%04d:%s", 100-want, strings.Repeat("x", 695)) {
		t.Fatalf("first retained line = %q, want line %d", lines[0], 100-want)
	}
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	if total > maxStderrBytes {
		t.Fatalf("retained %d bytes, want ≤ %d", total, maxStderrBytes)
	}
}

// TestDiagnostics_hugeUnterminatedLine: one oversized line without a
// newline is bounded to the newest 64 KiB tail.
func TestDiagnostics_hugeUnterminatedLine(t *testing.T) {
	const size = 200 << 10
	payload := strings.Repeat("x", size)
	lines := drainLines(t, payload, 4096)
	if len(lines) != 1 {
		t.Fatalf("retained %d lines, want 1", len(lines))
	}
	if len(lines[0]) != maxStderrBytes {
		t.Fatalf("line length = %d, want %d", len(lines[0]), maxStderrBytes)
	}
	if lines[0] != payload[size-maxStderrBytes:] {
		t.Fatal("retained tail is not the newest 64 KiB")
	}
}

// TestDiagnostics_hugeLineThenTail: an oversized line that later
// terminates is dropped whole when the budget must make room for newer
// lines.
func TestDiagnostics_hugeLineThenTail(t *testing.T) {
	payload := strings.Repeat("x", 200<<10) + "\ntail\n"
	lines := drainLines(t, payload, 4096)
	if len(lines) != 1 || lines[0] != "tail" {
		t.Fatalf("lines = %q, want [tail]", lines)
	}
}

// TestDiagnostics_invalidUTF8: each run of invalid bytes is replaced
// with U+FFFD, and a valid rune split across write chunks survives
// intact.
func TestDiagnostics_invalidUTF8(t *testing.T) {
	lines := drainLines(t, "ok\n\xff\xfe bad\n\xc3\xa9\nend\xff", 1)
	want := []string{"ok", "\ufffd bad", "é", "end\ufffd"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines = %q, want %q", lines, want)
		}
	}
}

// TestDiagnostics_invalidUTF8ExpansionCapped: replacing invalid bytes
// can triple a line's size; the stored result never exceeds the byte
// budget, stays valid UTF-8, and keeps the tail aligned to a rune
// boundary.
func TestDiagnostics_invalidUTF8ExpansionCapped(t *testing.T) {
	// Each raw unit "\xffa\xfe" sanitizes to "\ufffda\ufffd" (7 bytes),
	// so the 64 KiB tail starts mid-rune and must be realigned.
	lines := drainLines(t, strings.Repeat("\xffa\xfe", 30000)+"\n", 4096)
	if len(lines) != 1 {
		t.Fatalf("retained %d lines, want 1", len(lines))
	}
	if len(lines[0]) > maxStderrBytes || len(lines[0]) < maxStderrBytes-7 {
		t.Fatalf("line length = %d, want the %d cap aligned to a rune", len(lines[0]), maxStderrBytes)
	}
	if !utf8.ValidString(lines[0]) {
		t.Fatal("retained line is not valid UTF-8")
	}
	if !strings.HasSuffix(lines[0], "\ufffd") {
		t.Fatal("retained tail does not end with the newest rune")
	}
}

// TestDiagnostics_manyShortLinesAccounting: tens of thousands of short
// lines keep the byte accounting exact — the retained tail is the
// newest 100 logical lines (the trailing unterminated line counts as
// one) and the head trim never panics.
func TestDiagnostics_manyShortLinesAccounting(t *testing.T) {
	var payload strings.Builder
	for range 100_000 {
		payload.WriteString("x\n")
	}
	payload.WriteString(strings.Repeat("y", 40<<10))
	lines := drainLines(t, payload.String(), 4096)
	if len(lines) != maxStderrLines {
		t.Fatalf("retained %d lines, want %d", len(lines), maxStderrLines)
	}
	if lines[0] != "x" || len(lines[len(lines)-1]) != 40<<10 {
		t.Fatalf("first/last = %q/%d, want x and the 40 KiB open tail", lines[0], len(lines[len(lines)-1]))
	}
}

// TestDiagnostics_snapshotOpenTailBounded: snapshots taken while an
// oversized unterminated line with expanding invalid bytes streams in
// never expose more than the byte budget, and every line is valid
// UTF-8.
func TestDiagnostics_snapshotOpenTailBounded(t *testing.T) {
	pr, pw := io.Pipe()
	drain := &stderrDrain{}
	reaped := make(chan struct{})
	go func() {
		drain.run(pr)
		close(reaped)
	}()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer pw.Close()
		payload := []byte(strings.Repeat("\xffa\xfe", 30000))
		for len(payload) > 0 {
			n := 4096
			if n > len(payload) {
				n = len(payload)
			}
			if _, err := pw.Write(payload[:n]); err != nil {
				return
			}
			payload = payload[n:]
		}
	}()

	check := func(lines []string) {
		if len(lines) > 1 {
			t.Errorf("snapshot has %d lines during one unterminated line", len(lines))
		}
		for _, line := range lines {
			if len(line) > maxStderrBytes {
				t.Errorf("snapshot line holds %d bytes, bound is %d", len(line), maxStderrBytes)
			}
			if !utf8.ValidString(line) {
				t.Errorf("snapshot line %q is not valid UTF-8", line)
			}
		}
	}
	for {
		check(drain.snapshot())
		select {
		case <-writerDone:
			<-reaped
			check(drain.snapshot())
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// checkDrainBounds asserts the retention invariants under the drain
// lock after every mutation: open length and backing capacity, retained
// bytes, line count, and each stored line's length, backing capacity,
// and UTF-8 validity — none may exceed the bounds.
func checkDrainBounds(t *testing.T, d *stderrDrain) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.open) > maxStderrBytes || cap(d.open) > maxStderrBytes {
		t.Fatalf("open = %d bytes (cap %d), bound is %d", len(d.open), cap(d.open), maxStderrBytes)
	}
	if d.total > maxStderrBytes {
		t.Fatalf("retained bytes = %d, bound is %d", d.total, maxStderrBytes)
	}
	if d.count > maxStderrLines {
		t.Fatalf("retained lines = %d, bound is %d", d.count, maxStderrLines)
	}
	for i := range d.count {
		line := d.buf[(d.head+i)%maxStderrLines]
		if len(line) > maxStderrBytes || cap(line) > maxStderrBytes || !utf8.Valid(line) {
			t.Fatalf("stored line %d = %d bytes (cap %d, valid %t), bound is %d",
				i, len(line), cap(line), utf8.Valid(line), maxStderrBytes)
		}
	}
}

// TestDiagnostics_openAllocationBounded: streaming a huge unterminated
// line through repeated overflow never grows the open line's backing
// allocation past the byte budget, and the retained tail is exactly the
// newest 64 KiB.
func TestDiagnostics_openAllocationBounded(t *testing.T) {
	drain := &stderrDrain{}
	chunk := bytes.Repeat([]byte{'x'}, 4096)
	for range 64 { // 256 KiB on one unterminated line
		drain.append(chunk)
		checkDrainBounds(t, drain)
	}
	if lines := drain.snapshot(); len(lines) != 1 || len(lines[0]) != maxStderrBytes {
		t.Fatalf("open tail = %d lines/%d bytes, want one %d-byte line", len(lines), len(lines[0]), maxStderrBytes)
	}
}

// TestDiagnostics_lineStorageBounded: repeatedly closing oversized
// lines through repeated overflow never grows retained storage past
// the byte budget.
func TestDiagnostics_lineStorageBounded(t *testing.T) {
	drain := &stderrDrain{}
	line := append(bytes.Repeat([]byte{'a'}, maxStderrBytes), '\n')
	for range 40 {
		drain.addLine(line)
		checkDrainBounds(t, drain)
	}
	if lines := drain.snapshot(); len(lines) != 1 || len(lines[0]) != maxStderrBytes-1 {
		t.Fatalf("tail = %d lines/%d bytes, want one %d-byte line", len(lines), len(lines[0]), maxStderrBytes-1)
	}
}

// TestDiagnostics_snapshotIsACopy: mutating a returned snapshot never
// affects the retained tail.
func TestDiagnostics_snapshotIsACopy(t *testing.T) {
	drain := &stderrDrain{}
	drain.run(&chunkedReader{payload: []byte("one\ntwo\n"), chunk: 1})
	first := drain.snapshot()
	first[0] = "hacked"
	first = append(first, "injected")
	second := drain.snapshot()
	if len(second) != 2 || second[0] != "one" || second[1] != "two" {
		t.Fatalf("snapshot after mutation = %q, want [one two]", second)
	}
}

// drainState captures a drain's raw retention state, for asserting
// that Snapshot never mutates it.
type drainState struct {
	count, head, total int
	openLen, openCap   int
	open               string
	lines              []string
}

func captureDrainState(d *stderrDrain) drainState {
	d.mu.Lock()
	defer d.mu.Unlock()
	state := drainState{
		count:   d.count,
		head:    d.head,
		total:   d.total,
		openLen: len(d.open),
		openCap: cap(d.open),
		open:    string(d.open),
	}
	for i := range d.count {
		state.lines = append(state.lines, string(d.buf[(d.head+i)%maxStderrLines]))
	}
	return state
}

func equalDrainState(a, b drainState) bool {
	if a.count != b.count || a.head != b.head || a.total != b.total ||
		a.openLen != b.openLen || a.openCap != b.openCap || a.open != b.open ||
		len(a.lines) != len(b.lines) {
		return false
	}
	for i := range a.lines {
		if a.lines[i] != b.lines[i] {
			return false
		}
	}
	return true
}

// TestDiagnostics_snapshotTotalBounded: raw retention is bounded, but
// sanitizing the stored unterminated open line can expand it past the
// byte budget, so the whole returned tail — not each line — must be
// bounded. Snapshot drops the oldest whole complete lines first, then
// trims the open line's oldest bytes at a rune boundary when even that
// is not enough, and never mutates the raw retained state.
func TestDiagnostics_snapshotTotalBounded(t *testing.T) {
	line := func(i int) string {
		return fmt.Sprintf("%03d", i) + strings.Repeat("x", 4093) // 4096 bytes
	}
	build := func(t *testing.T, complete []string, open []byte) *stderrDrain {
		t.Helper()
		drain := &stderrDrain{}
		for _, l := range complete {
			drain.addLine([]byte(l + "\n"))
		}
		if len(open) > 0 {
			drain.append(open)
		}
		checkDrainBounds(t, drain)
		return drain
	}
	check := func(t *testing.T, drain *stderrDrain, want []string) {
		t.Helper()
		before := captureDrainState(drain)
		first := drain.snapshot()
		second := drain.snapshot()
		if !equalDrainState(before, captureDrainState(drain)) {
			t.Fatal("Snapshot mutated the raw retained state")
		}
		if len(first) != len(want) || len(second) != len(want) {
			t.Fatalf("snapshot = %d/%d lines, want %d", len(first), len(second), len(want))
		}
		total := 0
		for i := range want {
			if first[i] != want[i] || second[i] != want[i] {
				t.Fatalf("snapshot line %d = %q, want %q", i, first[i], want[i])
			}
			if len(first[i]) > maxStderrBytes {
				t.Fatalf("snapshot line %d holds %d bytes, bound is %d", i, len(first[i]), maxStderrBytes)
			}
			if !utf8.ValidString(first[i]) {
				t.Fatalf("snapshot line %d %q is not valid UTF-8", i, first[i])
			}
			total += len(first[i])
		}
		if total > maxStderrBytes {
			t.Fatalf("snapshot totals %d bytes, bound is %d", total, maxStderrBytes)
		}
		if len(first) > maxStderrLines {
			t.Fatalf("snapshot has %d lines, bound is %d", len(first), maxStderrLines)
		}
	}

	t.Run("drop oldest complete lines for expanding open tail", func(t *testing.T) {
		// "\xffa\xfeb" sanitizes to "\ufffda\ufffdb" (8 bytes per unit,
		// valid bytes at both boundaries keep each run separate).
		// 10 complete 4 KiB lines plus 6144 raw units (24 KiB): raw
		// retention is exactly 64 KiB, but sanitizing expands the open
		// tail to 48 KiB, so the exposed tail would be 88 KiB. The six
		// oldest complete lines drop whole to reach exactly 64 KiB.
		var complete []string
		for i := range 10 {
			complete = append(complete, line(i))
		}
		drain := build(t, complete, bytes.Repeat([]byte("\xffa\xfeb"), 6144))
		var want []string
		for i := 6; i < 10; i++ {
			want = append(want, line(i))
		}
		want = append(want, strings.Repeat("\ufffda\ufffdb", 6144))
		check(t, drain, want)
	})

	t.Run("open line alone past budget trims rune-aligned", func(t *testing.T) {
		// 8192 units plus a trailing "é" sanitize to 65538 bytes: both
		// complete lines drop, and the 64 KiB cap starts mid-rune
		// (offset 2 is a continuation byte) so it advances one byte,
		// keeping "a\ufffdb", units 1..8191, and the trailing "é":
		// 65535 bytes.
		drain := build(t, []string{line(0), line(1)},
			append(bytes.Repeat([]byte("\xffa\xfeb"), 8192), []byte("é")...))
		want := "a\ufffdb" + strings.Repeat("\ufffda\ufffdb", 8191) + "é"
		check(t, drain, []string{want})
	})

	t.Run("cap lands exactly on a rune boundary", func(t *testing.T) {
		// 10000 units sanitize to 80000 bytes; the 64 KiB cap lands
		// exactly on a unit boundary (65536 = 8 x 8192) and needs no
		// realignment: the retained tail is the newest 8192 units.
		drain := build(t, []string{line(0)}, bytes.Repeat([]byte("\xffa\xfeb"), 10000))
		check(t, drain, []string{strings.Repeat("\ufffda\ufffdb", 8192)})
	})

	t.Run("expansion inside a stored complete line", func(t *testing.T) {
		// A stored complete line that expanded at sanitization time
		// (48 KiB stored from 24 KiB raw) is already accounted for;
		// the snapshot drops it whole before trimming anything newer.
		drain := &stderrDrain{}
		drain.addLine(append(bytes.Repeat([]byte("\xffa\xfeb"), 6000), '\n'))
		drain.addLine([]byte("new\n"))
		drain.append(bytes.Repeat([]byte("\xffa\xfeb"), 4096))
		checkDrainBounds(t, drain)
		check(t, drain, []string{"new", strings.Repeat("\ufffda\ufffdb", 4096)})
	})
}

// TestDiagnostics_concurrentSnapshotUnderWrites: snapshots taken while
// stderr streams in concurrently are race-free and always within both
// bounds; the final tail is exactly the newest suffix that fits the
// byte budget.
func TestDiagnostics_concurrentSnapshotUnderWrites(t *testing.T) {
	var all []string
	for i := range 500 {
		if i%10 == 7 {
			all = append(all, fmt.Sprintf("%04d:%s", i, strings.Repeat("x", 2000)))
		} else {
			all = append(all, fmt.Sprintf("noise %d", i))
		}
	}
	pr, pw := io.Pipe()
	drain := &stderrDrain{}
	reaped := make(chan struct{})
	go func() {
		drain.run(pr)
		close(reaped)
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer pw.Close()
		for _, line := range all {
			chunk := 1 + (len(line) % 13)
			payload := []byte(line + "\n")
			for len(payload) > 0 {
				n := chunk
				if n > len(payload) {
					n = len(payload)
				}
				if _, err := pw.Write(payload[:n]); err != nil {
					return
				}
				payload = payload[n:]
			}
		}
	}()

	check := func(lines []string) {
		if len(lines) > maxStderrLines {
			t.Errorf("snapshot has %d lines, bound is %d", len(lines), maxStderrLines)
		}
		total := 0
		for _, line := range lines {
			total += len(line)
			if !utf8.ValidString(line) {
				t.Errorf("snapshot line %q is not valid UTF-8", line)
			}
			if strings.HasSuffix(line, "\n") {
				t.Errorf("snapshot line %q ends with a delimiter", line)
			}
		}
		if total > maxStderrBytes {
			t.Errorf("snapshot holds %d bytes, bound is %d", total, maxStderrBytes)
		}
	}
	stop := make(chan struct{})
	var snapWg sync.WaitGroup
	for range 4 {
		snapWg.Add(1)
		go func() {
			defer snapWg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				check(drain.snapshot())
			}
		}()
	}
	wg.Wait()
	<-reaped
	close(stop)
	snapWg.Wait()

	var n, total int
	for i := len(all) - 1; i >= 0 && n < maxStderrLines; i-- {
		if total+len(all[i]) > maxStderrBytes {
			break
		}
		total += len(all[i])
		n++
	}
	final := drain.snapshot()
	check(final)
	if len(final) != n {
		t.Fatalf("final snapshot has %d lines, want %d", len(final), n)
	}
	for i, line := range final {
		if want := all[len(all)-n+i]; line != want {
			t.Fatalf("final line %d = %q, want %q", i, line, want)
		}
	}
}

// waitForSnapshot polls client.Snapshot until cond holds; the reap
// happens in the reader goroutine after the failing call returns.
func waitForSnapshot(t *testing.T, client *Client, cond func(Snapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond(client.Snapshot()) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("snapshot never reached the expected state")
}

// TestDiagnostics_snapshotLifecycle: running/in-flight state, identity,
// canonical path, init duration, and the clean-exit terminal state.
func TestDiagnostics_snapshotLifecycle(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "block_execute")
	marker := filepath.Join(t.TempDir(), "events.log")
	t.Setenv("PERK_PLUGIN_MARKER", marker)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"),
		[]string{executable}, func(s database.Shim) error {
			shim = s
			return nil
		})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })
	loader.mu.Lock()
	client := loader.clients[0]
	loader.mu.Unlock()

	snap := client.Snapshot()
	if snap.Path != canonical {
		t.Fatalf("Path = %q, want %q", snap.Path, canonical)
	}
	if snap.PID <= 0 {
		t.Fatalf("PID = %d, want the child pid", snap.PID)
	}
	if snap.Plugin != "pluginkv" {
		t.Fatalf("Plugin = %q, want pluginkv", snap.Plugin)
	}
	if snap.InitDuration <= 0 {
		t.Fatalf("InitDuration = %v, want a recorded duration", snap.InitDuration)
	}
	if snap.InFlight != 0 {
		t.Fatalf("InFlight = %d, want 0", snap.InFlight)
	}
	if !snap.Running {
		t.Fatal("Running = false, want true")
	}
	if snap.ExitStatus != -1 {
		t.Fatalf("ExitStatus = %d, want -1 while running", snap.ExitStatus)
	}
	if snap.Error != "" {
		t.Fatalf("Error = %q, want empty", snap.Error)
	}
	if len(snap.Stderr) != 0 {
		t.Fatalf("Stderr = %q, want empty", snap.Stderr)
	}

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	callErr := make(chan error, 1)
	go func() {
		_, err := service.Execute(context.Background(), "select 1")
		callErr <- err
	}()
	waitForMarkerLines(t, marker, 1) // execute reached the plugin

	snap = client.Snapshot()
	if snap.InFlight != 1 {
		t.Fatalf("InFlight = %d, want 1 during the blocked call", snap.InFlight)
	}
	if !snap.Running || snap.PID <= 0 {
		t.Fatalf("child state = running:%t pid:%d, want running with a pid", snap.Running, snap.PID)
	}

	_ = client.Close() // stdin EOF: the child exits 0
	select {
	case <-callErr:
	case <-time.After(5 * time.Second):
		t.Fatal("pending call not released by Close")
	}
	waitForSnapshot(t, client, func(s Snapshot) bool { return !s.Running && s.PID == 0 })
	snap = client.Snapshot()
	if snap.ExitStatus != 0 {
		t.Fatalf("ExitStatus = %d, want 0 for the clean exit", snap.ExitStatus)
	}
	if snap.Running || snap.PID != 0 {
		t.Fatalf("Running = %t PID = %d, want the reaped state", snap.Running, snap.PID)
	}
	if snap.InFlight != 0 {
		t.Fatalf("InFlight = %d, want 0 after terminal", snap.InFlight)
	}
	if snap.Error != "EOF" {
		t.Fatalf("Error = %q, want the EOF terminal error", snap.Error)
	}
}

// TestDiagnostics_crashStatus: a child that exits nonzero mid-request
// records its exit status and terminal error.
func TestDiagnostics_crashStatus(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "exit_on_execute")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"),
		[]string{executable}, func(s database.Shim) error {
			shim = s
			return nil
		})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })
	loader.mu.Lock()
	client := loader.clients[0]
	loader.mu.Unlock()

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := service.Execute(context.Background(), "select 1"); err == nil {
		t.Fatal("Execute succeeded, want the child-crash error")
	}
	waitForSnapshot(t, client, func(s Snapshot) bool { return !s.Running })
	snap := client.Snapshot()
	if snap.ExitStatus != 3 {
		t.Fatalf("ExitStatus = %d, want 3", snap.ExitStatus)
	}
	if snap.PID != 0 {
		t.Fatalf("PID = %d, want 0 once reaped", snap.PID)
	}
	if !strings.Contains(snap.Error, "exit status 3") {
		t.Fatalf("Error = %q, want it to name the exit status", snap.Error)
	}
	if snap.InFlight != 0 {
		t.Fatalf("InFlight = %d, want 0 after terminal", snap.InFlight)
	}
}

// TestDiagnostics_protocolViolationStatus: a protocol violation kills
// the child; the snapshot records the terminal error and the
// signal-killed status.
func TestDiagnostics_protocolViolationStatus(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "malformed")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	client, err := spawn(executable, spawnArgs...)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = client.Close() }()

	var result sharedsql.Result
	if err := client.Call(context.Background(), methodExecute, statementParams{Statement: "select 1"}, &result); err == nil {
		t.Fatal("call succeeded, want the terminal malformed-frame error")
	}
	waitForSnapshot(t, client, func(s Snapshot) bool { return !s.Running })
	snap := client.Snapshot()
	if snap.ExitStatus != -1 {
		t.Fatalf("ExitStatus = %d, want -1 for a signal kill", snap.ExitStatus)
	}
	if !strings.Contains(snap.Error, "malformed response frame") {
		t.Fatalf("Error = %q, want the malformed-frame error", snap.Error)
	}
	if snap.PID != 0 || snap.InFlight != 0 {
		t.Fatalf("PID = %d InFlight = %d, want 0/0 after reap", snap.PID, snap.InFlight)
	}
}

// TestDiagnostics_rejectedHandshakeIdentity: a rejected handshake —
// an initialize RPC error or a wrong protocol version — still records
// the initialize duration, but never the plugin identity.
func TestDiagnostics_rejectedHandshakeIdentity(t *testing.T) {
	for _, behavior := range []string{"rpc_error_initialize", "wrong_version"} {
		t.Run(behavior, func(t *testing.T) {
			t.Setenv("PERK_PLUGIN_HELPER", "1")
			t.Setenv("PERK_PLUGIN_BEHAVIOR", behavior)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}

			loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"),
				[]string{executable}, func(database.Shim) error { return nil })
			if len(errs) != 1 {
				t.Fatalf("Load errors = %v, want the handshake rejection", errs)
			}
			t.Cleanup(func() { _ = loader.Close() })
			loader.mu.Lock()
			client := loader.clients[0]
			loader.mu.Unlock()

			waitForSnapshot(t, client, func(s Snapshot) bool { return !s.Running })
			snap := client.Snapshot()
			if snap.InitDuration <= 0 {
				t.Fatalf("InitDuration = %v, want a recorded duration despite failure", snap.InitDuration)
			}
			if snap.Plugin != "" {
				t.Fatalf("Plugin = %q, want empty before a successful handshake", snap.Plugin)
			}
			if snap.Error == "" {
				t.Fatal("Error = empty, want the terminal error text")
			}
		})
	}
}

// TestDiagnostics_stderrFloodNoBlock: a child flooding stderr never
// blocks the handshake or requests, and the retained tail is bounded,
// newest-first, and UTF-8-clean.
func TestDiagnostics_stderrFloodNoBlock(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_STDERR_FLOOD", fmt.Sprint(256<<10))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"),
		[]string{executable}, func(s database.Shim) error {
			shim = s
			return nil
		})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })
	loader.mu.Lock()
	client := loader.clients[0]
	loader.mu.Unlock()

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	result, err := service.Execute(context.Background(), "select 1")
	if err != nil {
		t.Fatalf("Execute across the stderr flood: %v", err)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "name" {
		t.Fatalf("Execute columns = %v, want [name]", result.Columns)
	}

	snap := client.Snapshot()
	if len(snap.Stderr) != maxStderrLines {
		t.Fatalf("retained %d stderr lines, want %d", len(snap.Stderr), maxStderrLines)
	}
	total := 0
	for _, line := range snap.Stderr {
		if line != "stderr noise" {
			t.Fatalf("stderr line %q, want the flood noise", line)
		}
		if !utf8.ValidString(line) {
			t.Fatalf("stderr line %q is not valid UTF-8", line)
		}
		total += len(line)
	}
	if total > maxStderrBytes {
		t.Fatalf("retained %d stderr bytes, want ≤ %d", total, maxStderrBytes)
	}
}
