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
        v
internal/workbench/app <-- Bubble Tea state, input, layout, async commands
        |                         |
        |                         +--> internal/ai (optional chat)
        |                         |
        |   sibling packages under internal/workbench/:
        |   querylog notification connection chat browse schema uikit
        v
internal/database -- target routing --> sqlite | mysql | postgres | mongodb
        |                                      \       |       /        |
        +--------------------------------------> internal/sql <---------+

internal/core      query lifecycle, focus, and tab state
internal/chrome    stateless terminal rendering
internal/clipboard optional native clipboard support
internal/log       event log with debug/info/warn/error levels
```

`cmd/perk-workbench` owns process setup: command-line parsing, signal context, optional clipboard and AI initialization, Bubble Tea startup, and closing the database service and AI history.

`internal/workbench/app` owns all Bubble Tea state and presentation decisions. It starts database work and query execution as commands, then applies result messages only when they match the active request. The root `app.Model` is a shell that coordinates; each UI feature lives in its own package under `internal/workbench/` (`querylog`, `notification`, `connection`, `chat`, `browse`, `schema`, plus the shared `uikit` contract), and the shell itself lives in `internal/workbench/app`.

`internal/core` owns the small workflow state machine: opening, ready, failure, and picking states; focused pane and tab; selected table; and the active cancelable query.

`internal/database.Open` selects a service from the target form:

- `mysql:<DSN>` → MySQL
- `postgres:`, `postgres://`, or `postgresql://` → PostgreSQL
- `mongo:` or `mongodb://` / `mongodb+srv://` → MongoDB (database from the URI path, default `test`)
- everything else → SQLite

It opens the service, lists the initial schema, and returns both atomically. On schema-list failure it closes the service. SQLite accepts `:memory:`; every other target must resolve to an existing regular file.

Routing goes through a driver group in `internal/database`: each compiled-in driver registers a `Spec` (name, display label, target-form matcher, open function, optional connection-form description) via `Register`, and `Open` dispatches through `Match` with SQLite as the fallback. New compiled-in drivers register a spec there; the workbench and `internal/sql` never switch on target prefixes. The registry is in-process only — the durable plugin boundary is the serializable DTO/capability protocol below, reached through a transport shim, not through this table.

The connection form is driven by each spec's declarative `FormSpec`: field keys/titles/kinds, select options, validation rules, and the opener-target prefix — plain data, no code, so plugin-supplied specs survive the DTO boundary. Fields outside the fixed key set (host, port, username, password, database, target, tls) bind to `profile.Extras`, which is encrypted at rest like `Pass`. Target serialization is not part of the spec: each driver registers an in-process builder (grammar lives in the adapter — `mysql.Target`, `postgres.Target`), and the form dispatches its field-value DTO to it — the same shape a future plugin shim uses to produce a target for its driver.

`internal/sql.Service` is the boundary between the workbench and all drivers. It defines execution, schema inspection, table browsing, index/foreign-key management, and column changes. Driver-specific SQL stays inside its matching driver package.

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

Keybindings load from the XDG config path. If no file exists, defaults are written there. Both flat and nested JSON keybinding maps are accepted.

App defaults load from `$XDG_CONFIG_HOME/perk-workbench/config.json` (also written on first run). Supported fields, all optional (0/omitted = built-in default):

- `browse_page_size` — default row limit for table browsing, within `[1, 500]`
- `log_level` — minimum severity written to `event.log` and surfaced as notifications: `debug`, `info`, `warn`, or `error` (default `info`; `debug` opts the database-opening and database-ready notices back in). The opening notice is transient: it toasts and reaches `event.log` but never persists to notification history, because it fires before the connection profile scope exists
- `query_log_page_size` — query-log pane page size, within `[1, 100]`
- `query_log_retention_days` — days of query-log history kept (default 30; set `PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS=0` to keep none)
- `read_only` — open every connection read-only by default; the per-connection form toggle still opts out
- `theme` — startup theme: `ocean`, `nord`, `monokai`, `dracula`, `catppuccin`, `solarized`; choosing a theme in-app (palette or picker) writes it back to config.json
- `table_open_target` — workspace tab focused after selecting a table in the schema tree: `structure` (the Columns tab), `browse`, `sql`, `indexes`, or `foreign_keys`; choosing it in the command palette writes it back to config.json

