---
title: Plugins
eyebrow: Documentation / extension protocol
order: 50
lede: Author and manage database plugins for Perk Workbench — from wire protocol specification and SDKs to conformance testing and approval.
keywords: [plugins, Perk protocol, drivers, extension SDK, JSON-RPC, conformance, sha256]
---

## What a plugin is

A plugin is an out-of-process executable that speaks the Perk protocol to the workbench over standard I/O (`stdin` and `stdout`). The four built-in database backends — SQLite, MySQL, PostgreSQL, and MongoDB — are themselves built-in plugins: child processes of the same `perk-workbench` binary, launched with `--plugin sqlite`, `--plugin mysql`, `--plugin postgres`, or `--plugin mongodb`. External plugins are separate executables that you compile, pin, and run.

Nothing is auto-discovered. `$XDG_CONFIG_HOME/perk-workbench/config.json` is the explicit allowlist: a plugin runs only when its descriptor is listed there. A missing config file is materialized with the four built-ins in stable order:

```json
{
  "plugins": [
    {"builtin": "sqlite"},
    {"builtin": "mysql"},
    {"builtin": "postgres"},
    {"builtin": "mongodb"}
  ]
}
```

An external descriptor pins an executable by path with an optional SHA-256 digest, verified immediately before the child spawns:

```json
{
  "plugins": [
    {"builtin": "sqlite"},
    {"builtin": "postgres"},
    {"builtin": "mongodb"},
    {
      "path": "/home/alice/.local/bin/perk-redis",
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  ]
}
```

Remove a built-in line to disable that plugin; add a `path` entry to enable an external one. Config validation runs before any child starts: invalid descriptors, malformed lowercase digests, blank paths, and duplicates stop startup with a diagnostic.

### Plugin identity versus driver family

In Perk Workbench, a plugin's identity (`name`) is distinct from the database family it serves (`driver`):

- **`name`** is the unique identifier of the plugin instance (e.g. `sqlite`, `mysql-cloud`, `perk-redis`, `duckdb`). No two loaded plugins may have the same `name`.
- **`driver`** is the database family (e.g. `sqlite`, `mysql`, `postgres`, `mongodb`, `redis`). Multiple plugins can serve the same driver family (for instance, a local MySQL driver and a cloud-proxied MySQL driver).

Persisted connection profiles store the exact `name` of the plugin selected, ensuring that saved connections always route to the intended binary.

---

## Manage the plugin set

The workbench CLI provides subcommands to inspect, register, test, and maintain plugins:

```sh
perk-workbench plugin list
perk-workbench plugin inspect EXECUTABLE
perk-workbench plugin add EXECUTABLE
perk-workbench plugin add --approve SHA256 EXECUTABLE
perk-workbench plugin remove NAME_OR_PATH
perk-workbench plugin doctor
perk-workbench plugin test EXECUTABLE
```

Every command is scriptable: `--json` emits one machine-readable document on stdout, item-level failures are encoded in it, and diagnostics go to stderr only when no JSON document can be produced. Exit status is `0` on success, `1` on a plugin or operational failure, and `2` on a usage error.

- **`plugin list [--json]`** reads the config file using the same parser as startup, resolves each configured entry without spawning processes, and reports trust status (`unpinned`, or `pinned` with fingerprint). Any unresolvable entry fails the overall command with exit code `1`.
- **`plugin inspect [--json] EXECUTABLE`** executes the plugin through the real loader lifecycle — spawn, `perk/v1/initialize` handshake, and registration invariant checks — then shuts it down cleanly. Works for executables not yet configured. Pinned executables with altered bytes are rejected before spawn.
- **`plugin add [--json] EXECUTABLE`** is the preview stage: resolves the path, runs the inspect lifecycle, and calculates the canonical SHA-256 hash. The configuration file is **never touched**. Output ends with:
  ```text
  NOT ENABLED: rerun with --approve <fingerprint> to pin and enable this plugin
  ```
- **`plugin add --approve SHA256 EXECUTABLE`** is the mutating stage: re-verifies the inspect lifecycle and hash from scratch. If the supplied digest does not match the file's current bytes, the command fails closed. Only on an exact match is the plugin persisted atomically to `config.json` with its trust record set.
- **`plugin remove [--json] NAME_OR_PATH`** atomically removes a configured plugin and its trust record by matching either its configured name or its resolved canonical path. Ambiguous or unknown operands fail closed.
- **`plugin doctor [--json] [EXECUTABLE...]`** evaluates health across configured plugins (or specified paths), testing the full lifecycle: `resolve`, `pin-verify`, `initialize`, `register`, `trust`, and `shutdown`.
- **`plugin test [--json] EXECUTABLE`** runs the complete `perk/v1` conformance test suite (16 isolation cases), verifying protocol adherence against edge cases and framing boundaries.

---

## Trust and safety

A configured plugin is trusted executable code: the workbench spawns it with your OS privileges, so never add a plugin you do not trust. `config.json` is the allowlist, the SHA-256 pin makes a drifted external binary refuse to start, and every add/remove path is previewable or reversible — the two-stage `add` flow means nothing is enabled until you approve the exact fingerprint you saw.

---

## Perk v1

The Perk protocol boundary is what `inspect`, `doctor`, and `test` exercise. Plugins communicate over `perk-v1`: JSON-RPC 2.0 over newline-delimited JSON (NDJSON) on standard input and standard output.

### Transport and framing

1. **Stdio Streams**:
   - The host writes JSON-RPC request and notification frames to the child's `stdin`.
   - The child writes JSON-RPC response frames to its `stdout`.
   - `stderr` is passed through for diagnostics, user warnings, and debug logs.
