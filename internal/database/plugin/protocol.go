// Package plugin hosts external database driver plugins: child processes
// speaking the perk/v1 JSON-RPC stdio protocol. The Loader spawns and
// handshakes each plugin, registers a database.Shim for it, and routes
// every driver operation through a bounded concurrent RPC client to the
// child process.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// ProtocolVersion is the perk/v1 wire protocol version this host speaks.
// A plugin whose initialize result carries a different version is
// rejected at handshake, before registration.
const ProtocolVersion = 1

// MaxFrameBytes bounds one protocol frame — a UTF-8 JSON object plus a
// trailing newline — on the wire. A frame that does not fit is oversized
// and terminates the child.
const MaxFrameBytes = 16 << 20

// RPCErrorCanceled is the perk/v1 error code for a canceled operation;
// the host maps it exactly to context.Canceled.
const RPCErrorCanceled = -32800

// Kind classifies a plugin operation error. It is the Go-side mirror of
// the wire's error data.kind and of the Node SDK's ErrorKind constants.
type Kind string

// Stable operation-error kinds. Unknown or blank kinds normalize to
// KindOperation.
const (
	KindValidation     Kind = "validation"
	KindAuthentication Kind = "authentication"
	KindConnection     Kind = "connection"
	KindOperation      Kind = "operation"
	KindUnsupported    Kind = "unsupported"
	KindCancelled      Kind = "cancelled"
	KindProtocol       Kind = "protocol"
	KindPluginCrash    Kind = "plugin_crash"
)

// shortCallTimeout bounds RPC calls that have no caller context:
// interface-without-context calls — target building and session close.
// Session operations forward the caller's context unchanged.
const shortCallTimeout = 5 * time.Second

// workbenchVersion is advertised in the initialize handshake so plugins
// can adapt to the host's feature level. Bump it when the host gains
// protocol-relevant behavior.
const workbenchVersion = "perk-workbench 0.1.0"

// perk/v1 method names.
const (
	methodInitialize                 = "perk/v1/initialize"
	methodBuildTarget                = "perk/v1/build_target"
	methodOpen                       = "perk/v1/open"
	methodClose                      = "perk/v1/close"
	methodCancel                     = "perk/v1/cancel"
	methodExecute                    = "perk/v1/execute"
	methodExecuteReadOnly            = "perk/v1/execute_read_only"
	methodValidate                   = "perk/v1/validate"
	methodListSchema                 = "perk/v1/list_schema"
	methodTableInfo                  = "perk/v1/table_info"
	methodListIndexes                = "perk/v1/list_indexes"
	methodCreateIndex                = "perk/v1/create_index"
	methodReplaceIndex               = "perk/v1/replace_index"
	methodDropIndex                  = "perk/v1/drop_index"
	methodListForeignKeys            = "perk/v1/list_foreign_keys"
	methodListReferencingForeignKeys = "perk/v1/list_referencing_foreign_keys"
	methodListForeignKeysAll         = "perk/v1/list_foreign_keys_all"
	methodListIndexesAll             = "perk/v1/list_indexes_all"
	methodCreateForeignKey           = "perk/v1/create_foreign_key"
	methodReplaceForeignKey          = "perk/v1/replace_foreign_key"
	methodDropForeignKey             = "perk/v1/drop_foreign_key"
	methodAlterColumn                = "perk/v1/alter_column"
	methodDropColumn                 = "perk/v1/drop_column"
	methodAddColumn                  = "perk/v1/add_column"
	methodBrowseTable                = "perk/v1/browse_table"
	methodRowWrite                   = "perk/v1/row_write"
	methodDocumentWrite              = "perk/v1/document_write"
	methodWorkspaceView              = "perk/v1/workspace_view"
)

// request is the host-to-plugin envelope; params is a marshaled DTO.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// notification is a host-to-plugin envelope without an id; the host uses
// it only for perk/v1/cancel.
type notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// response is the plugin-to-host envelope. Exactly one of Result or Error
// is set.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// rpcError is a plugin operation error. Data is the optional structured
// provenance object (kind/plugin/method); it is kept raw so malformed
// non-object data can never make an otherwise valid operation error
// terminal.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// rpcErrorData is the optional structured provenance in an error's data
// object. Kind classifies the failure; Plugin and Method are advisory
// only — the host overrides them with its own method and the identity
// retained from the initialize handshake. Hint and SuggestedStatement
// are optional non-control advisory guidance: the host renders them
// separately from the error and never acts on them.
type rpcErrorData struct {
	Kind               string `json:"kind"`
	Plugin             string `json:"plugin"`
	Method             string `json:"method"`
	Hint               string `json:"hint"`
	SuggestedStatement string `json:"suggested_statement"`
}

// initializeParams carries the host's protocol version and identity.
type initializeParams struct {
	ProtocolVersion  int    `json:"protocol_version"`
	WorkbenchVersion string `json:"workbench_version"`
}

// initializeResult is the plugin's handshake reply: the protocol version
// it speaks and its driver capabilities.
type initializeResult struct {
	ProtocolVersion int                   `json:"protocol_version"`
	Capabilities    database.Capabilities `json:"capabilities"`
}

// buildTargetResult is the plugin's target serialization of one
// connection form.
type buildTargetResult struct {
	Target string `json:"target"`
	OK     bool   `json:"ok"`
}

type openParams struct {
	Target string `json:"target"`
}

type openResult struct {
	SessionID uint64                 `json:"session_id"`
	Info      sharedsql.DatabaseInfo `json:"info"`
}

type sessionParams struct {
	SessionID uint64 `json:"session_id"`
}

type statementParams struct {
	SessionID uint64 `json:"session_id"`
	Statement string `json:"statement"`
}

