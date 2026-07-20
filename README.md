# Bubble Workbench

Bubble Workbench is a small terminal workbench for exploring an existing SQLite database. It opens one database, lists its tables and views, runs one SQL statement at a time, and shows the result or status in a Bubble Tea interface.

## Requirements

- Go 1.25 or newer
- A terminal with alternate screen support
- An existing SQLite database file, or the in-memory target `:memory:`

## Start

Run the workbench with an existing database:

```bash
go run ./cmd/bubble-workbench <database.db>
```

For a temporary database:

```bash
go run ./cmd/bubble-workbench :memory:
```

With no argument, the application opens a database picker. The picker includes `:memory:`, directories, and regular files whose names end in `.db`, `.sqlite`, or `.sqlite3`. It follows valid symlinks and omits broken links and unsupported files. A missing path supplied on the command line is not created. Press Enter on a database failure to return to the picker.

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
- `Enter` in the schema pane loads and runs the selected table or view's DDL query.
- `Escape` cancels an active query. In the editor, `Escape` switches from insert mode to normal mode.
- `q` quits when the editor is empty or another pane owns focus. In an editor with text, `q` is inserted as text.
- Raw `Ctrl+C` requests quit. If a query is running, the query is canceled first and the program exits after cancellation completes.

### Editor

The editor has insert mode and a small normal-mode Vim subset. Insert mode accepts SQL text. In normal mode, the available keys are `i`, `a`, `h`, `j`, `k`, `l`, `w`, `b`, `0`, `$`, `gg`, and `G`.

There are no operators, visual mode, command mode, registers, or syntax highlighting.

## SQL behavior

The workbench uses SQLite only. It accepts one SQL statement per run and rejects empty input, comments-only input, multiple statements, trailing tokens after a semicolon, and trigger creation, including temporary triggers. Semicolons inside strings, comments, or quoted identifiers are allowed.

Queries run asynchronously with cancellation. Results retain up to 500 rows and mark larger results as truncated. Cell values are made safe for terminal display, `NULL` values are shown as `NULL`, and long cells are capped at 300 runes. A failed query leaves the previous result table visible.

The application does not create databases, support other database engines, provide migrations, or offer a multi-statement script runner.

## Development checks

Run the product regression suite and quality checks:

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/bubble-workbench
gofmt -l cmd internal
```

The repository-wide `go test -race ./...` command also discovers pre-existing ignored example packages under `agent/skills/golang-cli/assets/examples`. Those samples have missing placeholder dependencies and are outside the product scope. They are not modified by the product checks above.