2. **Framing**: Every frame is a single UTF-8 encoded JSON object terminated by a newline (`\n`). Blank lines are ignored.
3. **Maximum Frame Size**: A single frame on the wire (including the newline) cannot exceed **16 MiB** (`16,777,216` bytes). Any frame exceeding this bound is oversized and triggers immediate termination.
4. **The Golden Rule of Stdout**: **`stdout` belongs strictly and exclusively to the protocol.** Never print logging messages, debug info, or initialization banners to `stdout`. Any non-JSON text on `stdout` breaks the framing parser and terminates the plugin. Always send logs and errors to `stderr`.
5. **Numeric IDs**: Host-assigned JSON-RPC request IDs are 64-bit unsigned integers (`uint64`). Plugins must preserve the exact integer value in responses (do not convert to floating-point or string).

### Protocol envelopes

The protocol adheres to JSON-RPC 2.0:

#### Request envelope (host to plugin)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "perk/v1/execute",
  "params": {
    "session_id": 1,
    "statement": "SELECT * FROM users"
  }
}
```

#### Notification envelope (host to plugin)

Notifications have no `id` field. The host uses notifications exclusively for request cancellation (`perk/v1/cancel`):

```json
{
  "jsonrpc": "2.0",
  "method": "perk/v1/cancel",
  "params": {
    "id": 1
  }
}
```

#### Success response envelope (plugin to host)

Notice that rows are 2D arrays of nullable strings, and execution time is reported in nanoseconds (`duration_ns`):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "columns": ["id", "username"],
    "column_types": ["integer", "string"],
    "rows": [["1", "alice"], ["2", "bob"]],
    "untruncated_rows": [["1", "alice"], ["2", "bob"]],
    "rows_affected": 0,
    "has_more": false,
    "duration_ns": 12500000,
    "truncated": false
  }
}
```

#### Error response envelope (plugin to host)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32000,
    "message": "table users does not exist",
    "data": {
      "kind": "operation",
      "plugin": "sqlite",
      "method": "perk/v1/execute",
      "hint": "Check schema for existing tables",
      "suggested_statement": "SELECT name FROM sqlite_master WHERE type='table'"
    }
  }
}
```

### Error codes and structured error kinds

The host maps JSON-RPC error codes according to standard conventions:

| Code | Meaning | Usage |
| --- | --- | --- |
| `-32700` | Parse error | Received frame is not valid JSON. Terminates session. |
| `-32600` | Invalid request | Protocol invariant violation (e.g. non-integer ID, wrong jsonrpc, or request before initialize). |
| `-32601` | Method not found | Unknown or unadvertised method requested. |
| `-32602` | Invalid params | Missing or mistyped parameters. |
| `-32603` | Internal error | Unhandled runtime exception or crash inside plugin. |
| `-32800` | Canceled | Request was aborted by `perk/v1/cancel`. |
| `-32000` | Operation error | Structured database/domain failure with optional `data` payload. |

For `-32000` operation errors, the `data` object provides structured provenance and UI guidance:

- **`kind`** (string): One of the stable error classifications:
  - `validation`: Statement syntax, parameter range, or schema contract violation.
  - `authentication`: Bad credentials or denied access permissions.
  - `connection`: Database host unreachable, timeout, or lost socket.
  - `operation`: Query execution failure (e.g. constraint violation, table not found).
  - `unsupported`: Feature not supported by this database version or engine.
  - `cancelled`: Explicitly canceled operation.
  - `protocol`: Transport framing or serialization failure.
  - `plugin_crash`: Subprocess unexpectedly aborted.
- **`plugin`** (string): Plugin `name` originating the error.
- **`method`** (string): Method name where error occurred.
- **`hint`** (optional string): Advisory guidance shown in notifications/diagnostics.
- **`suggested_statement`** (optional string): Non-control statement suggested to resolve the error.

### Compatibility policy

The `perk/v1` protocol is defined under `protocol/perk-v1/`:
- **Additive changes are compatible**: Every object in the schema accepts unknown properties (`additionalProperties: true`). Implementations must ignore unrecognized fields.
- **Host speaks version 1**: The host advertises `protocol_version: 1` during initialize and requires the plugin to echo `protocol_version: 1`.
- **Breaking changes require perk/v2**: Changing required fields, error semantics, or frame bounds requires a new protocol namespace and version.

---

## Plugin capabilities

During the handshake (`perk/v1/initialize`), the plugin advertises its declarative capabilities:

```json
{
  "name": "redis",
  "display": "Redis",
  "driver": "redis",
  "targets": [
    {"prefix": "redis://", "keep_target": true},
    {"prefix": "rediss://", "keep_target": true}
  ],
  "form": {
    "fields": [
      {
        "key": "host",
        "title": "Host*",
        "kind": 0,
        "placeholder": "127.0.0.1",
        "default": "127.0.0.1",
        "validate": 1,
        "error": "host is required"
      },
      {
        "key": "port",
        "title": "Port*",
        "kind": 0,
        "placeholder": "6379",
        "default": "6379",
        "validate": 2,
        "error": "valid port 1-65535 required"
      },
      {
        "key": "password",
        "title": "Password",
        "kind": 1,
        "validate": 0
      }
    ]
  },
  "query_language": {
    "name": "Redis Commands",
    "editor_label": "CLI",
    "placeholder": "Enter Redis command (e.g. GET key, INFO, KEYS *)...",
    "lexer": "bash",
    "commands": [
      {"name": "GET", "usage": "GET key", "summary": "Get the value of a key"},
      {"name": "SET", "usage": "SET key value", "summary": "Set the string value of a key"},
      {"name": "KEYS", "usage": "KEYS pattern", "summary": "Find all keys matching the given pattern"}
    ]
  },
  "write_capabilities": {
    "row_writer": false
  },
  "workspace": {
    "standard_tabs": ["columns"],
    "custom_views": [
      {"id": "slowlog", "label": "Slow Log", "scopes": ["database"]},
      {"id": "clients", "label": "Connected Clients", "scopes": ["database"]}
    ]
  }
}
```

### Capabilities reference

| Field | Type | Description |
| --- | --- | --- |
| `name` | string (required) | Unique identifier of the plugin. |
| `display` | string (required) | Display label shown in connection dialogs and status bar. |
| `driver` | string (optional) | Database family name. Defaults to `name` if omitted. |
| `targets` | array (optional) | Target URL prefixes used for target routing (e.g. `redis://`, `sqlite:`). `keep_target: true` preserves prefix when passing target to `open`. |
| `form` | object (optional) | Declarative connection dialog form fields. |
| `query_language` | object (optional) | Syntax highlighter (`lexer`), editor label, and tab-completion command catalog. |
| `write_capabilities` | object (required) | Advertises write support: `row_writer: true` enables row insert/update/delete; `document` enables JSON document editing (MongoDB). |
| `workspace` | object (optional) | Advertises `standard_tabs` (`columns`, `indexes`, `foreign_keys`, `diagram`) and `custom_views`. |

