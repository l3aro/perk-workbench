package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestPluginTestHelperChild is the re-executed plugin child for the
// plugin test command tests. It serves an SDK-like perk/v1 transport on
// stdio — initialize required, unknown methods answered with -32601,
// invalid requests answered with -32600 and id null, malformed or
// non-object or invalid UTF-8 or oversized input frames terminal — plus
// behavior overrides for broken fixtures. It always ends with os.Exit,
// never returning to the testing framework, so no PASS/ok output
// corrupts the protocol stream.
func TestPluginTestHelperChild(t *testing.T) {
	if os.Getenv("PERK_PLUGIN_TEST_HELPER") != "1" {
		return
	}
	servePluginTestHelper()
}

// writePluginTestHelperScriptAt writes an executable wrapper at path
// that re-executes the current test binary as the plugin test helper
// child, so the real spawn path is exercised end to end.
func writePluginTestHelperScriptAt(t *testing.T, path string) string {
	t.Helper()
	t.Setenv("PERK_HELPER_BINARY", os.Args[0])
	script := "#!/bin/sh\nexec \"$PERK_HELPER_BINARY\" -test.run=TestPluginTestHelperChild\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// setupPluginTestHelper prepares the environment for one plugin test
// run: the helper guard, the given PERK_PLUGIN_* overrides, and a fresh
// helper executable path.
func setupPluginTestHelper(t *testing.T, env map[string]string) string {
	t.Helper()
	script := writePluginTestHelperScriptAt(t, filepath.Join(t.TempDir(), "plugin-test-helper"))
	t.Setenv("PERK_PLUGIN_TEST_HELPER", "1")
	for key, value := range env {
		t.Setenv(key, value)
	}
	return script
}

// testWireResponse is the helper's response envelope: success replies
// carry result, error replies carry error, never both.
type testWireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *wireError      `json:"error,omitempty"`
}

type testInitializeResult struct {
	ProtocolVersion int `json:"protocol_version"`
	Capabilities    struct {
		Name    string `json:"name"`
		Display string `json:"display"`
		Targets []struct {
			Prefix string `json:"prefix"`
		} `json:"targets"`
	} `json:"capabilities"`
}

// pluginTestHelper is the SDK-like child serving the test command.
type pluginTestHelper struct {
	behavior string
	marker   string
}

func servePluginTestHelper() {
	h := &pluginTestHelper{
		behavior: os.Getenv("PERK_PLUGIN_BEHAVIOR"),
		marker:   os.Getenv("PERK_PLUGIN_MARKER"),
	}
	if flood := envIntOr(os.Getenv("PERK_PLUGIN_STDERR_FLOOD"), 0); flood > 0 {
		noise := []byte(strings.Repeat("stderr noise\n", 64)) // 1536 bytes
		for written := 0; written < flood; written += len(noise) {
			_, _ = os.Stderr.Write(noise)
		}
	}
	h.serve()
}

// serve reads request frames and answers until stdin closes or a
// terminal input violation occurs.
func (h *pluginTestHelper) serve() {
	reader := bufio.NewReaderSize(os.Stdin, 16<<20)
	initialized := false
	for {
		line, err := readHelperFrame(reader)
		if err != nil {
			os.Exit(0) // EOF, or an oversized input frame: terminal
		}
		if !utf8.Valid(line) {
			os.Exit(1) // invalid UTF-8 input: terminal
		}
		var incoming struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &incoming); err != nil {
			os.Exit(1) // malformed or non-object input: terminal
		}
		if len(incoming.ID) == 0 {
			continue // notifications (cancel) never answered
		}
		id, validID := parseTestHelperID(incoming.ID)
		if incoming.JSONRPC != "2.0" || incoming.Method == "" || !validID {
			h.respondID(id, -32600, "invalid request")
			continue
		}
		// The marker records every dispatched request, so tests can
		// prove which protocol methods the suite actually sends.
		if h.marker != "" {
			appendMarker(h.marker, incoming.Method)
		}
		if incoming.Method == "perk/v1/initialize" {
			if initialized {
				h.respondID(id, -32600, "already initialized")
				continue
			}
			initialized = true
			h.respondInitialize(*id)
			continue
		}
		if !initialized {
			h.respondID(id, -32600, "initialize required before "+incoming.Method)
			continue
		}
		if incoming.Method == "perk/v1/frobnicate" {
			h.respondID(id, -32601, "method not found: "+incoming.Method)
			continue
		}
		h.respondID(id, 0, "")
	}
}

