package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// TestRPCErrorToGoError_exactFormatting pins the concise stable
// operation-error text: the method constant already carries the perk/v1
// prefix, so it renders exactly once.
func TestRPCErrorToGoError_exactFormatting(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		plugin string
		rpcErr *rpcError
		want   string
	}{
		{
			name:   "plain operation error",
			method: methodExecute,
			plugin: "pluginkv",
			rpcErr: &rpcError{Code: -32000, Message: "boom"},
			want:   "perk/v1/execute: boom (code -32000)",
		},
		{
			name:   "error with data still renders once",
			method: methodListSchema,
			plugin: "pluginkv",
			rpcErr: &rpcError{
				Code:    -32001,
				Message: "auth denied",
				Data:    json.RawMessage(`{"kind":"authentication","method":"perk/v1/steal"}`),
			},
			want: "perk/v1/list_schema: auth denied (code -32001)",
		},
		{
			name:   "empty message keeps the shape",
			method: methodOpen,
			plugin: "pluginkv",
			rpcErr: &rpcError{Code: -32000},
			want:   "perk/v1/open:  (code -32000)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := rpcErrorToGoError(test.method, test.plugin, test.rpcErr)
			if err.Error() != test.want {
				t.Fatalf("error text = %q, want %q", err.Error(), test.want)
			}
			if strings.Contains(err.Error(), "perk/v1/perk/v1") {
				t.Fatalf("error text %q renders the perk/v1 prefix twice", err.Error())
			}
		})
	}
}

// TestRPCErrorToGoError_canceled: code -32800 maps exactly to
// context.Canceled, regardless of any data on the error.
func TestRPCErrorToGoError_canceled(t *testing.T) {
	for _, test := range []struct {
		name string
		data json.RawMessage
	}{
		{name: "without data"},
		{name: "with cancelled data", data: json.RawMessage(`{"kind":"cancelled","plugin":"spoofed","method":"perk/v1/nope"}`)},
		{name: "with malformed data", data: json.RawMessage(`"not an object"`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := rpcErrorToGoError(methodExecute, "pluginkv", &rpcError{Code: RPCErrorCanceled, Message: "canceled", Data: test.data})
			if err != context.Canceled {
				t.Fatalf("error = %v, want exactly context.Canceled", err)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("errors.Is(err, context.Canceled) = false for %v", err)
			}
		})
	}
}

// TestRPCErrorToGoError_kindNormalization: omitted, null, unknown,
// blank, and malformed kind claims all normalize to KindOperation; every
// stable kind passes through.
func TestRPCErrorToGoError_kindNormalization(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want Kind
	}{
		{name: "data omitted", data: "", want: KindOperation},
		{name: "null data", data: `null`, want: KindOperation},
		{name: "string data", data: `"boom"`, want: KindOperation},
		{name: "number data", data: `42`, want: KindOperation},
		{name: "array data", data: `[1,2]`, want: KindOperation},
		{name: "object without kind", data: `{"plugin":"x"}`, want: KindOperation},
		{name: "unknown kind", data: `{"kind":"bogus"}`, want: KindOperation},
		{name: "blank kind", data: `{"kind":""}`, want: KindOperation},
		{name: "non-string kind", data: `{"kind":42}`, want: KindOperation},
		{name: "non-string plugin does not mask the kind", data: `{"kind":"connection","plugin":123}`, want: KindConnection},
		{name: "validation", data: `{"kind":"validation"}`, want: KindValidation},
		{name: "authentication", data: `{"kind":"authentication"}`, want: KindAuthentication},
		{name: "connection", data: `{"kind":"connection"}`, want: KindConnection},
		{name: "operation", data: `{"kind":"operation"}`, want: KindOperation},
		{name: "unsupported", data: `{"kind":"unsupported"}`, want: KindUnsupported},
		{name: "cancelled", data: `{"kind":"cancelled"}`, want: KindCancelled},
		{name: "protocol", data: `{"kind":"protocol"}`, want: KindProtocol},
		{name: "plugin_crash", data: `{"kind":"plugin_crash"}`, want: KindPluginCrash},
	} {
		t.Run(test.name, func(t *testing.T) {
			rpcErr := &rpcError{Code: -32000, Message: "boom"}
			if test.data != "" {
				rpcErr.Data = json.RawMessage(test.data)
			}
			err := rpcErrorToGoError(methodExecute, "pluginkv", rpcErr)
			var pluginErr *Error
			if !errors.As(err, &pluginErr) {
				t.Fatalf("error = %T %v, want *plugin.Error", err, err)
			}
			if pluginErr.Kind != test.want {
				t.Fatalf("Kind = %q, want %q", pluginErr.Kind, test.want)
			}
		})
	}
}

