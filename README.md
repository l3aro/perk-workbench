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

## Install with npm

The npm launcher installs the matching prebuilt binary. It supports these
targets:

- macOS Intel: `darwin-x64`
- macOS Apple Silicon: `darwin-arm64`
- Linux Intel: `linux-x64`
- Linux ARM64: `linux-arm64`
- Windows Intel: `win32-x64`

Install globally:

```bash
npm install -g perk-workbench
perk-workbench --help
perk-workbench --version
```

Or run it without a global install:

```bash
npx perk-workbench --help
npx perk-workbench --version
```

Perk Workbench needs a terminal with a TTY and alternate-screen support. The
launcher does not compile Go code, run install hooks, download binaries, or
create a GitHub Release. SQLite paths must already exist, except for
`:memory:`. MySQL and PostgreSQL targets must be reachable.

The npm command accepts the same target arguments as the Go binary:

```bash
perk-workbench demo/chinook-sqlite.db
perk-workbench :memory:
perk-workbench 'mysql:dsn'
perk-workbench 'postgres:dsn'
perk-workbench --read-only database.db
```

## Publish a release

The trusted-publish workflow runs for tags matching exactly
`vMAJOR.MINOR.PATCH` with an optional SemVer `-prerelease` suffix. For
example, `v1.2.3` and `v1.2.3-rc.1` are valid. `1.2.3`, `v1.2`, and tags with
build metadata such as `v1.2.3+build.1` are rejected. Stable tags publish to
npm's `latest` dist-tag. Prerelease tags publish to `next`.

Before the first release, perform this npm-name availability preflight. Each
command should report that the package is not found. Stop if any name is
already registered, and confirm ownership before publishing:

```bash
for package in \
  perk-workbench \
  perk-workbench-darwin-x64 \
  perk-workbench-darwin-arm64 \
  perk-workbench-linux-x64 \
  perk-workbench-linux-arm64 \
  perk-workbench-win32-x64; do
  npm view "$package" name
done
```

Configure npm trusted publishing for each of these six package names:

1. `perk-workbench`
2. `perk-workbench-darwin-x64`
3. `perk-workbench-darwin-arm64`
4. `perk-workbench-linux-x64`
5. `perk-workbench-linux-arm64`
6. `perk-workbench-win32-x64`

For every package, add a GitHub Actions trusted publisher bound to owner
`l3aro`, repository `perk-workbench`, and workflow
`.github/workflows/npm-publish.yml`. Then verify that the repository is clean,
create and push the tag, and let the workflow run:

```bash
git status --short
git tag v1.2.3
git push origin v1.2.3
```

The workflow builds and publishes the five platform packages first, followed
by the `perk-workbench` launcher, using npm provenance and the selected
`latest` or `next` dist-tag. It does not create a GitHub Release automatically.

Every published package is MIT licensed. The repository includes the MIT
notice in `LICENSE`.

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
