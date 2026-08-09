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
internal/workbench  <-- Bubble Tea state, input, layout, async commands
        |                         |
        |                         +--> internal/ai (optional chat)
        v
internal/database -- target routing --> sqlite | mysql | postgres | mongodb
        |                                      \       |       /        |
        +--------------------------------------> internal/sql <---------+

internal/core      query lifecycle, focus, and tab state
internal/chrome    stateless terminal rendering
internal/clipboard optional native clipboard support
internal/log       event types
```

`cmd/perk-workbench` owns process setup: command-line parsing, signal context, optional clipboard and AI initialization, Bubble Tea startup, and closing the database service and AI history.

`internal/workbench` owns all Bubble Tea state and presentation decisions. It starts database work and query execution as commands, then applies result messages only when they match the active request.

`internal/core` owns the small workflow state machine: opening, ready, failure, and picking states; focused pane and tab; selected table; and the active cancelable query.

`internal/database.Open` selects a service from the target form:

- `mysql:<DSN>` → MySQL
- `postgres:`, `postgres://`, or `postgresql://` → PostgreSQL
- `mongo:` or `mongodb://` / `mongodb+srv://` → MongoDB (database from the URI path, default `test`)
- everything else → SQLite

It opens the service, lists the initial schema, and returns both atomically. On schema-list failure it closes the service. SQLite accepts `:memory:`; every other target must resolve to an existing regular file.

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

The primary view has a schema pane and a workspace. Workspace tabs cover query editing/results, structure, and table browsing. Compact layout decisions, focus navigation, keybindings, palettes, dialogs, and forms all remain in `internal/workbench`; `internal/chrome` only renders stateless terminal fragments.

Frame borders use rounded corners throughout: `lipgloss.RoundedBorder()` for lipgloss frames (panes, SQL editor) and the matching `╭╮╰╯` glyphs for canvas-drawn frames (dialogs, context menu, palette, confirmation card, cell viewer, ER diagram cards). Canvas frames hardcode those glyphs, so a future square-corner theme must update both the lipgloss styles and the canvas glyphs. Schema-tree connectors (`└`) and the confirmation accent bar (`┃`) intentionally keep their square glyphs.

Keybindings load from the XDG config path. If no file exists, defaults are written there. Both flat and nested JSON keybinding maps are accepted.

App defaults load from `$XDG_CONFIG_HOME/perk-workbench/config.json` (also written on first run). Supported fields, all optional (0/omitted = built-in default):

- `browse_page_size` — default row limit for table browsing, within `[1, 500]`
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
- New driver capabilities belong in `internal/sql.Service` only when every backend can implement them coherently; otherwise keep the behavior local.
- AI remains optional and must not block opening or using a database.

## Verification

Use focused package tests while changing a component, then run:

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
gofmt -l cmd internal
```

For manual query behavior, run against `demo/chinook-sqlite.db`, execute with `F5`, and cancel a running query with `Escape`.
