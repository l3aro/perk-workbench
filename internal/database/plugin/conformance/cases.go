package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stable case names, fixed in JSON and human output.
const (
	caseInitialize          = "initialize"
	caseWrongJSONRPC        = "wrong_jsonrpc"
	caseStringID            = "string_id"
	caseFloatID             = "float_id"
	caseUnknownMethod       = "unknown_method"
	caseCancelNotification  = "cancel_notification"
	caseRequestBeforeInit   = "request_before_initialize"
	caseTwoRequestsOneWrite = "two_requests_one_write"
	caseCancelUnknownID     = "cancel_unknown_id"
	caseMalformedJSON       = "malformed_json_terminates"
	caseNonObjectJSON       = "non_object_json_terminates"
	caseInvalidUTF8         = "invalid_utf8_terminates"
	caseEOFInput            = "eof_input_no_response"
	caseOversizedInput      = "oversized_input_terminates"
	caseExactMaxFrame       = "exact_max_frame"
	caseCleanEOFShutdown    = "clean_eof_shutdown"
)

// cases builds the conformance suite. Every request and notification
// frame comes from the canonical fixtures (derived frames keep their
// fixture origin); only deliberately invalid input frames, which cannot
// exist as fixtures, are constructed here. The suite never invokes
// build_target, open, or any session RPC, so a transport-only plugin
// needs no backend. The canceled-code (-32800) verification is
// deliberately absent: it requires a deterministic in-flight cancellable
// request, which only session handlers can provide, and the suite must
// not require backend or session semantics from a transport-only
// plugin; cancellation is exercised at the transport level (id-less
// cancel notifications produce no response and unknown cancel ids never
// kill the child).
func (e *Engine) cases() []Case {
	quiet := e.Quiet
	init := e.fixtures["request-initialize.json"]
	cancel := e.fixtures["notification-cancel.json"]
	unknown := e.fixtures["invalid-request-unknown-method.json"]
	return []Case{
		{
			Name: caseInitialize,
			Run: func(child *Child, until time.Time) error {
				if err := sendAndExpectInitialize(child, until, init); err != nil {
					return err
				}
				return child.ExpectQuiet(until, quiet)
			},
		},
		{
			Name: caseWrongJSONRPC,
			Run: func(child *Child, until time.Time) error {
				if err := child.SendFixture(e.fixtures["invalid-request-jsonrpc.json"]); err != nil {
					return err
				}
				return e.expectErrorCode(child, until, "invalid-request-jsonrpc.json")
			},
		},
		{
			Name: caseStringID,
			Run: func(child *Child, until time.Time) error {
				if err := child.SendFixture(e.fixtures["invalid-request-string-id.json"]); err != nil {
					return err
				}
				return e.expectErrorCode(child, until, "invalid-request-string-id.json")
			},
		},
		{
			Name: caseFloatID,
			Run: func(child *Child, until time.Time) error {
				if err := child.SendFixture(e.fixtures["invalid-request-float-id.json"]); err != nil {
					return err
				}
				return e.expectErrorCode(child, until, "invalid-request-float-id.json")
			},
		},
		{
			Name: caseUnknownMethod,
			Run: func(child *Child, until time.Time) error {
				if err := sendAndExpectInitialize(child, until, init); err != nil {
					return err
				}
				// The canonical invalid request: an unknown method the
				// manifest declares must be answered with -32601.
				if err := child.SendFixture(unknown); err != nil {
					return err
				}
				return e.expectErrorCode(child, until, "invalid-request-unknown-method.json")
			},
		},
		{
			Name: caseCancelNotification,
			Run: func(child *Child, until time.Time) error {
				// The canonical id-less cancel notification: the child
				// must answer nothing and stay usable.
				if err := child.SendFixture(cancel); err != nil {
					return err
				}
				if err := child.ExpectSilent(until, quiet); err != nil {
					return err
				}
				if err := sendAndExpectInitialize(child, until, init); err != nil {
					return err
				}
				return child.ExpectQuiet(until, quiet)
			},
		},
		{
			Name: caseRequestBeforeInit,
			Run: func(child *Child, until time.Time) error {
				// A request before the initialize handshake must get a
				// JSON-RPC error — never a result — and the child must
				// remain usable. The unknown-method fixture carries no
				// session semantics, so a transport-only plugin can
				// answer it without a backend.
				if err := child.SendFixture(unknown); err != nil {
					return err
				}
				frame, err := child.Expect(until)
				if err != nil {
					return err
				}
				if frame.Error == nil {
					return &CaseError{CategoryBehavior, "request before initialize: expected a JSON-RPC error, got a result"}
				}
				if err := sendAndExpectInitialize(child, until, init); err != nil {
					return err
				}
				return child.ExpectQuiet(until, quiet)
			},
		},
		{
			Name: caseTwoRequestsOneWrite,
			Run: func(child *Child, until time.Time) error {
				initID, _ := frameID(init)
				unknownID, _ := frameID(unknown)
				// Two requests in one write: the child must process them
				// in order and answer each with its own id.
				if err := child.SendBatch(init, unknown); err != nil {
					return err
				}
				frame, err := child.Expect(until)
				if err != nil {
					return err
				}
				if frame.ID != initID {
					return &CaseError{CategoryBehavior, fmt.Sprintf(
						"first response id %d, want %d (in-order processing)", frame.ID, initID)}
				}
				if err := expectInitializeResult(frame); err != nil {
					return err
				}
				frame, err = child.Expect(until)
				if err != nil {
					return err
				}
				if frame.ID != unknownID {
					return &CaseError{CategoryBehavior, fmt.Sprintf(
						"second response id %d, want %d", frame.ID, unknownID)}
				}
				if frame.Error == nil {
					return &CaseError{CategoryBehavior, fmt.Sprintf(
						"second request: expected error code %d, got a result", e.codes["invalid-request-unknown-method.json"])}
				}
				if frame.Error.Code != e.codes["invalid-request-unknown-method.json"] {
					return &CaseError{CategoryBehavior, fmt.Sprintf(
						"second request: error code %d, want %d", frame.Error.Code, e.codes["invalid-request-unknown-method.json"])}
				}
				return child.ExpectQuiet(until, quiet)
			},
		},
		{
			Name: caseCancelUnknownID,
			Run: func(child *Child, until time.Time) error {
				// A generated id-less cancel for an id no request
				// carries: no response, and the child stays usable.
				frame, err := withCancelID(cancel, 7)
				if err != nil {
					return &CaseError{CategoryBehavior, fmt.Sprintf("building the cancel notification: %v", err)}
				}
				if err := child.SendRaw(frame); err != nil {
					return err
				}
				if err := child.ExpectSilent(until, quiet); err != nil {
					return err
				}
				if err := sendAndExpectInitialize(child, until, init); err != nil {
					return err
				}
				return child.ExpectQuiet(until, quiet)
			},
		},
		e.terminalInput(caseMalformedJSON, []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"perk/v1/initialize\",\"params\":{\"protocol_version\":1}\n")),
		e.terminalInput(caseNonObjectJSON, []byte("[1,2,3]\n")),
		e.terminalInput(caseInvalidUTF8, append([]byte{0xff, 0xfe}, []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"perk/v1/initialize\"}\n")...)),
		{
			Name: caseEOFInput,
			Run: func(child *Child, until time.Time) error {
				// EOF before any request: the child must produce no
				// protocol response and terminate.
				if err := child.CloseInput(); err != nil {
					return err
				}
				if err := child.ExpectSilent(until, quiet); err != nil {
					return err
				}
				return child.ExpectExit(until)
			},
		},
		{
			Name: caseOversizedInput,
			Run: func(child *Child, until time.Time) error {
				// A frame over 16 MiB is terminal with no response. The
				// frame is built from the canonical unknown-method
				// fixture, whose params a plugin never validates for an
				// unknown method.
				frame, err := paddedInitialize(unknown, maxFrameBytes+1)
				if err != nil {
					return &CaseError{CategoryBehavior, fmt.Sprintf("building the oversized frame: %v", err)}
				}
				if err := child.SendFixture(frame); err != nil {
					return err
				}
				if err := child.ExpectSilent(until, quiet); err != nil {
					return err
				}
				return child.ExpectExit(until)
			},
		},
		{
			Name: caseExactMaxFrame,
			Run: func(child *Child, until time.Time) error {
				// An exact 16 MiB frame including the newline is the
				// wire maximum: it must be accepted and answered. The
				// frame is built from the canonical unknown-method
				// fixture — whose params a plugin never validates for an
				// unknown method — so the pad proves the frame bound
				// without weakening initialize params validation; the
				// manifest-declared -32601 reply with the correct id is
				// required.
				if err := sendAndExpectInitialize(child, until, init); err != nil {
					return err
				}
				frame, err := paddedInitialize(unknown, maxFrameBytes)
				if err != nil {
					return &CaseError{CategoryBehavior, fmt.Sprintf("building the exact-max frame: %v", err)}
				}
				if err := child.SendFixture(frame); err != nil {
					return err
				}
				response, err := child.Expect(until)
				if err != nil {
					return err
				}
				unknownID, _ := frameID(unknown)
				if response.ID != unknownID {
					return &CaseError{CategoryBehavior, fmt.Sprintf(
						"exact-max response id %d, want %d", response.ID, unknownID)}
				}
				if response.Error == nil {
					return &CaseError{CategoryBehavior, fmt.Sprintf(
						"exact-max frame: expected error code %d, got a result", e.codes["invalid-request-unknown-method.json"])}
				}
				if response.Error.Code != e.codes["invalid-request-unknown-method.json"] {
					return &CaseError{CategoryBehavior, fmt.Sprintf(
						"exact-max frame: error code %d, want %d", response.Error.Code, e.codes["invalid-request-unknown-method.json"])}
				}
				return child.ExpectQuiet(until, quiet)
			},
		},
		{
			Name: caseCleanEOFShutdown,
			Run: func(child *Child, until time.Time) error {
				if err := sendAndExpectInitialize(child, until, init); err != nil {
					return err
				}
				// Clean EOF shutdown: stdin closes, the child exits 0
				// within the bound, and answers nothing further.
				if err := child.CloseInput(); err != nil {
					return err
				}
				if err := child.ExpectExit(until); err != nil {
					return err
				}
				if status := child.ExitStatus(); status != 0 {
					return &CaseError{CategoryBehavior, fmt.Sprintf(
						"clean EOF shutdown exited with status %d, want 0", status)}
				}
				return nil
			},
		},
	}
}

