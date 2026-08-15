package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/l3aro/perk-workbench/protocol/perk-v1"
)

// testEngine builds an engine from the canonical embedded assets with
// bounds shortened for tests. The bound stays above the ~1s startup of
// the re-exec'd race-instrumented helper child.
func testEngine(t *testing.T) *Engine {
	t.Helper()
	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	engine.Timeout = 2 * time.Second
	engine.Quiet = 100 * time.Millisecond
	return engine
}

// slowEngine is testEngine with a case bound long enough for the
// exact-16-MiB frame cases: the full frame travels through the pipe at
// several MB/s, which the 2s bound cannot hold.
func slowEngine(t *testing.T) *Engine {
	t.Helper()
	engine := testEngine(t)
	engine.Timeout = 8 * time.Second
	return engine
}

// runSuite runs the full suite against the helper child and returns the
// document. ctx cancels between cases when provided.
func runSuite(t *testing.T, engine *Engine, helper string, ctx context.Context) Document {
	t.Helper()
	if ctx == nil {
		ctx = context.Background()
	}
	return engine.Test(ctx, helper, helper)
}

// TestGoodHelperPassesEveryCase: an SDK-like plugin passes all 16
// cases, every child is reaped, and no build_target/open/session method
// ever crosses the wire.
func TestGoodHelperPassesEveryCase(t *testing.T) {
	dir := t.TempDir()
	helper := writeHelperScriptAt(t, dir)
	marker := filepath.Join(dir, "methods.log")
	helperEnv(t, map[string]string{"PERK_PLUGIN_MARKER": marker})
	engine := slowEngine(t)

	doc := runSuite(t, engine, helper, nil)
	if !doc.OK || doc.Failed != 0 || doc.Passed != 16 || doc.Error != "" {
		t.Fatalf("doc = %+v, want all 16 cases passed", doc)
	}
	for _, result := range doc.Cases {
		if !result.OK {
			t.Fatalf("case %q failed: %s: %s", result.Name, result.Category, result.Error)
		}
		if result.Duration <= 0 {
			t.Fatalf("case %q has no duration", result.Name)
		}
	}

	// Every child recorded its pid at start; all must be gone (reaped)
	// once the suite returns, and the only request methods ever seen are
	// the transport's — never build_target, open, or any session RPC.
	lines := readMarkerLines(t, marker)
	starts := 0
	pids := []int{}
	methods := map[string]bool{}
	for _, line := range lines {
		if rest, ok := strings.CutPrefix(line, "start "); ok {
			pid, err := strconv.Atoi(rest)
			if err != nil {
				t.Fatalf("unreadable pid line %q", line)
			}
			starts++
			pids = append(pids, pid)
			continue
		}
		methods[line] = true
	}
	if starts != 16 {
		t.Fatalf("marker records %d child starts, want 16", starts)
	}
	for _, pid := range pids {
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("child pid %d still alive after the suite — leaked child", pid)
		} else if !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("checking pid %d: %v", pid, err)
		}
	}
	want := map[string]bool{"perk/v1/initialize": true, "perk/v1/frobnicate": true}
	for method := range methods {
		if !want[method] {
			t.Fatalf("suite invoked %q — build_target/open/session RPCs must never run", method)
		}
	}
	for method := range want {
		if !methods[method] {
			t.Fatalf("suite never exercised %q", method)
		}
	}
}

