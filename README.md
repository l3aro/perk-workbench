# Perk Workbench

Perk Workbench is a small terminal workbench for exploring an existing SQLite or MySQL database. It opens one database, lists its tables and views, runs one SQL statement at a time, and shows the result or status in a Bubble Tea interface.

## Requirements

- Go 1.25 or newer
- A terminal with alternate screen support
- An existing SQLite database file, or the in-memory target `:memory:`
- A reachable MySQL server when using a MySQL connection

## Start

Run the workbench with an existing database:

```bash
go run ./cmd/perk-workbench <database.db>
```

For a temporary database:

```bash
go run ./cmd/perk-workbench :memory:
```

With no argument, the application opens a database picker. The picker includes `:memory:`, directories, and regular files whose names end in `.db`, `.sqlite`, or `.sqlite3`. It follows valid symlinks and omits broken links and unsupported files. A missing path supplied on the command line is not created. Press Enter on a database failure to return to the picker.

Select MySQL or PostgreSQL in the connection form to enter the server, credentials, and database. Successful connections are available as named profiles in `$XDG_CONFIG_HOME/perk-workbench/connections.json`; profiles store only the driver, name, host, port, username, and database target. Passwords are never written to disk and must be entered each time a remote profile connects. To open a MySQL target directly, prefix a standard driver DSN with `mysql:`, for example:

```bash
go run ./cmd/perk-workbench 'mysql:alice:secret@tcp(127.0.0.1:3306)/app'
```

## Docker Compose

The Compose development environment mounts this source directory and the sibling `../demo` directory at `/demo`. It opens the bundled demo database by default:

```bash
docker compose run --rm dev
```

For a demo directory elsewhere, set `DEMO_DIR` to its host path:

```bash
DEMO_DIR=/path/to/demo docker compose run --rm dev
```

Run the product checks in the same container environment:

```bash
docker compose run --rm dev go test -race ./cmd/... ./internal/...
docker compose run --rm dev go vet ./cmd/... ./internal/...
docker compose run --rm dev go build ./cmd/perk-workbench
docker compose run --rm dev gofmt -l cmd internal
```

The Compose `dev` service forwards the host terminal's color capability
variables (`TERM` and `COLORTERM`) so Lipgloss renders RGB colors inside the
container. Without them, true-color hex values collapse into a reduced ANSI
palette that is hard to read.

### Clipboard in tmux

The query log copies the selected cell with `y`. When running Perk Workbench through Docker Compose inside tmux, enable application-originated OSC 52 clipboard requests:

```bash
tmux set-option -g set-clipboard on
```

To keep the setting after restarting tmux, add this line to `~/.tmux.conf` and reload it:

```tmux
set -g set-clipboard on
```

```bash
tmux source-file ~/.tmux.conf
```

The container cannot access the native desktop clipboard; the terminal forwards OSC 52 instead.

## Keys

### Picker

- `Up` and `Down` move through the list.
- Type to filter the list.
- `Enter` opens the selected database or enters a selected directory.
- `r` reloads the current directory.
- `q` or `Ctrl+C` quits.

### Workbench

- `Tab` moves focus forward: schema, editor, results, then schema.
- `Shift+Tab` moves focus backward through the same three panes.
- `F5` runs the editor contents.
- `Ctrl+Enter` runs the editor contents when the terminal reports the modified Enter key.
- `Ctrl+S` runs the editor contents and works in terminals that cannot distinguish modified Enter keys.
- `Ctrl+R` recalls earlier executed statements.
- `Ctrl+K` saves the editor contents for this session; `Ctrl+O` selects a saved query.
- `Enter` in the schema pane loads and runs the selected table or view's DDL query.
- `Escape` cancels an active query. In the editor, `Escape` switches from insert mode to normal mode.
- `q` quits when the editor is empty or another pane owns focus. In an editor with text, `q` is inserted as text.
- Raw `Ctrl+C` requests quit. If a query is running, the query is canceled first and the program exits after cancellation completes.

### Custom key bindings

Key bindings are configurable through `$XDG_CONFIG_HOME/perk-workbench/keybindings.json`. The file path is
`~/.config/perk-workbench/keybindings.json` on Linux, and the platform equivalent on macOS and Windows.

Commands can be grouped by their dotted prefix:

```json
{
  "app": {
    "quit": ["ctrl+q"]
  },
  "query": {
    "execute": ["f1"],
    "cancel": ["esc"]
  },
  "form": {
    "save": []
  }
}
```

Commands with no group, or a mix of both formats, also work:

```json
{
  "focus.schema": ["f1"],
  "query": { "execute": ["f5"] }
}
```

- Omitted commands retain their built-in defaults.
- A listed array replaces the command's default key aliases.
- An empty array (`[]`) disables the command.
- Unknown command IDs, invalid key names, or malformed JSON cause the program to print an error and exit.
- Keys active in narrower scopes (forms, active pane) take precedence over global bindings.

### Editor

The SQL editor starts in normal mode. Press `i` to enter text, `Escape` to return to normal mode, and `Ctrl+E` in insert mode to edit SQL through `$EDITOR`.

The editor does not provide inline Vim motions, operators, visual mode, command mode, registers, or syntax highlighting.

## SQL behavior

The workbench accepts SQLite and MySQL connections. It accepts one SQL statement per run and rejects empty input, comments-only input, multiple statements, trailing tokens after a semicolon, and trigger creation, including temporary triggers. Semicolons inside strings, comments, or quoted identifiers are allowed.

Queries run asynchronously with cancellation. Results retain up to 500 rows and mark larger results as truncated. Cell values are made safe for terminal display, `NULL` values are shown as `NULL`, and long cells are capped at 300 runes. A failed query leaves the previous result table visible.

The application does not create SQLite databases, provide migrations, or offer a multi-statement script runner.

## Development checks

Run the product regression suite and quality checks:

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
gofmt -l cmd internal
```

The repository-wide `go test -race ./...` command also discovers pre-existing ignored example packages under `agent/skills/golang-cli/assets/examples`. Those samples have missing placeholder dependencies and are outside the product scope. They are not modified by the product checks above.
