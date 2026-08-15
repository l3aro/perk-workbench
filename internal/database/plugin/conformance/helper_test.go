package conformance

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestConformanceHelperChild is the re-executed plugin child for
// conformance tests. It serves an SDK-like perk/v1 transport on stdio —
// the protocol rules the canonical Node SDK enforces: initialize
// required, unknown methods answered with -32601, invalid requests
// answered with -32600 and id null, malformed or non-object or invalid
// UTF-8 or oversized input frames terminal — plus behavior overrides
// for broken fixtures. It always ends with os.Exit, never returning to
// the testing framework, so no PASS/ok output corrupts the protocol
// stream.
func TestConformanceHelperChild(t *testing.T) {
	if os.Getenv("PERK_CONFORMANCE_HELPER") != "1" {
		return
	}
	serveHelper()
}

// writeHelperScriptAt writes an executable wrapper at dir/helper that
// re-executes the current test binary as the conformance helper child,
// so the engine's real spawn path is exercised end to end. It returns
// the wrapper path.
func writeHelperScriptAt(t *testing.T, dir string) string {
	t.Helper()
	t.Setenv("PERK_HELPER_BINARY", os.Args[0])
	path := filepath.Join(dir, "conformance-helper")
	script := "#!/bin/sh\nexec \"$PERK_HELPER_BINARY\" -test.run=TestConformanceHelperChild\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// helperEnv prepares the environment for one test: the helper guard and
// the given PERK_PLUGIN_* overrides.
func helperEnv(t *testing.T, env map[string]string) {
	t.Helper()
	t.Setenv("PERK_CONFORMANCE_HELPER", "1")
	for key, value := range env {
		t.Setenv(key, value)
	}
}

type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *wireError      `json:"error,omitempty"`
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type helperCapabilities struct {
	Name    string `json:"name"`
	Display string `json:"display"`
	Targets []struct {
		Prefix string `json:"prefix"`
	} `json:"targets"`
}

// helperCaps builds the advertised capabilities: a nonblank identity
// with one target form, valid under the registration invariants.
func helperCaps(name, display string) helperCapabilities {
	return helperCapabilities{
		Name:    name,
		Display: display,
		Targets: []struct {
			Prefix string `json:"prefix"`
		}{{Prefix: "conftest:"}},
	}
}

type helperInitializeResult struct {
	ProtocolVersion int                `json:"protocol_version"`
	Capabilities    helperCapabilities `json:"capabilities"`
}

type helperBehavior string

const (
	behaviorDefault         helperBehavior = ""
	behaviorNoResponse      helperBehavior = "no_response"
	behaviorStderrFlood     helperBehavior = "stderr_flood"
	behaviorBoundaryResp    helperBehavior = "boundary_response"
	behaviorOversizedResp   helperBehavior = "oversized_response"
	behaviorWrongID         helperBehavior = "wrong_id"
	behaviorDuplicate       helperBehavior = "duplicate"
	behaviorNoise           helperBehavior = "noise"
	behaviorWrongJSONRPC    helperBehavior = "wrong_jsonrpc"
	behaviorBadCaps         helperBehavior = "bad_caps"
	behaviorWrongVersion    helperBehavior = "wrong_version"
	behaviorMalformedResp   helperBehavior = "malformed_response"
	behaviorStrictParams    helperBehavior = "strict_params"
	behaviorMissingErrMsg   helperBehavior = "missing_error_message"
	behaviorNonstringErrMsg helperBehavior = "nonstring_error_message"
	behaviorScalarErrData   helperBehavior = "scalar_error_data"
)

type helper struct {
	behavior helperBehavior
	marker   string
}

func serveHelper() {
	h := &helper{behavior: helperBehavior(os.Getenv("PERK_PLUGIN_BEHAVIOR"))}
	if h.behavior == behaviorStderrFlood {
		noise := []byte(strings.Repeat("stderr noise\n", 64)) // 1536 bytes
		for written := 0; written < 1<<20; written += len(noise) {
			_, _ = os.Stderr.Write(noise)
		}
	}
	h.marker = os.Getenv("PERK_PLUGIN_MARKER")
	if h.marker != "" {
		appendLine(h.marker, "start "+strconv.Itoa(os.Getpid()))
	}
	h.serve()
}

// serve reads request frames and answers until stdin closes or a
// terminal input violation occurs.
func (h *helper) serve() {
	reader := bufio.NewReaderSize(os.Stdin, maxFrameBytes)
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
		id, validID := parseHelperID(incoming.ID)
		if incoming.JSONRPC != "2.0" || incoming.Method == "" || !validID {
			h.respondID(id, -32600, "invalid request")
			continue
		}
		// The marker records every dispatched request, so tests can
		// prove which protocol methods the suite actually sends.
		if h.marker != "" {
			appendLine(h.marker, incoming.Method)
		}
		if incoming.Method == "perk/v1/initialize" {
			if initialized {
				h.respondID(id, -32600, "already initialized")
				continue
			}
			if h.behavior == behaviorStrictParams {
				// A strict plugin validates initialize params: any
				// member beyond the canonical pair is rejected, so a
				// pad smuggled into initialize would fail the case.
				var params map[string]any
				if err := json.Unmarshal(incoming.Params, &params); err != nil {
					os.Exit(1)
				}
				unknown := ""
				for key := range params {
					if key != "protocol_version" && key != "workbench_version" {
						unknown = key
						break
					}
				}
				if unknown != "" {
					h.respondID(id, -32602, "unknown initialize param: "+unknown)
					continue
				}
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
func (h *helper) respondInitialize(id uint64) {
	switch h.behavior {
	case behaviorNoResponse:
		return
	case behaviorWrongID:
		h.respond(999999, helperInitializeResult{ProtocolVersion: 1, Capabilities: helperCaps("conftest", "Conformance Helper")}, nil)
		return
	case behaviorDuplicate:
		frame := h.responseFrame(id, helperInitializeResult{ProtocolVersion: 1, Capabilities: helperCaps("conftest", "Conformance Helper")}, nil)
		_, _ = os.Stdout.Write(append(frame, frame...))
		return
	case behaviorNoise:
		fmt.Fprint(os.Stdout, "not a protocol frame\n")
	case behaviorWrongJSONRPC:
		fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"1.0\",\"id\":%d,\"result\":{}}\n", id)
		return
	case behaviorMalformedResp:
		fmt.Fprint(os.Stdout, "not json\n")
		return
	case behaviorMissingErrMsg:
		// A JSON-RPC error without a message member: the strict parser
		// must reject the envelope before any fixture case can pass on
		// its code alone.
		fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32600}}\n", id)
		return
	case behaviorNonstringErrMsg:
		fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32600,\"message\":42}}\n", id)
		return
	case behaviorScalarErrData:
		fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32600,\"message\":\"boom\",\"data\":42}}\n", id)
		return
	case behaviorOversizedResp:
		fmt.Fprint(os.Stdout, strings.Repeat(" ", 17<<20)+"\n")
		return
	case behaviorBoundaryResp:
		// A success result padded so the complete frame is exactly
		// maxFrameBytes including the newline — the wire maximum.
		result := map[string]any{
			"protocol_version": 1,
			"capabilities": map[string]any{
				"name":    "conftest",
				"display": "Conformance Helper",
				"targets": []any{map[string]any{"prefix": "conftest:"}},
			},
			"pad": "",
		}
		frame := map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
		base, err := json.Marshal(frame)
		if err != nil {
			os.Exit(1)
		}
		pad := maxFrameBytes - 1 - len(base)
		result["pad"] = strings.Repeat(" ", pad)
		frame["result"] = result
		out, err := json.Marshal(frame)
		if err != nil || len(out)+1 != maxFrameBytes {
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(append(out, '\n'))
		return
	case behaviorBadCaps:
		h.respond(id, helperInitializeResult{ProtocolVersion: 1, Capabilities: helperCaps("", "Conformance Helper")}, nil)
		return
	case behaviorWrongVersion:
		h.respond(id, helperInitializeResult{ProtocolVersion: 2, Capabilities: helperCaps("conftest", "Conformance Helper")}, nil)
		return
	}
	h.respond(id, helperInitializeResult{ProtocolVersion: 1, Capabilities: helperCaps("conftest", "Conformance Helper")}, nil)
}