// TestBrokenFixturesDetected: every intentionally broken child fails
// the initialize case with the expected category and message. Each
// subtest spawns one child, so the suite stays fast under -race.
func TestBrokenFixturesDetected(t *testing.T) {
	tests := []struct {
		name         string
		behavior     string
		wantCategory string
		wantError    string
	}{
		{name: "bad response id", behavior: "wrong_id", wantCategory: CategoryProtocol, wantError: "unknown request id"},
		{name: "duplicate response", behavior: "duplicate", wantCategory: CategoryProtocol, wantError: "duplicate response"},
		{name: "stdout noise", behavior: "noise", wantCategory: CategoryProtocol, wantError: "single JSON object"},
		{name: "wrong protocol", behavior: "wrong_jsonrpc", wantCategory: CategoryProtocol, wantError: `jsonrpc "1.0"`},
		{name: "bad capability", behavior: "bad_caps", wantCategory: CategoryBehavior, wantError: "needs a name"},
		{name: "wrong version", behavior: "wrong_version", wantCategory: CategoryBehavior, wantError: "protocol version 2, want 1"},
		{name: "no response", behavior: "no_response", wantCategory: CategoryTimeout, wantError: "no response"},
		{name: "malformed response", behavior: "malformed_response", wantCategory: CategoryProtocol, wantError: "single JSON object"},
		{name: "oversized response", behavior: "oversized_response", wantCategory: CategoryProtocol, wantError: "oversized response frame"},
		{name: "missing error message", behavior: "missing_error_message", wantCategory: CategoryProtocol, wantError: "string message"},
		{name: "non-string error message", behavior: "nonstring_error_message", wantCategory: CategoryProtocol, wantError: "string message"},
		{name: "scalar error data", behavior: "scalar_error_data", wantCategory: CategoryProtocol, wantError: "data must be an object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			helper := writeHelperScriptAt(t, dir)
			helperEnv(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": test.behavior})
			engine := testEngine(t)

			result := engine.runCase(engine.cases()[0], helper)
			if result.OK {
				t.Fatalf("case = %+v, want a failure", result)
			}
			if result.Category != test.wantCategory {
				t.Fatalf("category = %q, want %q (error %q)", result.Category, test.wantCategory, result.Error)
			}
			if !strings.Contains(result.Error, test.wantError) {
				t.Fatalf("error %q, want it to contain %q", result.Error, test.wantError)
			}
		})
	}
}

// TestSuiteContinuesAfterBrokenCase: with a child that fabricates
// response ids, the suite still runs every one of its 16 cases —
// failures never stop later cases — and the transport-only cases that
// need no response still pass.
func TestSuiteContinuesAfterBrokenCase(t *testing.T) {
	dir := t.TempDir()
	helper := writeHelperScriptAt(t, dir)
	helperEnv(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "wrong_id"})
	engine := testEngine(t)

	doc := runSuite(t, engine, helper, nil)
	if doc.OK || doc.Failed == 0 {
		t.Fatalf("doc = %+v, want the run to fail", doc)
	}
	if len(doc.Cases) != 16 {
		t.Fatalf("ran %d cases, want all 16 despite the failure", len(doc.Cases))
	}
	if doc.Passed == 0 || doc.Failed == 0 {
		t.Fatalf("doc = %+v, want both passing and failing cases", doc)
	}
	for _, name := range []string{caseInitialize, caseCleanEOFShutdown} {
		result := resultByName(t, doc, name)
		if result.OK || result.Category != CategoryProtocol {
			t.Fatalf("case %q = %+v, want a protocol failure", name, result)
		}
	}
	for _, name := range []string{caseMalformedJSON, caseEOFInput} {
		result := resultByName(t, doc, name)
		if !result.OK {
			t.Fatalf("case %q = %+v, want it to have run and passed", name, result)
		}
	}
}

// resultByName returns one case result by its stable name.
func resultByName(t *testing.T, doc Document, name string) Result {
	t.Helper()
	for _, result := range doc.Cases {
		if result.Name == name {
			return result
		}
	}
	t.Fatalf("case %q did not run", name)
	return Result{}
}

// TestExactMaxFrameResponseAccepted: a response frame of exactly
// maxFrameBytes including the newline is accepted and answered; the
// initialize case passes end to end.
func TestExactMaxFrameResponseAccepted(t *testing.T) {
	dir := t.TempDir()
	helper := writeHelperScriptAt(t, dir)
	helperEnv(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "boundary_response"})
	engine := slowEngine(t)

	result := engine.runCase(engine.cases()[0], helper)
	if !result.OK {
		t.Fatalf("case = %+v, want a pass against the exact-max response frame", result)
	}
}