#### Form field kinds and validation

- **Field `kind`**: `0` = Text Input, `1` = Password Input (masked), `2` = Select Dropdown (requires `options: [{"label": "...", "value": "..."}]`).
- **Field `validate`**: `0` = None, `1` = Required (non-empty), `2` = Port number (`1` to `65535`).

---

## Protocol lifecycle and method reference

A plugin session progresses through well-defined phases:

```mermaid
sequenceDiagram
    participant Host as Perk Workbench
    participant Plugin as Plugin Process

    Note over Host,Plugin: Phase 1: Initialization
    Host->>Plugin: perk/v1/initialize
    Plugin-->>Host: Result (protocol_version: 1, capabilities)

    Note over Host,Plugin: Phase 2: Target Building
    Host->>Plugin: perk/v1/build_target (FormValues)
    Plugin-->>Host: Result (target string, ok)

    Note over Host,Plugin: Phase 3: Open Session
    Host->>Plugin: perk/v1/open (target)
    Plugin-->>Host: Result (session_id, DatabaseInfo)

    Note over Host,Plugin: Phase 4: Schema & Inspection
    Host->>Plugin: perk/v1/list_schema (session_id)
    Plugin-->>Host: Result ([SchemaObject])
    Host->>Plugin: perk/v1/table_info (session_id, table)
    Plugin-->>Host: Result ([ColumnInfo])

    Note over Host,Plugin: Phase 5: Query Execution
    Host->>Plugin: perk/v1/execute (session_id, statement)
    opt User presses Escape
        Host->>Plugin: notification perk/v1/cancel (id)
    end
    Plugin-->>Host: Result or Error (-32800 if canceled)

    Note over Host,Plugin: Phase 6: Session Teardown
    Host->>Plugin: perk/v1/close (session_id)
    Plugin-->>Host: Result null
```

### 1. Initialization (`perk/v1/initialize`)

The very first request sent to the child process. Any other request sent before initialization completes is rejected with error code `-32600`.

- **Params**:
  ```json
  {"protocol_version": 1, "workbench_version": "perk-workbench 0.1.0"}
  ```
- **Result**:
  ```json
  {
    "protocol_version": 1,
    "capabilities": { ... }
  }
  ```

### 2. Build target (`perk/v1/build_target`)

Converts values entered in the connection form (`FormValues`) into a canonical target string. The DTO keys match the schema: `host`, `port`, `user`, `pass`, `database`, `tls`, and `extras`.

- **Params**:
  ```json
  {
    "host": "localhost",
    "port": "5432",
    "user": "postgres",
    "pass": "secret",
    "database": "app_db",
    "tls": "disable",
    "extras": {"application_name": "perk"}
  }
  ```
- **Result**:
  ```json
  {
    "target": "postgres://postgres:secret@localhost:5432/app_db?sslmode=disable",
    "ok": true
  }
  ```

### 3. Open session (`perk/v1/open`)

Opens a database connection. Returns an integer `session_id` and database product metadata.

- **Params**:
  ```json
  {"target": "postgres://postgres:secret@localhost:5432/app_db"}
  ```
- **Result**:
  ```json
  {
    "session_id": 1,
    "info": {
      "product": "PostgreSQL",
      "version": "16.2"
    }
  }
  ```

### 4. Close session (`perk/v1/close`)

Closes an opened session and releases associated connections.

- **Params**:
  ```json
  {"session_id": 1}
  ```
- **Result**: `null`

### 5. Statement execution (`perk/v1/execute` and `perk/v1/execute_read_only`)

Executes a database statement or query. Result objects must satisfy the canonical schema requirements: `columns`, `column_types`, `rows`, `untruncated_rows`, `rows_affected`, `has_more`, `duration_ns` (nanoseconds), and `truncated`.

- **Params**:
  ```json
  {
    "session_id": 1,
    "statement": "SELECT id, name FROM categories"
  }
  ```
- **Result**:
  ```json
  {
    "columns": ["id", "name"],
    "column_types": ["integer", "string"],
    "rows": [
      ["1", "Electronics"],
      ["2", "Books"]
    ],
    "untruncated_rows": [
      ["1", "Electronics"],
      ["2", "Books"]
    ],
    "rows_affected": 0,
    "has_more": false,
    "duration_ns": 3200000,
    "truncated": false
  }
  ```

