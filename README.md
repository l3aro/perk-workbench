# Perk Workbench

A terminal UI for exploring databases. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

```text
  ┌─────────────────────────────────────────────┐
  │  perk-workbench chinook-sqlite.db           │
  │                                             │
  │  ┌── Schema ─────┐ ┌── Workspace (tab) ───┐│
  │  │  artists       │ │ SELECT * FROM        ││
  │  │  albums        │ │ artists LIMIT 5;     ││
  │  │  tracks        │ │                      ││
  │  │  ...           │ │ ┌─ Results ────────┐││
  │  │                │ │ │ 1 | AC/DC        │││
  │  │                │ │ │ 2 | Accept       │││
  │  └────────────────┘ │ └──────────────────┘││
  │                      └─────────────────────┘│
  └─────────────────────────────────────────────┘
```

## Quick start

```bash
npx perk-workbench path/to/database.db
```

Or install globally:

```bash
npm install -g perk-workbench
perk-workbench path/to/database.db
```

Connect to MySQL and PostgreSQL:

```bash
perk-workbench 'mysql:user:pass@tcp(host:3306)/db'
perk-workbench 'postgres://user:pass@host:5432/db'
```

## Features

| | |
|---|---|
| **Browse schemas** | Tables, views, columns, types, indexes, foreign keys |
| **Run queries** | Write, execute, and cancel SQL queries |
| **AI assist** | Natural language to SQL (OpenAI, Claude, Gemini) |
| **3 backends** | SQLite, MySQL, PostgreSQL via a shared query interface |
| **Configurable** | TLS support for MySQL/Postgres, custom keybindings |

## Architecture

```
cmd/perk-workbench/    CLI entry point
internal/
├── workbench/         Bubble Tea models, layout, keybindings
├── core/              Workflow state machine (query lifecycle, focus, tabs)
├── database/          Connection dispatcher (routes DSN to driver)
├── sql/               Shared types & contracts (Service, Column, Rows)
├── sqlite/            SQLite driver (modernc.org/sqlite, no CGO)
├── mysql/             MySQL driver
├── postgres/          PostgreSQL driver
├── chrome/            Stateless terminal rendering helpers
├── ai/                AI clients (OpenAI, Anthropic, Gemini)
├── clipboard/         System clipboard access
└── log/               Event logging
```

## Development

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
gofmt -l cmd internal
```

Demo databases for testing:

```bash
make sqlite      # Chinook (SQLite)
make mysql       # Office demo (MySQL)
make postgres    # Employees demo (PostgreSQL)
```

## License

MIT