// respondInitialize answers one initialize request, applying the
// response behaviors the broken fixtures need.
func (h *pluginTestHelper) respondInitialize(id uint64) {
	result := testInitializeResult{ProtocolVersion: 1}
	result.Capabilities.Name = "conftest"
	result.Capabilities.Display = "Conformance Helper"
	result.Capabilities.Targets = []struct {
		Prefix string `json:"prefix"`
	}{{Prefix: "conftest:"}}
	switch h.behavior {
	case "no_response":
		return
	case "wrong_id":
		h.respond(999999, result, nil)
		return
	case "duplicate":
		frame := h.responseFrame(id, result, nil)
		_, _ = os.Stdout.Write(append(frame, frame...))
		return
	case "noise":
		fmt.Fprint(os.Stdout, "not a protocol frame\n")
	case "wrong_jsonrpc":
		fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"1.0\",\"id\":%d,\"result\":{}}\n", id)
		return
	case "malformed_response":
		fmt.Fprint(os.Stdout, "not json\n")
		return
	case "oversized_response":
		fmt.Fprint(os.Stdout, strings.Repeat(" ", 17<<20)+"\n")
		return
	case "bad_caps":
		result.Capabilities.Name = ""
	case "wrong_version":
		result.ProtocolVersion = 2
	}
	h.respond(id, result, nil)
}

// respondID answers with a JSON-RPC error, echoing the id when it is
// valid and null otherwise, matching the canonical SDK.
func (h *pluginTestHelper) respondID(id *uint64, code int, message string) {
	_, _ = os.Stdout.Write(h.responseFramePtr(id, nil, &wireError{Code: code, Message: message}))
}

func (h *pluginTestHelper) respond(id uint64, result any, rpcErr *wireError) {
	_, _ = os.Stdout.Write(h.responseFramePtr(&id, result, rpcErr))
}

// responseFrame builds one complete response frame.
func (h *pluginTestHelper) responseFrame(id uint64, result any, rpcErr *wireError) []byte {
	return h.responseFramePtr(&id, result, rpcErr)
}

func (h *pluginTestHelper) responseFramePtr(id *uint64, result any, rpcErr *wireError) []byte {
	frame := testWireResponse{JSONRPC: "2.0", ID: id, Error: rpcErr}
	if rpcErr == nil {
		payload, err := json.Marshal(result)
		if err != nil {
			os.Exit(1)
		}
		frame.Result = payload
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		os.Exit(1)
	}
	return append(payload, '\n')
}

// parseTestHelperID parses an id member: a valid unsigned integer id,
// or nil with validID false for anything else (strings, floats,
// negatives, null — the reply must then carry id null).
func parseTestHelperID(raw json.RawMessage) (*uint64, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] < '0' || trimmed[0] > '9' {
		return nil, false
	}
	var id uint64
	if err := json.Unmarshal(trimmed, &id); err != nil {
		return nil, false
	}
	return &id, true
}

// TestPluginTest_goodPlugin: a compliant SDK-like plugin passes every
// conformance case; --json emits exactly one document on stdout with
// nothing on stderr; stderr tails stay bounded despite a flood; and the
// suite exercises only the transport methods — never build_target,
// open, or any session RPC.
func TestPluginTest_goodPlugin(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "methods.log")
	helper := setupPluginTestHelper(t, map[string]string{
		"PERK_PLUGIN_STDERR_FLOOD": "131072",
		"PERK_PLUGIN_MARKER":       marker,
	})

	status, stdout, stderr := runCLI(t, "plugin", "test", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("plugin test = %d, stderr %q, want 0 with a JSON document", status, stderr)
	}
	var doc struct {
		Entry string `json:"entry"`
		Path  string `json:"path"`
		Error string `json:"error"`
		Cases []struct {
			Name     string   `json:"name"`
			OK       bool     `json:"ok"`
			Duration int64    `json:"duration"`
			Category string   `json:"category"`
			Error    string   `json:"error"`
			Stderr   []string `json:"stderr"`
		} `json:"cases"`
		Passed int  `json:"passed"`
		Failed int  `json:"failed"`
		OK     bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout %q is not one JSON document: %v", stdout, err)
	}
	if !doc.OK || doc.Error != "" || doc.Failed != 0 || doc.Passed != 16 {
		t.Fatalf("doc = %+v, want 16 passed and no failures", doc)
	}
	if doc.Entry != helper || doc.Path != helper {
		t.Fatalf("entry/path = %q/%q, want the resolved helper", doc.Entry, doc.Path)
	}
	if len(doc.Cases) != 16 {
		t.Fatalf("doc has %d cases, want 16", len(doc.Cases))
	}
	withTail := 0
	for _, result := range doc.Cases {
		if !result.OK || result.Name == "" || result.Duration <= 0 {
			t.Fatalf("case = %+v, want a passed named case with a duration", result)
		}
		if len(result.Stderr) == 0 {
			continue
		}
		withTail++
		if len(result.Stderr) > 100 {
			t.Fatalf("case %q stderr tail has %d lines, want at most 100", result.Name, len(result.Stderr))
		}
		total := 0
		for _, line := range result.Stderr {
			total += len(line)
		}
		if total > 64<<10 {
			t.Fatalf("case %q stderr tail is %d bytes, want at most 64 KiB", result.Name, total)
		}
	}
	if withTail == 0 {
		t.Fatal("want a retained stderr tail from the flood")
	}

	// The suite never invoked build_target/open/session methods.
	methods := map[string]bool{}
	for _, line := range readMarkerLines(t, marker) {
		methods[line] = true
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

	// Human output: one PASS line per case plus final counts.
	status, stdout, stderr = runCLI(t, "plugin", "test", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("human plugin test = %d, stderr %q", status, stderr)
	}
	if strings.Count(stdout, "PASS") != 16 {
		t.Fatalf("human stdout has %d PASS lines, want 16:\n%s", strings.Count(stdout, "PASS"), stdout)
	}
	if !strings.Contains(stdout, "16 passed, 0 failed") {
		t.Fatalf("human stdout = %q, want the final counts", stdout)
	}
}