type tableParams struct {
	SessionID uint64 `json:"session_id"`
	Table     string `json:"table"`
}

type indexChangeParams struct {
	SessionID uint64                `json:"session_id"`
	Table     string                `json:"table"`
	Change    sharedsql.IndexChange `json:"change"`
}

type replaceIndexParams struct {
	SessionID uint64                `json:"session_id"`
	Table     string                `json:"table"`
	OldName   string                `json:"old_name"`
	Change    sharedsql.IndexChange `json:"change"`
}

type dropParams struct {
	SessionID uint64 `json:"session_id"`
	Table     string `json:"table"`
	Name      string `json:"name"`
}

type foreignKeyChangeParams struct {
	SessionID uint64                     `json:"session_id"`
	Table     string                     `json:"table"`
	Change    sharedsql.ForeignKeyChange `json:"change"`
}

type replaceForeignKeyParams struct {
	SessionID uint64                     `json:"session_id"`
	Table     string                     `json:"table"`
	OldName   string                     `json:"old_name"`
	Change    sharedsql.ForeignKeyChange `json:"change"`
}

type columnChangeParams struct {
	SessionID uint64                 `json:"session_id"`
	Table     string                 `json:"table"`
	Change    sharedsql.ColumnChange `json:"change"`
}

type addColumnParams struct {
	SessionID uint64              `json:"session_id"`
	Table     string              `json:"table"`
	Def       sharedsql.ColumnDef `json:"def"`
}

type browseParams struct {
	SessionID uint64                  `json:"session_id"`
	Table     string                  `json:"table"`
	Options   sharedsql.BrowseOptions `json:"options"`
}

type rowWriteParams struct {
	SessionID uint64                    `json:"session_id"`
	Request   sharedsql.RowWriteRequest `json:"request"`
}

type documentWriteParams struct {
	SessionID uint64                         `json:"session_id"`
	Request   sharedsql.DocumentWriteRequest `json:"request"`
}

type workspaceViewParams struct {
	SessionID uint64                        `json:"session_id"`
	ViewID    string                        `json:"view_id"`
	Target    sharedsql.WorkspaceViewTarget `json:"target"`
}

// cancelParams identifies the request a cancel notification targets.
type cancelParams struct {
	ID uint64 `json:"id"`
}

// Error is a structured plugin operation error with stable provenance:
// the JSON-RPC code and message, the normalized Kind, and the host-side
// Method and Plugin identity. Operation errors are never terminal client
// failures; inspect the fields with errors.As. Hint and
// SuggestedStatement are optional advisory guidance carried verbatim
// from the plugin's error data: the host renders them separately from
// the error (never merged into Error's text) and never executes a
// suggested statement.
type Error struct {
	Code    int
	Message string
	Kind    Kind
	Plugin  string
	Method  string
	// Hint explains the failure; empty when the plugin sent none.
	Hint string
	// SuggestedStatement is a statement the user may try instead;
	// empty when the plugin sent none. Advisory only — never executed
	// by the host.
	SuggestedStatement string
}

// Error renders the concise stable operation-error text. Method
// constants already carry the perk/v1 prefix, so the method is rendered
// exactly once.
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s (code %d)", e.Method, e.Message, e.Code)
}

// rpcErrorToGoError maps a plugin operation error onto a Go error. Code
// RPCErrorCanceled maps exactly to context.Canceled — regardless of any
// data — and any other code becomes a structured *Error: the host method
// and host-known plugin identity are authoritative over the child's
// data, and the plugin is empty before the initialize handshake.
func rpcErrorToGoError(method, plugin string, rpcErr *rpcError) error {
	if rpcErr.Code == RPCErrorCanceled {
		return context.Canceled
	}
	hint, suggested := errorDataAdvisories(rpcErr.Data)
	return &Error{
		Code:               rpcErr.Code,
		Message:            rpcErr.Message,
		Kind:               errorDataKind(rpcErr.Data),
		Plugin:             plugin,
		Method:             method,
		Hint:               hint,
		SuggestedStatement: suggested,
	}
}

// errorDataAdvisories extracts the plugin's optional advisory fields from
// the error data object. Omitted, null, blank, or non-string values are
// ignored (empty), mirroring errorDataKind's tolerance: malformed data
// can never change the error's identity or message, only optionally
// enrich the separately rendered guidance.
func errorDataAdvisories(raw json.RawMessage) (hint, suggested string) {
	if len(raw) == 0 {
		return "", ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", ""
	}
	for _, field := range []struct {
		key string
		dst *string
	}{
		{key: "hint", dst: &hint},
		{key: "suggested_statement", dst: &suggested},
	} {
		rawField, ok := fields[field.key]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(rawField, &text); err != nil || text == "" {
			continue
		}
		*field.dst = text
	}
	return hint, suggested
}

// errorDataKind extracts the plugin's kind claim from the optional error
// data object. Omitted, null, or malformed data — including non-object
// data and non-string kind fields — is ignored and normalizes to
// KindOperation, never a terminal failure.
func errorDataKind(raw json.RawMessage) Kind {
	if len(raw) == 0 {
		return KindOperation
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return KindOperation
	}
	rawKind, ok := fields["kind"]
	if !ok {
		return KindOperation
	}
	var kind string
	if err := json.Unmarshal(rawKind, &kind); err != nil {
		return KindOperation
	}
	return normalizeKind(kind)
}

// normalizeKind maps a plugin kind claim onto the stable enum: unknown
// or blank kinds are operation errors.
func normalizeKind(kind string) Kind {
	switch Kind(kind) {
	case KindValidation, KindAuthentication, KindConnection, KindUnsupported,
		KindCancelled, KindProtocol, KindPluginCrash:
		return Kind(kind)
	default:
		return KindOperation
	}
}