// TestStderrFloodBounded: a child flooding stderr can never deadlock a
// case or grow its diagnostics beyond the 64 KiB / 100 line bounds.
func TestStderrFloodBounded(t *testing.T) {
	dir := t.TempDir()
	helper := writeHelperScriptAt(t, dir)
	helperEnv(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "stderr_flood"})
	engine := testEngine(t)

	result := engine.runCase(engine.cases()[0], helper)
	if !result.OK {
		t.Fatalf("case = %+v, want a pass despite the flood", result)
	}
	if len(result.Stderr) == 0 {
		t.Fatal("want a retained stderr tail from the flood")
	}
	if len(result.Stderr) > maxStderrLines {
		t.Fatalf("stderr tail has %d lines, want at most %d", len(result.Stderr), maxStderrLines)
	}
	total := 0
	for _, line := range result.Stderr {
		total += len(line)
	}
	if total > maxStderrBytes {
		t.Fatalf("stderr tail is %d bytes, want at most %d", total, maxStderrBytes)
	}
}

// TestInterruptStopsBetweenCases: a canceled context stops the run
// between cases and fails it overall; no child is spawned after the
// interrupt.
func TestInterruptStopsBetweenCases(t *testing.T) {
	dir := t.TempDir()
	helper := writeHelperScriptAt(t, dir)
	marker := filepath.Join(dir, "events.log")
	helperEnv(t, map[string]string{"PERK_PLUGIN_MARKER": marker})
	engine := testEngine(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(120 * time.Millisecond) // after ~2 cases
		cancel()
	}()
	doc := runSuite(t, engine, helper, ctx)
	if doc.OK || doc.Error != "interrupted" {
		t.Fatalf("doc = %+v, want an interrupted failing run", doc)
	}
	starts := 0
	for _, line := range readMarkerLines(t, marker) {
		if strings.HasPrefix(line, "start ") {
			starts++
		}
	}
	if starts != len(doc.Cases) {
		t.Fatalf("%d children started but %d cases ran", starts, len(doc.Cases))
	}
}

