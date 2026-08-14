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
// the host maps it to context.Canceled.
const RPCErrorCanceled = -32800

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

// rpcError is a plugin operation error.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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

// cancelParams identifies the request a cancel notification targets.
type cancelParams struct {
	ID uint64 `json:"id"`
}

// rpcErrorToGoError maps a plugin operation error onto a Go error. Code
// RPCErrorCanceled maps to context.Canceled; any other code wraps as an
// operation error naming the method — never a terminal client failure.
func rpcErrorToGoError(method string, rpcErr *rpcError) error {
	if rpcErr.Code == RPCErrorCanceled {
		return context.Canceled
	}
	return fmt.Errorf("perk/v1/%s: %s (code %d)", method, rpcErr.Message, rpcErr.Code)
}