// expectErrorCode requires the next response to be a JSON-RPC error
// with the manifest-declared code for the given fixture, then a quiet
// window. The manifest's reject text is carried into failure messages
// so drift is visible.
func (e *Engine) expectErrorCode(child *Child, until time.Time, file string) error {
	entry, _ := e.entry(file)
	want := e.codes[file]
	label := file
	if entry.Reject != "" {
		label = fmt.Sprintf("%s (manifest: %s)", file, entry.Reject)
	}
	frame, err := child.Expect(until)
	if err != nil {
		return err
	}
	if frame.Error == nil {
		return &CaseError{CategoryBehavior, fmt.Sprintf(
			"%s: expected a JSON-RPC error (code %d), got a result", label, want)}
	}
	if frame.Error.Code != want {
		return &CaseError{CategoryBehavior, fmt.Sprintf(
			"%s: error code %d, want %d", label, frame.Error.Code, want)}
	}
	return child.ExpectQuiet(until, e.Quiet)
}

// terminalInput is the shared case for deliberately invalid input
// frames: the child must answer nothing at all and terminate.
func (e *Engine) terminalInput(name string, frame []byte) Case {
	quiet := e.Quiet
	return Case{Name: name, Run: func(child *Child, until time.Time) error {
		if err := child.SendRaw(frame); err != nil {
			return err
		}
		if err := child.ExpectSilent(until, quiet); err != nil {
			return err
		}
		return child.ExpectExit(until)
	}}
}

