# Perk Agent Guide

## Scope

- Go 1.25 module; the executable is `cmd/perk`. `internal/workbench` owns Bubble Tea state, layout decisions, and async commands. `internal/chrome` owns stateless terminal rendering helpers and must not import `workbench` or hold Bubble Tea state.
- `internal/database` selects SQLite, MySQL, or PostgreSQL; `internal/sql` defines their shared service and display contracts. Keep driver-specific SQL in `internal/sqlite`, `internal/mysql`, or `internal/postgres`.
- Preserve the SQLite contract: only existing files open (`:memory:` is the exception); non-memory targets use read-write mode and must not create files. The shared statement validator accepts one statement and rejects trigger creation.
- Preserve query behavior in `workbench` and driver services: execution is asynchronous and cancelable, failed queries retain the prior result table, and display results cap at 500 rows and 300 runes per cell.

## Development

```bash
# Product checks. Do not use `go test -race ./...`: it reaches ignored agent-skill examples with placeholder dependencies.
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk
gofmt -l cmd internal

# Focused checks
go test -race ./internal/sqlite -run 'TestServiceExecute|TestServiceRejects|TestOpenMissingFileDoesNotCreate'
go test -race ./internal/workbench -run 'TestExecute|TestPicker|TestResize'
go test -race ./cmd/perk -run TestParseTarget
```

## Running

```bash
go run ./cmd/perk demo/chinook-sqlite.db
go run ./cmd/perk path/to/database.db
make sqlite                 # demo SQLite database (recommended)
make mysql                  # starts MySQL and opens its office demo database
make postgres               # starts PostgreSQL and opens its employees demo database
```
- Compose mounts the source tree at `/workspace` and `demo/` at `/demo`; its default command opens `/demo/chinook-sqlite.db`.
- The TUI needs an alternate-screen terminal. For manual query QA, run `F5`; `Escape` cancels a running query.
