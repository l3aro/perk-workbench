# Perk Workbench Design

## Goal

Perk Workbench is an interactive terminal database client. It opens one SQLite, MySQL, PostgreSQL, or MongoDB target; exposes its schema; executes SQL (or mongosh-style statements for MongoDB); and supports table browsing and schema changes through a shared driver contract.

## Non-goals

- Database server management, migrations, or an ORM.
- Multiple simultaneous database connections.
- A web or GUI client.
- AI configuration as a requirement for normal use.

## System shape

```text
cmd/perk-workbench
        |
        +--> --plugin NAME: one built-in child speaking perk/v1
        |
        v
internal/workbench/app <-- Bubble Tea state, layout, async commands
        |                         |
        |                         +--> internal/ai (optional chat)
        |
        v
internal/database -- plugin-aware routing and persisted plugin IDs
        |
        +--> internal/database/plugin -- child loader and protocol shim
        |          |
        |          +--> perk-workbench --plugin sqlite|mysql|postgres|mongodb
        |          +--> external executable
        |
        v
internal/sql <-- shared service and wire-safe DTO contracts

internal/core      query lifecycle, focus, and tab state
internal/chrome    stateless terminal rendering
internal/clipboard optional native clipboard support
internal/log       event log with debug/info/warn/error levels
```

`cmd/perk-workbench` owns process setup, CLI parsing, self-plugin dispatch,
signal context, optional clipboard and AI initialization, Bubble Tea startup,
and cleanup. A built-in is deliberately a child process: the host and its
four bundled implementations communicate over the same perk/v1 boundary used
by external plugins.


`internal/workbench/app` owns all Bubble Tea state and presentation decisions. It starts database work and query execution as commands, then applies result messages only when they match the active request. The root `app.Model` is a shell that coordinates; each UI feature lives in its own package under `internal/workbench/` (`querylog`, `notification`, `connection`, `chat`, `browse`, `schema`, plus the shared `uikit` contract), and the shell itself lives in `internal/workbench/app`.

`internal/core` owns the small workflow state machine: opening, ready, failure, and picking states; focused pane and tab; selected table; and the active cancelable query.

`internal/database.Open` selects a registered plugin by explicit plugin ID
and target:

- an unprefixed target selects plugin ID `sqlite`;
- a prefixed target selects its plugin only when exactly one registered plugin
  matches;
- an ambiguous family reports every candidate and requires the connection
  form or `--select`.

The registry keeps `PluginID` unique and `Driver` non-unique. Target forms are
plain `TargetPattern` data, and profiles persist the selected plugin ID.
SQLite accepts `:memory:`; every other target must resolve to an existing
regular file where that driver requires one. Opening a target lists its
initial schema and closes the service when schema loading fails.

The connection form is driven by each plugin's declarative `FormSpec`.
`FormValues` and target serialization cross the same DTO boundary as external
plugins; driver-specific grammar stays in the child implementation.

`internal/sql.Service` is the boundary between the workbench and all
database implementations. It defines execution, schema inspection, table
browsing, index/foreign-key management, and column changes.

## Query lifecycle

1. The workbench asks `core.Workflow` to start a query. Only one can run.
2. Workflow creates a request ID and a child context. Execution runs asynchronously through `Service.Execute` or `Service.ExecuteReadOnly`.
3. `Escape` cancels that context. Completion messages with a stale request ID are ignored.
4. Success replaces displayed results; failure and cancellation only append the query log, preserving the prior result table.
5. A pending quit completes after the active query finishes or cancels.

All services validate ad-hoc statements with `sql.ValidateStatement`: the input must contain one non-empty statement, may have one trailing semicolon, and may not create a trigger. Read-only execution is selected by `--read-only` in the workbench, not by UI convention.

The MongoDB driver replaces SQL with a small mongosh-style DSL (`db.<collection>.find(...)`, `countDocuments`, `aggregate`, `distinct`, writes, `drop`, `createIndex`, `show collections`/`show dbs`) and validates it with its own parser. Collections map to tables: `ListSchema` advertises only the connected database and its collections (table operations carry no database, so cross-database roots would silently target the connected DB), `TableInfo` reports fields sampled from up to 100 documents with `_id` as the implicit primary key, and indexes are real MongoDB indexes. Result tables show compact mongosh-style cells; the cell viewer and copy action render object and array cells as valid relaxed extended JSON so values paste straight into mongosh, mongoimport, or jq. Foreign keys and column DDL have no MongoDB equivalent and return explicit errors. The browse row edit/delete flows emit SQL and are not supported on MongoDB connections yet.