### 6. Statement validation (`perk/v1/validate`)

Validates syntax and structural correctness without executing the statement.

- **Params**:
  ```json
  {"session_id": 1, "statement": "SELECT * FROM"}
  ```
- **Result**: `null` on valid syntax; returns `-32000` with `kind: "validation"` on syntax error.

### 7. Request cancellation (`perk/v1/cancel`)

A JSON-RPC notification (no `id`) indicating the user canceled a running query (e.g. by pressing `Escape`). The plugin must abort the in-flight work and answer the original execute request with error code `-32800`:

- **Params**:
  ```json
  {"id": 42}
  ```

### 8. Schema and table inspection

- **`perk/v1/list_schema`**: Returns database schema items.
  - **Params**: `{"session_id": 1}`
  - **Result**:
    ```json
    [
      {"database": "app_db", "type": "table", "name": "users", "row_count": 142},
      {"database": "app_db", "type": "view", "name": "v_active_users"}
    ]
    ```
- **`perk/v1/table_info`**: Returns column definitions for a table. `primary_key` is an integer (`1` for PK/first PK column, `0` for non-PK), and `indexes` is an array of integer index kinds (`1` primary key, `2` unique, `3` regular).
  - **Params**: `{"session_id": 1, "table": "users"}`
  - **Result**:
    ```json
    [
      {
        "name": "id",
        "type": "INTEGER",
        "attributes": "",
        "nullable": false,
        "default_value": null,
        "primary_key": 1,
        "indexes": [1]
      },
      {
        "name": "email",
        "type": "TEXT",
        "attributes": "",
        "nullable": false,
        "default_value": null,
        "primary_key": 0,
        "indexes": []
      }
    ]
    ```
- **`perk/v1/list_indexes`** / **`perk/v1/list_indexes_all`**: Returns index definitions.
- **`perk/v1/list_foreign_keys`** / **`perk/v1/list_referencing_foreign_keys`** / **`perk/v1/list_foreign_keys_all`**: Returns foreign key definitions.

### 9. Table browsing (`perk/v1/browse_table`)

Retrieves paginated table rows with optional filtering and sorting. Sort items use the `descending` boolean:

- **Params**:
  ```json
  {
    "session_id": 1,
    "table": "users",
    "options": {
      "offset": 0,
      "limit": 25,
      "filters": [{"column": "status", "operator": "eq", "value": "active"}],
      "sorts": [{"column": "created_at", "descending": true}]
    }
  }
  ```
- **Result**: Standard `Result` object (`columns`, `column_types`, `rows`, `untruncated_rows`, etc.).

### 10. Data writes (`perk/v1/row_write` and `perk/v1/document_write`)

When `write_capabilities.row_writer` is enabled, the workbench dispatches row mutations wrapped in a `request` object. Each row key and value entry uses the `name` field:

- **Params**:
  ```json
  {
    "session_id": 1,
    "request": {
      "operation": "update",
      "table": "users",
      "key": [{"name": "id", "value": {"kind": "integer", "integer": 42}}],
      "values": [{"name": "name", "value": {"kind": "string", "string": "Alice"}}]
    }
  }
  ```
- **Result**:
  ```json
  {
    "result": {
      "rows_affected": 1
    }
  }
  ```

Supported operations are `"insert"`, `"update"`, and `"delete"`. Tagged cell values carry a `kind` discriminator (`string`, `integer`, `float`, `bool`, `bytes`, `decimal`, `timestamp`, `null`, `default`).

---

## Authoring a plugin in Go