// TestRPCErrorToGoError_advisoryGuidance: optional hint and
// suggested_statement survive the wire mapping as separate fields and
// never enter the error text — the identity used for matching and
// diagnostics stays exactly the stable format. Omitted, null, blank,
// and non-string advisory values are ignored (empty).
func TestRPCErrorToGoError_advisoryGuidance(t *testing.T) {
	for _, test := range []struct {
		name          string
		data          string
		wantHint      string
		wantSuggested string
	}{
		{name: "advisories preserved", data: `{"kind":"operation","hint":"GET accepts strings, but user:1 is a hash","suggested_statement":"HGETALL user:1"}`, wantHint: "GET accepts strings, but user:1 is a hash", wantSuggested: "HGETALL user:1"},
		{name: "data omitted", data: "", wantHint: "", wantSuggested: ""},
		{name: "null data", data: `null`, wantHint: "", wantSuggested: ""},
		{name: "string data", data: `"boom"`, wantHint: "", wantSuggested: ""},
		{name: "array data", data: `[1,2]`, wantHint: "", wantSuggested: ""},
		{name: "blank hint", data: `{"hint":""}`, wantHint: "", wantSuggested: ""},
		{name: "blank suggested_statement", data: `{"suggested_statement":""}`, wantHint: "", wantSuggested: ""},
		{name: "non-string hint", data: `{"hint":42}`, wantHint: "", wantSuggested: ""},
		{name: "non-string suggested_statement", data: `{"suggested_statement":["HGETALL"]}`, wantHint: "", wantSuggested: ""},
		{name: "hint only", data: `{"hint":"the key is a hash"}`, wantHint: "the key is a hash", wantSuggested: ""},
		{name: "suggested only", data: `{"suggested_statement":"HGETALL user:1"}`, wantHint: "", wantSuggested: "HGETALL user:1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rpcErr := &rpcError{Code: -32000, Message: "redis: WRONGTYPE Operation against a key holding the wrong kind of value"}
			if test.data != "" {
				rpcErr.Data = json.RawMessage(test.data)
			}
			err := rpcErrorToGoError(methodExecute, "pluginkv", rpcErr)
			var pluginErr *Error
			if !errors.As(err, &pluginErr) {
				t.Fatalf("error = %T %v, want *plugin.Error", err, err)
			}
			if pluginErr.Hint != test.wantHint {
				t.Fatalf("Hint = %q, want %q", pluginErr.Hint, test.wantHint)
			}
			if pluginErr.SuggestedStatement != test.wantSuggested {
				t.Fatalf("SuggestedStatement = %q, want %q", pluginErr.SuggestedStatement, test.wantSuggested)
			}
			// Advisories never enter the error identity/message.
			text := err.Error()
			if strings.Contains(text, test.wantHint) && test.wantHint != "" {
				t.Fatalf("error text %q embeds the hint %q", text, test.wantHint)
			}
			if strings.Contains(text, test.wantSuggested) && test.wantSuggested != "" {
				t.Fatalf("error text %q embeds the suggested statement %q", text, test.wantSuggested)
			}
			if text != "perk/v1/execute: redis: WRONGTYPE Operation against a key holding the wrong kind of value (code -32000)" {
				t.Fatalf("error text = %q, want the stable operation-error format", text)
			}
		})
	}
}