// TestPreCanceledRunsNothing: a pre-canceled context runs no cases at
// all and fails the run.
func TestPreCanceledRunsNothing(t *testing.T) {
	dir := t.TempDir()
	helper := writeHelperScriptAt(t, dir)
	helperEnv(t, nil)
	engine := testEngine(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc := runSuite(t, engine, helper, ctx)
	if doc.OK || doc.Error != "interrupted" || len(doc.Cases) != 0 {
		t.Fatalf("doc = %+v, want an interrupted run with no cases", doc)
	}
}

// TestSpawnFailure: an unresolvable child fails every case with the
// spawn category, and the suite still runs through all of them.
func TestSpawnFailure(t *testing.T) {
	engine := testEngine(t)
	doc := runSuite(t, engine, filepath.Join(t.TempDir(), "no-such-executable"), nil)
	if doc.OK || doc.Failed != 16 {
		t.Fatalf("doc = %+v, want every case spawn-failed", doc)
	}
	for _, result := range doc.Cases {
		if result.Category != CategorySpawn || result.Error == "" || result.Duration <= 0 {
			t.Fatalf("case %q = %+v, want a spawn failure with a duration", result.Name, result)
		}
	}
}

// fakeSource is an injectable protocol-asset source for drift tests.
type fakeSource struct {
	schema   []byte
	manifest []byte
	fixtures map[string][]byte
}

func (f fakeSource) Schema() []byte { return f.schema }

func (f fakeSource) Manifest() ([]byte, error) {
	if f.manifest == nil {
		return nil, errors.New("no manifest")
	}
	return f.manifest, nil
}

func (f fakeSource) Fixture(name string) ([]byte, error) {
	frame, ok := f.fixtures[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return frame, nil
}

// canonicalAssets returns the embedded schema, manifest, and every
// fixture frame inlined, so a fake source can drift in exactly one
// dimension.
func canonicalAssets(t *testing.T) ([]byte, []byte, map[string][]byte) {
	t.Helper()
	var manifest struct {
		Fixtures []struct {
			File string `json:"file"`
		} `json:"fixtures"`
	}
	raw, err := perkv1.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	fixtures := map[string][]byte{}
	for _, entry := range manifest.Fixtures {
		frame, err := perkv1.Fixture(entry.File)
		if err != nil {
			t.Fatal(err)
		}
		fixtures[entry.File] = frame
	}
	return perkv1.Schema(), raw, fixtures
}

// TestManifestDriftFailsCoherently: every drift mode — missing or
// unparseable schema, a manifest ref that does not resolve in the
// schema, a manifest naming a missing fixture, a missing required
// entry, a missing expected error code, and a fixture whose method
// drifted from its manifest entry — fails engine construction with a
// coherent error instead of misbehaving per case.
func TestManifestDriftFailsCoherently(t *testing.T) {
	schema, raw, fixtures := canonicalAssets(t)

	driftManifest := func(mutate func(map[string]any)) []byte {
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		mutate(doc)
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	dropEntry := func(doc map[string]any, file string) {
		list := doc["fixtures"].([]any)
		kept := make([]any, 0, len(list))
		for _, item := range list {
			if item.(map[string]any)["file"] == file {
				continue
			}
			kept = append(kept, item)
		}
		doc["fixtures"] = kept
	}
	missingCode := func(doc map[string]any, file string) {
		for _, item := range doc["fixtures"].([]any) {
			entry := item.(map[string]any)
			if entry["file"] == file {
				delete(entry, "code")
			}
		}
	}
	brokenRef := func(doc map[string]any, file string) {
		for _, item := range doc["fixtures"].([]any) {
			entry := item.(map[string]any)
			if entry["file"] == file {
				entry["ref"] = "#/$defs/no-such-def"
			}
		}
	}
	valid := func(fixtures map[string][]byte) fakeSource {
		return fakeSource{schema: schema, manifest: raw, fixtures: fixtures}
	}

	tests := []struct {
		name    string
		source  source
		wantErr string
	}{
		{name: "missing schema", source: fakeSource{manifest: raw, fixtures: fixtures}, wantErr: "schema is empty"},
		{name: "unparseable schema", source: fakeSource{schema: []byte("{not json"), manifest: raw, fixtures: fixtures}, wantErr: "schema does not parse"},
		{name: "ref does not resolve", source: fakeSource{schema: schema, manifest: driftManifest(func(doc map[string]any) { brokenRef(doc, "request-initialize.json") }), fixtures: fixtures}, wantErr: "does not resolve in the schema"},
		{name: "missing manifest", source: fakeSource{schema: schema, fixtures: fixtures}, wantErr: "loading the fixture manifest"},
		{name: "unparseable manifest", source: fakeSource{schema: schema, manifest: []byte("{not json"), fixtures: fixtures}, wantErr: "does not parse"},
		{name: "manifest names missing fixture", source: valid(fixturesWithout(t, fixtures, "request-initialize.json")), wantErr: "drift"},
		{name: "required entry dropped", source: fakeSource{schema: schema, manifest: driftManifest(func(doc map[string]any) { dropEntry(doc, "request-initialize.json") }), fixtures: fixtures}, wantErr: "does not list required fixture"},
		{name: "expected code missing", source: fakeSource{schema: schema, manifest: driftManifest(func(doc map[string]any) { missingCode(doc, "invalid-request-jsonrpc.json") }), fixtures: fixtures}, wantErr: "missing the expected error code"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewFrom(test.source)
			if err == nil {
				t.Fatal("engine construction succeeded, want a coherent drift failure")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error %q, want it to contain %q", err, test.wantErr)
			}
		})
	}

	// Method drift: the fixture frame and the manifest disagree.
	mutated := map[string][]byte{}
	for name, frame := range fixtures {
		mutated[name] = frame
	}
	var request struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(fixtures["request-initialize.json"], &request); err != nil || request.Method == "" {
		t.Fatal("request-initialize fixture has no method")
	}
	mutated["request-initialize.json"] = []byte(strings.Replace(string(fixtures["request-initialize.json"]), request.Method, "perk/v1/frobnicate", 1))
	_, err := NewFrom(valid(mutated))
	if err == nil || !strings.Contains(err.Error(), "manifest expects") {
		t.Fatalf("method drift: err = %v, want a manifest-expects failure", err)
	}
}

func fixturesWithout(t *testing.T, fixtures map[string][]byte, file string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for name, frame := range fixtures {
		if name != file {
			out[name] = frame
		}
	}
	return out
}

// TestManifestCoherence pins the canonical embed: every fixture file is
// listed in the manifest and every manifest entry loads — the two can
// never drift apart, and the cases' metadata requirements hold.
func TestManifestCoherence(t *testing.T) {
	engine, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	names, err := perkv1.FixtureNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(engine.manifest.Fixtures) {
		t.Fatalf("embed holds %d fixtures but the manifest lists %d", len(names), len(engine.manifest.Fixtures))
	}
	listed := map[string]bool{}
	for _, entry := range engine.manifest.Fixtures {
		listed[entry.File] = true
		if _, ok := engine.fixtures[entry.File]; !ok {
			t.Fatalf("manifest entry %q has no embedded frame", entry.File)
		}
	}
	for _, name := range names {
		if !listed[name] {
			t.Fatalf("embedded fixture %q is not listed in the manifest", name)
		}
	}
}

// TestStderrDrainFixedBounds: repeated huge unterminated and completed
// lines can never grow the drain's storage beyond the fixed bounds —
// raw retained bytes, the open line's length and backing capacity
// (exact growth, never geometric append), and the ring slot count all
// stay within budget.
func TestStderrDrainFixedBounds(t *testing.T) {
	d := newStderrDrain()
	huge := bytes.Repeat([]byte("y"), 1<<20)
	for i := 0; i < 100; i++ {
		d.append(huge) // unterminated: only the newest 64 KiB survives
	}
	d.mu.Lock()
	if d.total > maxStderrBytes {
		t.Fatalf("raw total = %d, want at most %d", d.total, maxStderrBytes)
	}
	if len(d.open) > maxStderrBytes {
		t.Fatalf("open line length = %d, want at most %d", len(d.open), maxStderrBytes)
	}
	if cap(d.open) > maxStderrBytes {
		t.Fatalf("open line backing capacity = %d, want at most %d (exact growth)", cap(d.open), maxStderrBytes)
	}
	d.mu.Unlock()

	completed := newStderrDrain()
	for i := 0; i < 500; i++ {
		line := make([]byte, len(huge)+1)
		copy(line, huge)
		line[len(huge)] = '\n'
		completed.addLine(line)
	}
	completed.mu.Lock()
	if completed.total > maxStderrBytes {
		t.Fatalf("completed raw total = %d, want at most %d", completed.total, maxStderrBytes)
	}
	if completed.count > maxStderrLines {
		t.Fatalf("completed line count = %d, want at most %d", completed.count, maxStderrLines)
	}
	if cap(completed.buf) != maxStderrLines {
		t.Fatalf("ring capacity = %d, want exactly %d slots", cap(completed.buf), maxStderrLines)
	}
	completed.mu.Unlock()
	tail := completed.snapshot()
	if len(tail) > maxStderrLines {
		t.Fatalf("snapshot has %d lines, want at most %d", len(tail), maxStderrLines)
	}
	total := 0
	for _, line := range tail {
		total += len(line)
	}
	if total > maxStderrBytes {
		t.Fatalf("snapshot total = %d bytes, want at most %d", total, maxStderrBytes)
	}
}

// TestStderrDrainEOFPartialTail: a trailing unterminated line is
// retained at EOF — its bytes are already counted in total, so
// transferring it to completed storage must not double-count and trim
// it away (a 40 KiB tail fits the budget and must survive).
func TestStderrDrainEOFPartialTail(t *testing.T) {
	d := newStderrDrain()
	line := bytes.Repeat([]byte("x"), 40<<10)
	for len(line) > 0 {
		chunk := line
		if len(chunk) > 4096 {
			chunk = chunk[:4096]
		}
		d.append(chunk)
		line = line[len(chunk):]
	}
	d.finish()
	d.mu.Lock()
	if d.total != 40<<10 {
		t.Fatalf("raw total = %d, want %d", d.total, 40<<10)
	}
	d.mu.Unlock()
	tail := d.snapshot()
	if len(tail) != 1 {
		t.Fatalf("tail has %d lines, want the one unterminated line", len(tail))
	}
	if len(tail[0]) != 40<<10 {
		t.Fatalf("tail line is %d bytes, want the full 40 KiB", len(tail[0]))
	}
}

// TestStderrDrainCompletedLineBudget: a single large completed line
// within the byte budget survives; the delimiter is never double-counted.
func TestStderrDrainCompletedLineBudget(t *testing.T) {
	d := newStderrDrain()
	line := bytes.Repeat([]byte("z"), 40<<10)
	chunk := make([]byte, len(line)+1)
	copy(chunk, line)
	chunk[len(line)] = '\n'
	d.addLine(chunk)
	d.mu.Lock()
	if d.total != 40<<10 {
		t.Fatalf("raw total = %d, want %d", d.total, 40<<10)
	}
	d.mu.Unlock()
	tail := d.snapshot()
	if len(tail) != 1 || len(tail[0]) != 40<<10 {
		t.Fatalf("tail = %d lines of %d bytes, want one 40 KiB line", len(tail), len(tail[0]))
	}
}

// TestStderrDrainSanitizedTailBounded: sanitizing invalid UTF-8 can
// expand bytes up to threefold; the exposed tail must still stay within
// the byte and line bounds and remain valid UTF-8.
func TestStderrDrainSanitizedTailBounded(t *testing.T) {
	d := newStderrDrain()
	noise := bytes.Repeat([]byte{0xff, 0xfe}, 512) // 1024 invalid bytes per line
	chunk := make([]byte, len(noise)+1)
	for i := 0; i < 300; i++ {
		copy(chunk, noise)
		chunk[len(noise)] = '\n'
		d.addLine(chunk)
	}
	tail := d.snapshot()
	if len(tail) > maxStderrLines {
		t.Fatalf("snapshot has %d lines, want at most %d", len(tail), maxStderrLines)
	}
	total := 0
	for _, line := range tail {
		total += len(line)
		if !utf8.ValidString(line) {
			t.Fatalf("snapshot line is not valid UTF-8: %q", line)
		}
	}
	if total > maxStderrBytes {
		t.Fatalf("sanitized snapshot total = %d bytes, want at most %d", total, maxStderrBytes)
	}
}

// TestStrictInitializeParamsPassesSuite: a plugin that rejects any
// unknown initialize params — so a pad smuggled into initialize would
// fail — still passes every case, because the exact-16 MiB frame is
// built from the unknown-method fixture whose params are never
// validated.
func TestStrictInitializeParamsPassesSuite(t *testing.T) {
	dir := t.TempDir()
	helper := writeHelperScriptAt(t, dir)
	helperEnv(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "strict_params"})
	engine := slowEngine(t)

	doc := runSuite(t, engine, helper, nil)
	if !doc.OK || doc.Failed != 0 || doc.Passed != 16 {
		t.Fatalf("doc = %+v, want all 16 cases passed against strict initialize params", doc)
	}
}

// TestStderrDrainSnapshotIsFresh: mutating a returned snapshot never
// affects the drain.
func TestStderrDrainSnapshotIsFresh(t *testing.T) {
	d := newStderrDrain()
	d.addLine([]byte("hello\n"))
	tail := d.snapshot()
	tail[0] = "mutated"
	if got := d.snapshot()[0]; got != "hello" {
		t.Fatalf("snapshot is not a fresh copy: %q", got)
	}
}

// readMarkerLines returns the non-empty lines of the marker file.
func readMarkerLines(t *testing.T, path string) []string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(contents), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