`sql.CollectRows` retains at most 500 display rows. It stores both display-safe and full cell values so the cell viewer can show original content; rendered cells use the shared display sanitizer and 300-rune cap.

## UI model

The primary view has a schema pane and a workspace. Workspace tabs cover query editing/results, structure, and table browsing. Compact layout decisions, focus navigation, keybindings, palettes, dialogs, and forms all remain in `internal/workbench/app`; `internal/chrome` only renders stateless terminal fragments.

### Root shell and feature packages

The root `app.Model` (package `internal/workbench/app`) is a shell, not the home of feature state. Each UI feature is a package under `internal/workbench/` — `querylog` (query-log pane, paging, detail), `notification` (popup, detail, history), `connection` (profile form, recent list/filter), `chat` (assistant pane, runs, tools), `browse` (result table, pager, row/document/cell editors, filter form, cell viewer), and `schema` (sidebar tree, structure/index/foreign-key tabs and their forms). Every component exposes the same contract: an `Update` (or feature-specific routing methods) that returns the updated model plus typed events, and `View`/`Draw` methods for rendering. Components never reach into root state; the shell hands each one an immutable snapshot (`uikit.Layout`, keybindings, an…

The root owns what is shared across features: `core.Workflow` and the query lifecycle, window layout and focus/tab navigation, global keybindings and the command palette, the status/log drain, the form-mode controller, and the modal overlays (context menus, quit/delete dialogs, confirmations, the explain picker, the table popup, the cell viewer). It applies the typed events components emit — status changes, clipboard copies, query/schema/browse requests, and CRUD executions — and keeps the asynchronous invariants: one active query, stale completions rejected by request ID, Escape cancellation, failed or canceled queries preserving the prior result table, and pending quit completing only after the active query finishes or cancels.

**Feature-event rule.** Feature logic lives in the feature package; the root only routes and applies. A message that belongs to a feature (chat via `chat.OwnsMessage`, notification via `Consumes`, browse/schema/connection/querylog by focus and tab) is dispatched into that component's update path, and the component's returned event is applied by the root (which may read exported component state for layout and overlay decisions but never mutates feature internals directly). Overlay precedence is pinned: palette/theme/table-target → notification history → notification detail → query-log detail → confirm overlays → popup, with the popup drawn last. Root-owned dialogs and menus draw over the composed panes; components draw their own overlays (notification history/detail/popup, query-log detail) through `Draw`.

Frame borders use rounded corners throughout: `lipgloss.RoundedBorder()` for lipgloss frames (panes, SQL editor) and the matching `╭╮╰╯` glyphs for canvas-drawn frames (dialogs, context menu, palette, confirmation card, cell viewer, ER diagram cards). Canvas frames hardcode those glyphs, so a future square-corner theme must update both the lipgloss styles and the canvas glyphs. Schema-tree connectors (`└`) and the confirmation accent bar (`┃`) intentionally keep their square glyphs.

Keybindings are built-in defaults. There is no keybindings file: the app never writes a placeholder registry. Overrides live in the optional `keybinds` object inside `config.json` — users add entries by hand for only the commands they want to change (both flat `"app.quit": ["q"]` and nested `"app": {"quit": ["q"]}` maps are accepted; an empty array disables a command). Unknown command IDs or invalid keystrokes fail startup with the config path.

App defaults load from `$XDG_CONFIG_HOME/perk-workbench/config.json` (also written on first run). Supported fields, all optional (0/omitted = built-in default):

- `browse_page_size` — default row limit for table browsing, within `[1, 500]`
- `log_level` — minimum severity written to `event.log` and surfaced as notifications: `debug`, `info`, `warn`, or `error` (default `info`; `debug` opts the database-opening and database-ready notices back in). The opening notice is transient: it toasts and reaches `event.log` but never persists to notification history, because it fires before the connection profile scope exists
- `query_log_page_size` — query-log pane page size, within `[1, 100]`
- `query_log_retention_days` — days of query-log history kept (default 30; set `PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS=0` to keep none)
- `read_only` — open every connection read-only by default; the per-connection form toggle still opts out
- `theme` — startup theme: `ocean`, `nord`, `monokai`, `dracula`, `catppuccin`, `solarized`; choosing a theme in-app (palette or picker) writes it back to config.json
- `table_open_target` — workspace tab focused after selecting a table in the schema tree: `structure` (the Columns tab), `browse`, `sql`, `indexes`, or `foreign_keys`; choosing it in the command palette writes it back to config.json
- `keybinds` — manual keybinding overrides, added by hand (never materialized): maps command IDs to keystroke arrays, e.g. `"keybinds": {"app.quit": ["q"]}`; every unlisted command keeps its built-in default, and an empty array disables a command

The `PERK_WORKBENCH_QUERY_LOG_*` env vars still override their config values.

### Focus diagrams

The foreign-keys tab toggles a relationship diagram (`g`) and the indexes tab an index diagram: the selected table is the hub card, tables referencing it render above, tables it references below. The relationship diagram labels every hub connector with the relation's endpoint cardinalities — `(N)` for the child (the FK holder) and `(1)` for the parent (the referenced table), joined by an upward arrow (head plus shaft), so each edge reads `(1)──▶(N)` from the parent to the child (or `(1)──▶(1)` when the FK columns carry a unique index, computed from the cached whole-schema index map; composite unique keys count regardless of column order). Both endpoint labels stay per edge: on top sides the child labels sit beside the referencing cards and the parent label beside the hub; on bottom sides both rows sit at the stub columns — child row on the hub side of the arrow, parent row beside the parent cards — so fan-outs with mixed uniqueness never mislabel the shared hub column. Labels are omitted rather than guessed when the index cache isn't loaded. The neighbor cards carry the FK column mappings with the hub's column qualified by the hub's bare table name and highlighted (bold, accent color) — incoming edges show the hub's key on the right (`order_id → orders.id`), outgoing edges on the left (`orders.customer_id → id`), so the main table's key is identifiable at a glance. The index diagram's cards list each table's indexes instead. `}`/`{` widen/narrow the focus ring one foreign-key hop at a time. The hub always follows the schema-tree selection; the two diagram modes are mutually exclusive.

The rings come from connection-level caches the root loads on connect and refreshes after any DDL mutation: `Service.ListForeignKeysAll` and `Service.ListIndexesAll` return the whole schema keyed by declaring table (MySQL and PostgreSQL keys qualified, SQLite bare, MongoDB collection names; MongoDB returns no foreign keys). The diagrams read them through the schema `Snapshot`. Each refresh is stamped with the connection generation (`openTag`) and a per-cache revision, so a stale result — whether from a superseded connection or an overlapping same-connection refresh — is dropped on arrival. Without the cache the diagrams degrade to the selected table's own data at depth 1. Diagrams too wide or tall for the workspace fall back to a flat list.

## AI integration

AI is optional. Startup loads and merges:

1. user config: `$XDG_CONFIG_HOME/perk-workbench/ai.json` (via `os.UserConfigDir`)
2. project config: `.perk-workbench/ai.json`

Project entries override user entries by name. Strict JSON decoding rejects unknown fields and multiple JSON values. AI activates only when an `assistant` agent is configured. Provider adapters handle OpenAI, Anthropic, Gemini, and OpenAI-compatible APIs; the shared client exposes regular and streaming chat plus optional tool calls.

## Invariants for changes

- `internal/chrome` must not import `workbench` or hold Bubble Tea state.
- Keep driver SQL and connection details out of `workbench` and `internal/sql`.
- Keep execution asynchronous and cancelable; never apply stale completions.
- Failed queries must retain prior results.
- Preserve the SQLite no-create-file rule.
- New driver capabilities belong in `internal/sql.Service` only when every backend can implement them coherently; otherwise keep the behavior local. Capability-gated optional interfaces (below) extend the contract without forcing every backend: the workbench discovers them by type assertion and degrades gracefully when a backend does not implement them.
- AI remains optional and must not block opening or using a database.

## Row-level writes (browse-tab CRUD)

The browse tab's row operations — cell edit, row edit, insert, delete — are split across the package boundary: `internal/workbench/browse` owns the forms, editors, and their pure state transitions, and emits typed requests; the app shell (`internal/workbench/app`) owns statement construction (`action_log.go` and the browse execution paths in `model_update.go` build the ALTER/CREATE/UPDATE/DELETE strings and still branch identifier quoting on `databaseInfo.Product`), plus the execution flows and capability wiring. A serializable capability descriptor plus narrow optional interfaces in `internal/sql` gate every action: drivers that implement `WriteCapabilitiesProvider` advertise their capabilities; otherwise the app derives the same descriptor from in-process type assertions (`RowWriter`, `DocumentReader`, `DocumentWriter`). Product names remain display-only for write-action availability — every browse write action dispatches on the descriptor — while the quoting rules themselves stay in the shell's statement helpers, never in `internal/sql`. Stores without a capability simply hide or reject the action.

The target is a **serializable capability descriptor plus narrow optional interfaces** in `internal/sql`, implemented by drivers that can coherently support them. `Service` itself does not change; Redis/Neo4j-style stores simply do not implement the interfaces and keep whatever views they get later.

```go
// RowWriter addresses a store as rows with a primary key (SQL tables;
// future CQL-style stores). Key values identify the row for update/delete.
type RowWriter interface {
	InsertRow(ctx context.Context, table string, values []RowValue) (Result, error)
	UpdateRow(ctx context.Context, table string, key []RowValue, values []RowValue) (Result, error)
	DeleteRow(ctx context.Context, table string, key []RowValue) (Result, error)
}

// DocumentFormat tags a document payload's serialization so a store's
// dialect can evolve without breaking the contract.
type DocumentFormat string

const (
	// DocumentFormatMongoExtendedJSON is MongoDB relaxed extended JSON,
	// mongoexport-compatible — what the mongodb driver already renders.
	DocumentFormatMongoExtendedJSON DocumentFormat =
		"application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed"
)

// DocumentPayload is a tagged payload: the store's declared format plus
// bytes. It carries both document bodies and row identities.
type DocumentPayload struct {
	Format DocumentFormat `json:"format"`
	Data   []byte         `json:"data"`
}

// DocumentWriteCapability declares a document store's editor contract:
// Format is the only payload format the driver accepts, and Text reports
// whether whole-document text editing is safe. A store that cannot replace
// a document safely must not advertise Text; it may still expose delete
// when browse results supply document identities.
type DocumentWriteCapability struct {
	Format DocumentFormat `json:"format"`
	Text   bool           `json:"text"`
}

// WriteCapabilities is the serializable capability descriptor at the
// child-process boundary. Built-ins and external plugins advertise the same
// descriptor and are wrapped by a shim.
type WriteCapabilities struct {
	RowWriter bool                     `json:"row_writer"`
	Document  *DocumentWriteCapability `json:"document,omitempty"`
}

// WriteCapabilitiesProvider is implemented by drivers that can describe
// their write capabilities without the workbench type-asserting internals.
type WriteCapabilitiesProvider interface {
	WriteCapabilities() WriteCapabilities
}

// DocumentReader loads one complete document by identity.
type DocumentReader interface {
	ReadDocument(ctx context.Context, collection string, id DocumentPayload) (DocumentPayload, error)
}

// DocumentWriter addresses a document store (MongoDB; future DynamoDB).
// Documents travel as tagged payloads; a second document store brings its
// own format constant rather than a contract change. ReplaceDocument is
// whole-document replacement: the driver translates the payload into its
// native replace, so mutation dialect stays driver-side.
type DocumentWriter interface {
	InsertDocument(ctx context.Context, collection string, doc DocumentPayload) (Result, error)
	ReplaceDocument(ctx context.Context, collection string, id DocumentPayload, doc DocumentPayload) (Result, error)
	DeleteDocument(ctx context.Context, collection string, id DocumentPayload) (Result, error)
}
```

**Value representation.** `RowValue` is an explicit tagged tree, not `any`: a `Kind` plus per-kind payloads (String, Bool, Integer, Float, Bytes, Decimal, Timestamp, Array, Object). Every kind is JSON-encodable, so the same tree survives a future out-of-process plugin boundary without leaking driver-native types (ObjectID, decimals, instants) into the contract. The workbench's existing tri-state form maps directly onto `Default` (omit the column), `Null`, and `String`; typed input (a per-field type picker) is a later enhancement. Document stores skip the tree entirely and exchange `DocumentPayload` — a format tag plus bytes — because a document's serialization *is* its value: extended JSON expresses nested documents and native types exactly, which is the Compass-style JSON document editor. The tag is what keeps the contract honest: the first non-BSON document store (DynamoDB JSON, Elasticsearch's dialect) adds a format constant and its own editor-to-driver pairing instead of forcing a breaking redesign of this interface.

**Capability discovery** replaces the product-string checks. The workbench
derives a `WriteCapabilities` descriptor from
`WriteCapabilitiesProvider` when the service implements it, otherwise from
the same service interfaces on the local or proxied child. Product names are
display-only; every browse action dispatches on the descriptor.

**Document editor policy.** A non-nil `Document` capability with `Text == true` is editable. `DocumentFormatMongoExtendedJSON` uses the JSON-aware editor: an insert starts at `{}`, the workbench requires `encoding/json.Valid` before confirmation, and the driver enforces BSON Extended JSON semantics. Every other non-empty textual format uses a labeled raw-text editor with no parsing/formatting/schema behavior; exact UTF-8 bytes travel unchanged to the driver. Empty format, `Text == false`, or a loaded document with invalid UTF-8 disables insert/edit and reports `document editing is unsupported for format <format>`. Delete remains available whenever the selected browse row carries a valid document identity (`Result.DocumentIDs`).

**Migration phases.**

1. *SQL family* — move the three statement builders into the `sqlite`, `mysql`, and `postgres` drivers as `RowWriter` implementations, binding values as parameters instead of quoting them by hand (all three drivers already own their `quoteIdentifier`). The workbench keeps the tri-state form and confirmation dialog; the confirmation and query-log entries show a structured preview of the same `RowValue` DTOs instead of dialect SQL, so no driver statement is duplicated in the UI. Read-only stays a workbench policy. Behavior parity: same `RowsAffected == 1` checks.
2. *MongoDB* — implement `WriteCapabilitiesProvider`, `DocumentReader`, and `DocumentWriter` (format `application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed`) with the driver's existing BSON extended-JSON parsing and collection layer; browse results carry per-row `DocumentIDs`; the workbench adds a JSON document editor for insert and whole-document replace, removing the current "not supported" rejection.
3. *Future* — external plugins advertise the same capability descriptor and
exchange the tagged DTOs. Built-in and external services remain separated by
the perk/v1 child boundary; the workbench does not special-case a driver
implementation in the UI.

## Plugins

Plugins are the process boundary for database implementations. Every built-in
and external plugin serves the same JSON-RPC 2.0, newline-delimited
perk/v1 protocol on stdin/stdout. A built-in descriptor launches this host
binary with `--plugin sqlite`, `--plugin mysql`, `--plugin postgres`, or
`--plugin mongodb`; an external descriptor launches a configured executable.
Neither source is called in-process by the TUI.

`config.json` stores descriptors under `plugins`:

```json
{
  "plugins": [
    {"builtin": "sqlite"},
    {"builtin": "mysql"},
    {"path": "/home/alice/.local/bin/perk-redis", "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
  ]
}
```

`builtin` and `path` are mutually exclusive. Built-ins resolve the current
executable and carry no digest. External paths resolve through `PATH` or
relative to the config file; a supplied lowercase 64-hex `sha256` is checked
immediately before spawn. The loader identity includes the executable and its
arguments, so distinct built-in modes do not deduplicate.

Capabilities separate plugin identity from database family: `name` is the
unique plugin ID, while `driver` may be shared. `mysql` and `mysql-cloud` can
both advertise `driver: "mysql"` and appear as separate form options.
Persisted connections retain the selected plugin ID. Direct opening of an
ambiguous target is rejected instead of choosing by load order.

The host release is one `perk-workbench` executable per supported target. It
contains all four built-in implementations and publishes no `plugins/`
directory, sidecar archive, release manifest, or independent official-driver
asset. The four former driver repositories retain source and behavior tests
but do not publish driver release workflows or package inputs.

## Verification

Use focused package tests while changing a component, then run:

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
gofmt -l cmd internal
```

For manual query behavior, run against `demo/chinook-sqlite.db`, execute with `F5`, and cancel a running query with `Escape`.
