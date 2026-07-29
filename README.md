# Perk Workbench

## A database workspace for your terminal

Perk Workbench turns a terminal into a focused database workspace. Connect to
SQLite, MySQL, or PostgreSQL, then move from schema discovery to SQL, data
editing, and relationship inspection without switching tools.

It is built for the moments between “what is in this database?” and “make this
change”: quick enough for exploration, visual enough for browsing, and
explicit enough for mutations.

```text
connect → explore → query → inspect → change
```

## Start anywhere

Open an existing SQLite database, create a temporary in-memory workspace, or
connect to a remote database:

```bash
go run ./cmd/perk-workbench database.db
go run ./cmd/perk-workbench :memory:
go run ./cmd/perk-workbench 'mysql:dsn'
go run ./cmd/perk-workbench 'postgres:dsn'
go run ./cmd/perk-workbench              # choose a connection interactively
go run ./cmd/perk-workbench --read-only database.db
```

The connection screen also supports recent profiles. Passwords are entered
when connecting and are never stored in profiles.

Docker Compose starts the included Chinook demo database:

```bash
docker compose run --rm dev
```

Set `DEMO_DIR` to use another demo directory.

## One workspace, several ways to work

### See the database before touching it

- Browse the schema tree and switch between tables, views, and other objects.
- Inspect columns, types, nullability, and primary keys in **Structure**.
- View, filter, create, edit, and drop indexes.
- View and manage foreign keys, then open a relationship diagram.
- Browse table data page by page with sorting and per-column filters.
- Open a cell in a full-value viewer with JSON pretty-printing.

### Write SQL when you know what you want

- Use the SQL editor with context-aware completion (`Ctrl+Space`).
- Open the query in an external editor (`Ctrl+E`).
- Recall previous SQL from history (`Ctrl+R`).
- Run asynchronously and cancel a query in progress (`Escape`).
- Keep the previous result visible when a new query fails.

Each run accepts one statement. Empty input, comments, multi-statement input,
and trigger creation are rejected. Table output is capped at 500 rows and 300
runes per cell; the cell viewer shows the complete value.

### Edit data without writing every form by hand

In **Browse**, use row editing (`Enter`, requires a primary key), cell editing
(`i`), and the context menu (`,`). Copy a cell with `y`. Read-only mode blocks
all `INSERT`, `UPDATE`, `DELETE`, and DDL operations.

### Keep an audit trail

The **Query Log** is persisted and paginated. Open query details, revisit
history, and choose a query from the explain picker. Retention defaults to 30
days and page size to 25; both are configurable with environment variables.

### Ask the optional AI assistant

The assistant can use the current schema, database version, and editor SQL as
context. It provides:

- `sql_read` for safe database exploration.
- `sql_write` on writable connections, with approval before each mutation.
- Optional sharing of result rows with the conversation (`Ctrl+Shift+R`).
- Bounded tool rounds with a deadline, call cap, and repeated-result detection.

Read-only connections do not expose `sql_write`. YOLO mode is available only
on writable connections when approval prompts are intentionally disabled.

## Navigation that stays out of the way

The screen is organized into four focusable panes: schema, workspace, query
log, and AI chat. The workspace contains SQL, Browse, Structure, Indexes, and
Foreign Keys tabs.

- `Tab`, `]`, `[` — move between panes
- `1`–`4` — focus a pane directly
- `f` — toggle fullscreen
- `Ctrl+P` — open the command palette
- `g` — show the foreign-key relationship diagram
- `v` — inspect the selected cell
- `y` — copy the selected cell

All commands have configurable key bindings. Use dotted command IDs and
scope-based overrides in
`$XDG_CONFIG_HOME/perk-workbench/keybindings.json`; an empty array disables a
command. Themes include Ocean, Nord, Monokai, Dracula, Catppuccin, and
Solarized.

## Requirements

Go 1.25 and an alternate-screen terminal. SQLite targets must already exist,
except for `:memory:`; MySQL and PostgreSQL targets must be reachable.

## Development

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
gofmt -l cmd internal
```

Configuration paths:

- AI: `$XDG_CONFIG_HOME/perk-workbench/ai.json`
- Local AI override: `.perk-workbench/ai.json`
- Conversation history: `$XDG_STATE_HOME/perk-workbench/conversations.db`
- Remote connection profiles: `$XDG_CONFIG_HOME/perk-workbench/connections.json`

Perk Workbench is a workbench, not a migration runner: it does not create
missing database files, provide migrations, or run multi-statement scripts.