// TestClient_structuredErrorProvenance drives the full wire path: a
// structured RPC error with spoofed data.plugin/method surfaces as a
// *Error carrying the host method and the identity retained via
// SetPlugin, with the child's kind claim normalized.
func TestClient_structuredErrorProvenance(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "rpc_error")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_CODE", "-32001")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_MESSAGE", "auth denied")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_DATA", `{"kind":"authentication","plugin":"spoofed","method":"perk/v1/steal"}`)
	client, err := spawn(executable, spawnArgs...)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = client.Close() }()
	client.SetPlugin("pluginkv")

	var result sharedsql.Result
	err = client.Call(context.Background(), methodExecute, statementParams{Statement: "select 1"}, &result)
	if err == nil {
		t.Fatal("Call succeeded, want the structured operation error")
	}
	var pluginErr *Error
	if !errors.As(err, &pluginErr) {
		t.Fatalf("error = %T %v, want *plugin.Error", err, err)
	}
	if pluginErr.Code != -32001 || pluginErr.Message != "auth denied" {
		t.Fatalf("Code/Message = %d/%q, want -32001/auth denied", pluginErr.Code, pluginErr.Message)
	}
	if pluginErr.Kind != KindAuthentication {
		t.Fatalf("Kind = %q, want authentication", pluginErr.Kind)
	}
	if pluginErr.Plugin != "pluginkv" {
		t.Fatalf("Plugin = %q, want the host identity pluginkv, not the spoofed value", pluginErr.Plugin)
	}
	if pluginErr.Method != methodExecute {
		t.Fatalf("Method = %q, want the host method %q, not the spoofed value", pluginErr.Method, methodExecute)
	}
	if want := "perk/v1/execute: auth denied (code -32001)"; err.Error() != want {
		t.Fatalf("error text = %q, want %q", err.Error(), want)
	}
}

// TestLoad_operationErrorProvenance: through the real Loader the
// identity retained after the initialize handshake is authoritative on
// operation errors, and the plugin keeps serving after the failure.
func TestLoad_operationErrorProvenance(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "rpc_error")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_CODE", "-32002")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_MESSAGE", "not supported")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_DATA", `{"kind":"unsupported","plugin":"spoofed","method":"perk/v1/spoofed"}`)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var shim database.Shim
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		shim = s
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	service, err := shim.Open(context.Background(), "pluginkv:svc")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = service.Execute(context.Background(), "select 1")
	if err == nil {
		t.Fatal("Execute succeeded, want the structured operation error")
	}
	var pluginErr *Error
	if !errors.As(err, &pluginErr) {
		t.Fatalf("error = %T %v, want *plugin.Error", err, err)
	}
	if pluginErr.Plugin != "pluginkv" {
		t.Fatalf("Plugin = %q, want the handshake identity pluginkv", pluginErr.Plugin)
	}
	if pluginErr.Method != methodExecute {
		t.Fatalf("Method = %q, want %q", pluginErr.Method, methodExecute)
	}
	if pluginErr.Kind != KindUnsupported {
		t.Fatalf("Kind = %q, want unsupported", pluginErr.Kind)
	}
}

// TestLoad_initializeErrorBeforeIdentity: an RPC error on initialize
// itself is a structured error with the host method and an empty plugin
// — the child's self-reported identity is not trusted before the
// handshake succeeds.
func TestLoad_initializeErrorBeforeIdentity(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "rpc_error_initialize")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_CODE", "-32010")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_MESSAGE", "handshake rejected")
	t.Setenv("PERK_PLUGIN_RPC_ERROR_DATA", `{"kind":"protocol","plugin":"spoofed","method":"perk/v1/nope"}`)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), testEntries(executable), func(s database.Shim) error {
		t.Fatal("a rejected handshake must not register")
		return nil
	})
	t.Cleanup(func() { _ = loader.Close() })
	if len(errs) != 1 {
		t.Fatalf("Load errors = %v, want exactly one", errs)
	}
	var pluginErr *Error
	if !errors.As(errs[0], &pluginErr) {
		t.Fatalf("error = %T %v, want *plugin.Error", errs[0], errs[0])
	}
	if pluginErr.Plugin != "" {
		t.Fatalf("Plugin = %q, want empty before the handshake", pluginErr.Plugin)
	}
	if pluginErr.Method != methodInitialize {
		t.Fatalf("Method = %q, want %q", pluginErr.Method, methodInitialize)
	}
	if pluginErr.Kind != KindProtocol {
		t.Fatalf("Kind = %q, want protocol", pluginErr.Kind)
	}
	if want := "perk/v1/initialize: handshake rejected (code -32010)"; pluginErr.Error() != want {
		t.Fatalf("error text = %q, want %q", pluginErr.Error(), want)
	}
}