// TestPluginTest_brokenPlugin: a child fabricating response ids fails
// the cases that need a response; the suite still runs all 16 cases,
// the JSON document stays valid, and human output shows the failure and
// the final counts.
func TestPluginTest_brokenPlugin(t *testing.T) {
	helper := setupPluginTestHelper(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "wrong_id"})

	status, stdout, stderr := runCLI(t, "plugin", "test", "--json", helper)
	if status != 1 || stderr != "" {
		t.Fatalf("plugin test = %d, stderr %q, want 1 with a JSON document", status, stderr)
	}
	var doc struct {
		Cases []struct {
			Name     string `json:"name"`
			OK       bool   `json:"ok"`
			Category string `json:"category"`
			Error    string `json:"error"`
		} `json:"cases"`
		Passed int  `json:"passed"`
		Failed int  `json:"failed"`
		OK     bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout %q is not one JSON document: %v", stdout, err)
	}
	if doc.OK || doc.Failed == 0 || doc.Passed == 0 {
		t.Fatalf("doc = %+v, want both passing and failing cases", doc)
	}
	if len(doc.Cases) != 16 {
		t.Fatalf("doc has %d cases, want all 16 despite the failure", len(doc.Cases))
	}
	for _, result := range doc.Cases {
		if result.Name == "initialize" {
			if result.OK || result.Category != "protocol" || !strings.Contains(result.Error, "unknown request id") {
				t.Fatalf("initialize = %+v, want a protocol failure with an unknown-id message", result)
			}
		}
		if result.Name == "malformed_json_terminates" && !result.OK {
			t.Fatalf("malformed_json_terminates = %+v, want it to have run and passed", result)
		}
	}

	// Human output: FAIL lines with category and message, PASS lines,
	// and the final counts.
	status, stdout, stderr = runCLI(t, "plugin", "test", helper)
	if status != 1 || stderr != "" {
		t.Fatalf("human plugin test = %d, stderr %q", status, stderr)
	}
	if !strings.Contains(stdout, "FAIL  protocol: response for unknown request id") {
		t.Fatalf("human stdout = %q, want the failing case line", stdout)
	}
	if !strings.Contains(stdout, fmt.Sprintf("%d passed, %d failed", doc.Passed, doc.Failed)) {
		t.Fatalf("human stdout = %q, want the final counts", stdout)
	}
}

// TestPluginTest_resolveFailure: an unresolvable executable fails with
// a JSON document and exit status 1, nothing on stderr.
func TestPluginTest_resolveFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-plugin")
	status, stdout, stderr := runCLI(t, "plugin", "test", "--json", missing)
	if status != 1 || stderr != "" {
		t.Fatalf("plugin test = %d, stderr %q, want 1 with a JSON document", status, stderr)
	}
	var doc struct {
		Entry string `json:"entry"`
		Error string `json:"error"`
		OK    bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout %q is not one JSON document: %v", stdout, err)
	}
	if doc.Entry != missing || doc.OK || !strings.Contains(doc.Error, "resolve:") {
		t.Fatalf("doc = %+v, want a resolve failure", doc)
	}

	status, stdout, stderr = runCLI(t, "plugin", "test", missing)
	if status != 1 || stderr != "" || !strings.Contains(stdout, "resolve:") {
		t.Fatalf("human resolve failure = %d, stdout %q, stderr %q", status, stdout, stderr)
	}
}

// TestPluginTest_flagPositionIndependent: --json works before or after
// the operand.
func TestPluginTest_flagPositionIndependent(t *testing.T) {
	helper := setupPluginTestHelper(t, nil)
	status1, stdout1, stderr1 := runCLI(t, "plugin", "test", "--json", helper)
	status2, stdout2, stderr2 := runCLI(t, "plugin", "test", helper, "--json")
	if status1 != status2 || status1 != 0 || stderr1 != "" || stderr2 != "" {
		t.Fatalf("flag position changed exit/stderr: %d/%q vs %d/%q", status1, stderr1, status2, stderr2)
	}
	var doc1, doc2 struct {
		Passed int  `json:"passed"`
		OK     bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(stdout1), &doc1); err != nil {
		t.Fatalf("stdout1 %q is not JSON: %v", stdout1, err)
	}
	if err := json.Unmarshal([]byte(stdout2), &doc2); err != nil {
		t.Fatalf("stdout2 %q is not JSON: %v", stdout2, err)
	}
	if doc1 != doc2 {
		t.Fatalf("flag position changed the document: %+v vs %+v", doc1, doc2)
	}
}
