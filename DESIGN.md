# Perk Workbench Design

## Goal

Perk Workbench is an interactive terminal database client. It opens one SQLite, MySQL, or PostgreSQL target; exposes its schema; executes SQL; and supports table browsing and schema changes through a shared driver contract.

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
internal/database -- target routing --> sqlite | mysql | postgres
        |                                      \       |       /
        +--------------------------------------> internal/sql

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

`sql.CollectRows` retains at most 500 display rows. It stores both display-safe and full cell values so the cell viewer can show original content; rendered cells use the shared display sanitizer and 300-rune cap.

## UI model

The primary view has a schema pane and a workspace. Workspace tabs cover query editing/results, structure, and table browsing. Compact layout decisions, focus navigation, keybindings, palettes, dialogs, and forms all remain in `internal/workbench`; `internal/chrome` only renders stateless terminal fragments.

Keybindings load from the XDG config path. If no file exists, defaults are written there. Both flat and nested JSON keybinding maps are accepted.

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