// respondID answers with a JSON-RPC error, echoing the id when it is
// valid and null otherwise, matching the canonical SDK.
func (h *helper) respondID(id *uint64, code int, message string) {
	h.respondPtr(id, nil, &wireError{Code: code, Message: message})
}

// respond writes one success response frame.
func (h *helper) respond(id uint64, result any, rpcErr *wireError) {
	h.respondPtr(&id, result, rpcErr)
}

func (h *helper) respondPtr(id *uint64, result any, rpcErr *wireError) {
	_, _ = os.Stdout.Write(h.responseFramePtr(id, result, rpcErr))
}

// responseFrame builds one complete response frame.
func (h *helper) responseFrame(id uint64, result any, rpcErr *wireError) []byte {
	return h.responseFramePtr(&id, result, rpcErr)
}

func (h *helper) responseFramePtr(id *uint64, result any, rpcErr *wireError) []byte {
	frame := wireResponse{JSONRPC: "2.0", ID: id, Error: rpcErr}
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

// readHelperFrame reads one LF-terminated frame, bounding it at
// maxFrameBytes.
func readHelperFrame(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if err == bufio.ErrBufferFull {
		return nil, fmt.Errorf("oversized frame")
	}
	return line, err
}

// parseHelperID parses an id member: a valid unsigned integer id, or
// nil with validID false for anything else (strings, floats, negatives,
// null — the reply must then carry id null, mirroring the SDK's
// Number.isInteger check).
func parseHelperID(raw json.RawMessage) (*uint64, bool) {
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

// appendLine appends one line to a marker file, flushing each write, so
// tests can observe child events deterministically.
func appendLine(path, line string) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(line + "\n")
	_ = file.Close()
}