The `PERK_WORKBENCH_QUERY_LOG_*` env vars still override their config values.

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

// WriteCapabilities is the serializable capability descriptor, the durable
// plugin boundary. Compiled-in drivers are discovered in-process; plugins
// advertise the same descriptor and are wrapped by a shim.
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

**Capability discovery** replaces the product-string checks. The workbench derives a `WriteCapabilities` descriptor from `WriteCapabilitiesProvider` when the service implements it, otherwise from the same in-process type assertions (`RowWriter`, `DocumentReader`, `DocumentWriter`). Product names remain display-only. Palette/menu availability and the row-action handlers dispatch on the descriptor; missing capability means the action is hidden or rejected with a clear status, never a broken statement. In-process assertions are the adapter seam, not the durable plugin boundary: a plugin advertises the same serializable descriptor and exchanges the tagged request/response DTOs declared in `internal/sql/row_write.go` (`RowWriteRequest`/`RowWriteResponse`, `DocumentWriteRequest`/`DocumentWriteResponse`), which serialize the `RowValue` tree and extended-JSON documents losslessly. Compiled-in drivers implement the Go interfaces directly; an out-of-process plugin is wrapped by a thin shim implementing the same interfaces by marshaling those DTOs across the boundary, so the workbench cannot tell the two cases apart.

**Document editor policy.** A non-nil `Document` capability with `Text == true` is editable. `DocumentFormatMongoExtendedJSON` uses the JSON-aware editor: an insert starts at `{}`, the workbench requires `encoding/json.Valid` before confirmation, and the driver enforces BSON Extended JSON semantics. Every other non-empty textual format uses a labeled raw-text editor with no parsing/formatting/schema behavior; exact UTF-8 bytes travel unchanged to the driver. Empty format, `Text == false`, or a loaded document with invalid UTF-8 disables insert/edit and reports `document editing is unsupported for format <format>`. Delete remains available whenever the selected browse row carries a valid document identity (`Result.DocumentIDs`).

**Migration phases.**

1. *SQL family* — move the three statement builders into the `sqlite`, `mysql`, and `postgres` drivers as `RowWriter` implementations, binding values as parameters instead of quoting them by hand (all three drivers already own their `quoteIdentifier`). The workbench keeps the tri-state form and confirmation dialog; the confirmation and query-log entries show a structured preview of the same `RowValue` DTOs instead of dialect SQL, so no driver statement is duplicated in the UI. Read-only stays a workbench policy. Behavior parity: same `RowsAffected == 1` checks.
2. *MongoDB* — implement `WriteCapabilitiesProvider`, `DocumentReader`, and `DocumentWriter` (format `application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed`) with the driver's existing BSON extended-JSON parsing and collection layer; browse results carry per-row `DocumentIDs`; the workbench adds a JSON document editor for insert and whole-document replace, removing the current "not supported" rejection.
3. *Future* — plugins advertise the same capability descriptor and exchange the tagged DTOs; the workbench reaches them through `RowWriter`/`DocumentWriter` adapter shims, making a plugin and a compiled-in driver indistinguishable. Each new store picks the family it fits. No uniformity of *semantics* is promised: ClickHouse and Cassandra mutate through UPSERT/INSERT-style statements and their own key models, not row-by-PK UPDATE, so their `RowWriter` implementations carry their own mutation and key semantics behind the interface. The interface decides where the dialect lives, not that all stores behave alike.

## Verification

Use focused package tests while changing a component, then run:

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
gofmt -l cmd internal
```

For manual query behavior, run against `demo/chinook-sqlite.db`, execute with `F5`, and cancel a running query with `Escape`.