// withCancelID rewrites the params.id of a perk/v1/cancel notification
// frame to a fresh id, keeping the canonical envelope.
func withCancelID(frame []byte, id uint64) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(frame, &obj); err != nil {
		return nil, err
	}
	params, ok := obj["params"].(map[string]any)
	if !ok {
		return nil, errors.New("cancel notification has no params object")
	}
	params["id"] = id
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// paddedInitialize rebuilds a request frame with a whitespace pad in
// params so the complete frame — newline included — is exactly want
// bytes, the wire's maximum (or just over it, for the oversized case).
// It is applied to the canonical unknown-method fixture, whose params a
// plugin never validates for an unknown method, so the pad exercises
// the frame bound without weakening initialize params validation.
func paddedInitialize(frame []byte, want int) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(frame, &obj); err != nil {
		return nil, err
	}
	params, ok := obj["params"].(map[string]any)
	if !ok {
		return nil, errors.New("initialize fixture has no params object")
	}
	params["pad"] = ""
	base, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	pad := want - 1 - len(base)
	if pad < 0 {
		return nil, fmt.Errorf("frame base is %d bytes, want a %d-byte frame", len(base)+1, want)
	}
	params["pad"] = strings.Repeat(" ", pad)
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	if len(out) != want {
		return nil, fmt.Errorf("frame is %d bytes, want %d", len(out), want)
	}
	return out, nil
}