The official Go SDK is [`github.com/l3aro/perk-workbench-plugin-sdk-go`](https://github.com/l3aro/perk-workbench-plugin-sdk-go).

### Installation

```sh
go get github.com/l3aro/perk-workbench-plugin-sdk-go
```

### Complete, verified runnable example

Below is a complete in-memory key-value database plugin. It adheres strictly to the SDK types (`Rows` as `[][]*string`, `DurationNS` as nanoseconds, and `PrimaryKey` as an integer order):

```go
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	"github.com/l3aro/perk-workbench-plugin-sdk-go/server"
)

func ptr(s string) *string { return &s }

func stringResult(cols []string, colTypes []string, rows [][]string, duration time.Duration, affected int64) driver.Result {
	ptrRows := make([][]*string, len(rows))
	for i, r := range rows {
		ptrRows[i] = make([]*string, len(r))
		for j, val := range r {
			ptrRows[i][j] = ptr(val)
		}
	}
	return driver.Result{
		Columns:         cols,
		ColumnTypes:     colTypes,
		Rows:            ptrRows,
		UntruncatedRows: ptrRows,
		RowsAffected:    affected,
		DurationNS:      duration.Nanoseconds(),
	}
}

type memorySession struct {
	mu   sync.RWMutex
	data map[string]string
}

func (s *memorySession) Execute(ctx context.Context, req driver.StatementRequest) (driver.Result, error) {
	start := time.Now()
	parts := strings.Fields(strings.TrimSpace(req.Statement))
	if len(parts) == 0 {
		return driver.Result{}, nil
	}

	cmd := strings.ToUpper(parts[0])
	switch cmd {
	case "GET":
		if len(parts) < 2 {
			return driver.Result{}, driver.NewOperationError(driver.KindValidation, "usage: GET <key>")
		}
		s.mu.RLock()
		val, ok := s.data[parts[1]]
		s.mu.RUnlock()
		if !ok {
			return stringResult([]string{"value"}, []string{"TEXT"}, [][]string{{"(nil)"}}, time.Since(start), 0), nil
		}
		return stringResult([]string{"value"}, []string{"TEXT"}, [][]string{{val}}, time.Since(start), 0), nil

	case "SET":
		if len(parts) < 3 {
			return driver.Result{}, driver.NewOperationError(driver.KindValidation, "usage: SET <key> <val>")
		}
		s.mu.Lock()
		s.data[parts[1]] = strings.Join(parts[2:], " ")
		s.mu.Unlock()
		return stringResult([]string{"status"}, []string{"TEXT"}, [][]string{{"OK"}}, time.Since(start), 1), nil

	case "KEYS":
		s.mu.RLock()
		keys := make([]string, 0, len(s.data))
		for k := range s.data {
			keys = append(keys, k)
		}
		s.mu.RUnlock()
		sort.Strings(keys)
		rows := make([][]string, len(keys))
		for i, k := range keys {
			rows[i] = []string{k}
		}
		return stringResult([]string{"key"}, []string{"TEXT"}, rows, time.Since(start), 0), nil

	default:
		return driver.Result{}, driver.NewOperationError(
			driver.KindValidation,
			fmt.Sprintf("unknown command %q (supported: GET, SET, KEYS)", cmd),
		)
	}
}

func (s *memorySession) ExecuteReadOnly(ctx context.Context, req driver.StatementRequest) (driver.Result, error) {
	parts := strings.Fields(strings.TrimSpace(req.Statement))
	if len(parts) > 0 && strings.ToUpper(parts[0]) == "SET" {
		return driver.Result{}, driver.NewOperationError(driver.KindValidation, "SET is rejected in read-only mode")
	}
	return s.Execute(ctx, req)
}

func (s *memorySession) Validate(_ context.Context, req driver.StatementRequest) error {
	parts := strings.Fields(strings.TrimSpace(req.Statement))
	if len(parts) == 0 {
		return nil
	}
	switch strings.ToUpper(parts[0]) {
	case "GET", "SET", "KEYS":
		return nil
	default:
		return driver.NewOperationError(driver.KindValidation, "unknown command: "+parts[0])
	}
}

func (s *memorySession) ListSchema(_ context.Context, _ driver.EmptyRequest) ([]driver.SchemaObject, error) {
	s.mu.RLock()
	count := int64(len(s.data))
	s.mu.RUnlock()
	return []driver.SchemaObject{
		{Name: "kv_store", Type: "table", RowCount: &count},
	}, nil
}

func (s *memorySession) TableInfo(_ context.Context, _ driver.TableRequest) ([]driver.ColumnInfo, error) {
	return []driver.ColumnInfo{
		{Name: "key", Type: "TEXT", PrimaryKey: 1, Indexes: []driver.IndexKind{driver.IndexPrimaryKey}},
		{Name: "value", Type: "TEXT"},
	}, nil
}

func (s *memorySession) BrowseTable(_ context.Context, req driver.BrowseTableRequest) (driver.Result, error) {
	start := time.Now()
	s.mu.RLock()
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var rows [][]string
	for i := req.Options.Offset; i < len(keys) && len(rows) < req.Options.Limit; i++ {
		rows = append(rows, []string{keys[i], s.data[keys[i]]})
	}
	s.mu.RUnlock()

	return stringResult([]string{"key", "value"}, []string{"TEXT", "TEXT"}, rows, time.Since(start), 0), nil
}

func (s *memorySession) Close() error { return nil }

func (s *memorySession) ListIndexes(context.Context, driver.TableRequest) ([]driver.IndexInfo, error) {
	return nil, nil
}
func (s *memorySession) CreateIndex(context.Context, driver.IndexChangeRequest) error { return nil }
func (s *memorySession) ReplaceIndex(context.Context, driver.ReplaceIndexRequest) error { return nil }
func (s *memorySession) DropIndex(context.Context, driver.DropRequest) error { return nil }
func (s *memorySession) ListForeignKeys(context.Context, driver.TableRequest) ([]driver.ForeignKeyInfo, error) {
	return nil, nil
}
func (s *memorySession) ListReferencingForeignKeys(context.Context, driver.TableRequest) ([]driver.ReferencingForeignKeyInfo, error) {
	return nil, nil
}
func (s *memorySession) ListForeignKeysAll(context.Context, driver.EmptyRequest) (map[string][]driver.ForeignKeyInfo, error) {
	return map[string][]driver.ForeignKeyInfo{}, nil
}
func (s *memorySession) ListIndexesAll(context.Context, driver.EmptyRequest) (map[string][]driver.IndexInfo, error) {
	return map[string][]driver.IndexInfo{}, nil
}
func (s *memorySession) CreateForeignKey(context.Context, driver.ForeignKeyChangeRequest) error {
	return nil
}
func (s *memorySession) ReplaceForeignKey(context.Context, driver.ReplaceForeignKeyRequest) error {
	return nil
}
func (s *memorySession) DropForeignKey(context.Context, driver.DropRequest) error { return nil }
func (s *memorySession) AlterColumn(context.Context, driver.ColumnChangeRequest) error    { return nil }
func (s *memorySession) DropColumn(context.Context, driver.DropRequest) error            { return nil }
func (s *memorySession) AddColumn(context.Context, driver.AddColumnRequest) error        { return nil }

type memoryFactory struct{}

func (memoryFactory) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		Name:    "memkv",
		Display: "In-Memory KV",
		Driver:  "memkv",
		Targets: []driver.TargetPattern{{Prefix: "memkv:"}},
		QueryLanguage: &driver.QueryLanguage{
			Name:        "KV Commands",
			EditorLabel: "KV",
			Placeholder: "GET key, SET key val, KEYS...",
			Lexer:       "bash",
			Commands: []driver.QueryCommand{
				{Name: "GET", Usage: "GET <key>", Summary: "Retrieve value"},
				{Name: "SET", Usage: "SET <key> <value>", Summary: "Store value"},
				{Name: "KEYS", Usage: "KEYS", Summary: "List all keys"},
			},
		},
	}
}

func (memoryFactory) BuildTarget(_ context.Context, values driver.FormValues) (driver.BuildTargetResult, error) {
	return driver.BuildTargetResult{Target: "memkv:default", OK: true}, nil
}

func (memoryFactory) Open(_ context.Context, _ string) (driver.OpenResult, error) {
	session := &memorySession{data: map[string]string{
		"welcome": "Hello from Perk Workbench memory plugin!",
	}}
	return driver.OpenResult{
		Info:    driver.DatabaseInfo{Product: "MemKV", Version: "1.0.0"},
		Service: session,
	}, nil
}

func main() {
	if err := server.Run(os.Stdin, os.Stdout, memoryFactory{}); err != nil {
		fmt.Fprintf(os.Stderr, "plugin runtime error: %v\n", err)
		os.Exit(1)
	}
}
```

---

## Authoring a plugin in Node.js / TypeScript

The official Node.js SDK is `perk-workbench-plugin-sdk`. It is dependency-free and runs on Node >= 18.

### Installation

```sh
npm install perk-workbench-plugin-sdk
```

### Complete, verified runnable example

The following sample provides a schema-valid in-memory key-value driver that passes all 16 conformance tests and responds to live interactive queries:

```javascript
#!/usr/bin/env node
'use strict';

const {
  createPluginServer,
  PluginOperationError,
  ErrorKind,
  IndexKind
} = require('perk-workbench-plugin-sdk');

const store = new Map([
  ['greeting', 'Welcome to Node Perk plugin!']
]);

function makeResult(columns, columnTypes, rows, durationNs = 0, affected = 0) {
  return {
    columns,
    column_types: columnTypes,
    rows,
    untruncated_rows: rows,
    rows_affected: affected,
    has_more: false,
    duration_ns: durationNs,
    truncated: false
  };
}

const definition = {
  capabilities: {
    name: 'node-kv',
    display: 'Node KV Store',
    targets: [{ prefix: 'nodekv:' }],
    query_language: {
      name: 'KV',
      editor_label: 'KV',
      placeholder: 'GET key, SET key val, KEYS...',
      lexer: 'bash',
      commands: [
        { name: 'GET', usage: 'GET <key>', summary: 'Fetch value' },
        { name: 'SET', usage: 'SET <key> <val>', summary: 'Store value' },
        { name: 'KEYS', usage: 'KEYS', summary: 'List keys' }
      ]
    },
    write_capabilities: {
      row_writer: false
    }
  },

  buildTarget: async (values) => ({
    target: `nodekv:${values.host || 'local'}`,
    ok: true
  }),

  open: async (target, { signal }) => ({
    info: { product: 'NodeKV', version: '1.0.0' },
    service: {
      execute: async ({ statement }, { signal }) => {
        const start = process.hrtime.bigint();
        const parts = statement.trim().split(/\s+/);
        const cmd = parts[0]?.toUpperCase();

        if (cmd === 'GET') {
          const val = store.get(parts[1]);
          const duration = Number(process.hrtime.bigint() - start);
          return makeResult(
            ['value'],
            ['TEXT'],
            [[val !== undefined ? val : '(nil)']],
            duration
          );
        }
        if (cmd === 'SET') {
          store.set(parts[1], parts.slice(2).join(' '));
          const duration = Number(process.hrtime.bigint() - start);
          return makeResult(['status'], ['TEXT'], [['OK']], duration, 1);
        }
        if (cmd === 'KEYS') {
          const keys = [...store.keys()].sort();
          const duration = Number(process.hrtime.bigint() - start);
          return makeResult(
            ['key'],
            ['TEXT'],
            keys.map((k) => [k]),
            duration
          );
        }

        throw new PluginOperationError(`Unknown command: ${cmd}`, {
          kind: ErrorKind.Validation
        });
      },

      executeReadOnly: async (req, ctx) => {
        if (req.statement.trim().toUpperCase().startsWith('SET')) {
          throw new PluginOperationError('SET not allowed in read-only mode', {
            kind: ErrorKind.Validation
          });
        }
        const opened = await definition.open(target, ctx);
        return opened.service.execute(req, ctx);
      },

      validate: async ({ statement }) => {
        const cmd = statement.trim().split(/\s+/)[0]?.toUpperCase();
        if (!['GET', 'SET', 'KEYS', ''].includes(cmd)) {
          throw new PluginOperationError(`Invalid command: ${cmd}`, {
            kind: ErrorKind.Validation
          });
        }
      },

      listSchema: async () => [
        { database: '', type: 'table', name: 'kv_data', row_count: store.size }
      ],

      tableInfo: async () => [
        {
          name: 'key',
          type: 'TEXT',
          attributes: '',
          nullable: false,
          default_value: null,
          primary_key: 1,
          indexes: [IndexKind.PrimaryKey]
        },
        {
          name: 'value',
          type: 'TEXT',
          attributes: '',
          nullable: false,
          default_value: null,
          primary_key: 0,
          indexes: []
        }
      ],

      browseTable: async ({ options }) => {
        const start = process.hrtime.bigint();
        const keys = [...store.keys()].sort();
        const slice = keys.slice(options.offset, options.offset + options.limit);
        const duration = Number(process.hrtime.bigint() - start);
        return makeResult(
          ['key', 'value'],
          ['TEXT', 'TEXT'],
          slice.map((k) => [k, store.get(k)]),
          duration
        );
      },

      listIndexes: async () => [],
      createIndex: async () => {},
      replaceIndex: async () => {},
      dropIndex: async () => {},
      listForeignKeys: async () => [],
      listReferencingForeignKeys: async () => [],
      listForeignKeysAll: async () => ({}),
      listIndexesAll: async () => ({}),
      createForeignKey: async () => {},
      replaceForeignKey: async () => {},
      dropForeignKey: async () => {},
      alterColumn: async () => {},
      dropColumn: async () => {},
      addColumn: async () => {},
      close: async () => {}
    }
  })
};

createPluginServer(definition, {
  input: process.stdin,
  output: process.stdout
});
```

---

## Authoring in Python or any other language

Because the protocol is standard JSON-RPC 2.0 over NDJSON on stdio, you can build a plugin in Python, Rust, C, or any language without external dependencies.

### Python complete runnable example

The script below demonstrates raw protocol handling: validating `jsonrpc: "2.0"`, verifying unsigned integer IDs, checking the 16 MiB frame size limit, cleanly handling EOF on stdin, and passing all 16 conformance tests:

```python
#!/usr/bin/env python3
import sys
import json

MAX_FRAME_BYTES = 16 * 1024 * 1024

CAPABILITIES = {
    "name": "python-kv",
    "display": "Python KV",
    "driver": "python-kv",
    "targets": [{"prefix": "pykv:"}],
    "write_capabilities": {"row_writer": False}
}

def send_frame(payload):
    data = json.dumps(payload, separators=(',', ':')).encode('utf-8') + b'\n'
    sys.stdout.buffer.write(data)
    sys.stdout.buffer.flush()

def send_result(req_id, result):
    send_frame({"jsonrpc": "2.0", "id": req_id, "result": result})

def send_error(req_id, code, message, data=None):
    err = {"code": code, "message": message}
    if data is not None:
        err["data"] = data
    send_frame({"jsonrpc": "2.0", "id": req_id, "error": err})

def make_result(columns, column_types, rows, duration_ns=0, affected=0):
    return {
        "columns": columns,
        "column_types": column_types,
        "rows": rows,
        "untruncated_rows": rows,
        "rows_affected": affected,
        "has_more": False,
        "duration_ns": duration_ns,
        "truncated": False
    }

def main():
    initialized = False
    store = {"greeting": "Hello from Python plugin"}

    while True:
        raw_line = sys.stdin.buffer.readline()
        if not raw_line:
            # EOF cleanly closes with status 0
            sys.exit(0)

        if len(raw_line) > MAX_FRAME_BYTES:
            sys.exit(1)

        line = raw_line.strip()
        if not line:
            continue

        try:
            text = line.decode('utf-8')
        except UnicodeDecodeError:
            sys.exit(1)

        try:
            req = json.loads(text)
        except Exception:
            sys.exit(1)

        if not isinstance(req, dict):
            sys.exit(1)

        method = req.get("method")
        if "id" not in req:
            if method == "perk/v1/cancel":
                continue
            # Unknown notification: produce no response
            continue

        req_id = req["id"]
        # id must be an unsigned integer (excluding boolean values)
        if not (isinstance(req_id, int) and not isinstance(req_id, bool) and req_id >= 0):
            send_error(None, -32600, "Invalid request: id must be an unsigned integer")
            continue

        if req.get("jsonrpc") != "2.0":
            send_error(req_id, -32600, "Invalid request: jsonrpc must be 2.0")
            continue

        if not isinstance(method, str):
            send_error(req_id, -32600, "Invalid request: method must be string")
            continue

        params = req.get("params", {})
        if params is not None and not isinstance(params, (dict, list)):
            send_error(req_id, -32602, "Invalid params: must be object or array")
            continue

        if not initialized:
            if method == "perk/v1/initialize":
                if not isinstance(params, dict) or params.get("protocol_version") != 1:
                    send_error(req_id, -32600, "Unsupported protocol version")
                    continue
                initialized = True
                send_result(req_id, {
                    "protocol_version": 1,
                    "capabilities": CAPABILITIES
                })
            else:
                send_error(req_id, -32600, "perk/v1/initialize must be called first")
            continue

        if method == "perk/v1/initialize":
            send_error(req_id, -32600, "already initialized")
            continue
        elif method == "perk/v1/build_target":
            send_result(req_id, {"target": "pykv:local", "ok": True})
        elif method == "perk/v1/open":
            send_result(req_id, {
                "session_id": 1,
                "info": {"product": "PythonKV", "version": "1.0.0"}
            })
        elif method == "perk/v1/close":
            send_result(req_id, None)
        elif method == "perk/v1/list_schema":
            send_result(req_id, [
                {"database": "", "type": "table", "name": "items", "row_count": len(store)}
            ])
        elif method == "perk/v1/table_info":
            send_result(req_id, [
                {"name": "key", "type": "TEXT", "attributes": "", "nullable": False, "default_value": None, "primary_key": 1, "indexes": [1]},
                {"name": "value", "type": "TEXT", "attributes": "", "nullable": False, "default_value": None, "primary_key": 0, "indexes": []}
            ])
        elif method in ("perk/v1/execute", "perk/v1/execute_read_only"):
            statement = params.get("statement", "").strip() if isinstance(params, dict) else ""
            parts = statement.split()
            cmd = parts[0].upper() if parts else ""
            if cmd == "GET" and len(parts) > 1:
                val = store.get(parts[1], "(nil)")
                send_result(req_id, make_result(["value"], ["TEXT"], [[val]]))
            elif cmd == "SET" and len(parts) > 2:
                if method == "perk/v1/execute_read_only":
                    send_error(req_id, -32000, "SET not allowed in read-only mode", {"kind": "validation"})
                else:
                    store[parts[1]] = " ".join(parts[2:])
                    send_result(req_id, make_result(["status"], ["TEXT"], [["OK"]], affected=1))
            elif cmd == "KEYS":
                keys = sorted(store.keys())
                send_result(req_id, make_result(["key"], ["TEXT"], [[k] for k in keys]))
            else:
                send_error(req_id, -32000, f"unknown command: {cmd}", {"kind": "validation"})
        elif method == "perk/v1/validate":
            statement = params.get("statement", "").strip() if isinstance(params, dict) else ""
            cmd = statement.split()[0].upper() if statement else ""
            if cmd in ("GET", "SET", "KEYS", ""):
                send_result(req_id, None)
            else:
                send_error(req_id, -32000, f"invalid command: {cmd}", {"kind": "validation"})
        elif method == "perk/v1/browse_table":
            opts = params.get("options", {}) if isinstance(params, dict) else {}
            offset = opts.get("offset", 0)
            limit = opts.get("limit", 25)
            keys = sorted(store.keys())
            slice_keys = keys[offset:offset+limit]
            rows = [[k, store[k]] for k in slice_keys]
            send_result(req_id, make_result(["key", "value"], ["TEXT", "TEXT"], rows))
        elif method in ("perk/v1/list_indexes", "perk/v1/list_foreign_keys", "perk/v1/list_referencing_foreign_keys"):
            send_result(req_id, [])
        elif method in ("perk/v1/list_indexes_all", "perk/v1/list_foreign_keys_all"):
            send_result(req_id, {})
        elif method in ("perk/v1/create_index", "perk/v1/replace_index", "perk/v1/drop_index",
                        "perk/v1/create_foreign_key", "perk/v1/replace_foreign_key", "perk/v1/drop_foreign_key",
                        "perk/v1/alter_column", "perk/v1/drop_column", "perk/v1/add_column"):
            send_result(req_id, None)
        else:
            send_error(req_id, -32601, "Method not found")

if __name__ == "__main__":
    main()
```

---

## Testing, conformance, and verification

Perk Workbench ships with a comprehensive conformance test suite that tests any binary against the exact protocol specifications.

### Running conformance tests

```sh
# Run standard conformance test suite
perk-workbench plugin test ./my-plugin

# Emit machine-readable evidence document
perk-workbench plugin test --json ./my-plugin
```

The conformance runner executes 16 isolated cases against fresh child instances:

1. **`initialize`**: Proves the handshake responds with protocol version 1 and valid capabilities.
2. **`wrong_jsonrpc`**: Rejects invalid `"jsonrpc"` strings with `-32600`.
3. **`string_id`**: Rejects non-integer IDs with `-32600`.
4. **`float_id`**: Rejects fractional IDs with `-32600`.
5. **`unknown_method`**: Verifies unknown methods return `-32601`.
6. **`cancel_notification`**: Ensures cancel notifications produce zero responses and do not crash the child.
7. **`request_before_initialize`**: Proves requests prior to initialize return error `-32600`.
8. **`two_requests_one_write`**: Handles multiple pipelined requests delivered in one buffer read.
9. **`cancel_unknown_id`**: Silently ignores cancel notifications for unknown or completed IDs.
10. **`malformed_json_terminates`**: Terminates immediately on unparseable JSON frames.
11. **`non_object_json_terminates`**: Terminates on primitive JSON values (`"string"`, `123`, `[1]`).
12. **`invalid_utf8_terminates`**: Terminates on non-UTF-8 byte sequences.
13. **`eof_input_no_response`**: Cleanly shuts down without trailing writes when `stdin` closes.
14. **`oversized_input_terminates`**: Terminates immediately when input exceeds 16 MiB.
15. **`exact_max_frame`**: Accepts frames at the exact 16 MiB boundary.
16. **`clean_eof_shutdown`**: Reaps child process with exit code 0 when standard input closes.

### Step-by-step verification checklist

1. **Test Conformance**:
   ```sh
   perk-workbench plugin test ./my-plugin
   ```
2. **Inspect Registration Lifecycle**:
   ```sh
   perk-workbench plugin inspect ./my-plugin
   ```
3. **Preview and Obtain SHA-256 Fingerprint**:
   ```sh
   perk-workbench plugin add ./my-plugin
   ```
4. **Approve and Pin in Configuration**:
   ```sh
   perk-workbench plugin add --approve <SHA256> ./my-plugin
   ```
5. **Launch Workbench and Connect**:
   ```sh
   perk-workbench my-prefix:target
   ```

---

## Best practices and pitfalls

- **`stdout` vs `stderr`**: Writing even a single unformatted string (e.g. `fmt.Println("Connecting...")` or `console.log("Ready")`) to `stdout` breaks the NDJSON parser and terminates your plugin. Redirect all internal logging and library output to `stderr`.
- **Immediate Flushing**: Buffers on standard output must be flushed after writing each response frame (`os.Stdout.Sync()` in Go, `sys.stdout.buffer.flush()` in Python).
- **Graceful EOF Handling**: When `stdin` reaches EOF, your plugin should close open database connections and exit cleanly with status code `0`.
- **Display Result Bounds & Implementation Responsibility**: The workbench host's internal session proxy (`sessionProxy.Execute` / `BrowseTable`) does not truncate, clamp, or alter result rows—it unmarshals and forwards whatever the plugin returns. It is the plugin author's obligation to implement truncation (capping `rows` at 500 rows and 300 runes per cell for safe TUI rendering) and provide `untruncated_rows` parallel to `rows` so that cell detail modals and exports access untruncated content. Neither the Go host shim nor the Node SDK automatically truncates result matrices.
