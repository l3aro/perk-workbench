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

Or configure the connection with Laravel-compatible environment variables, then launch without an argument:

```bash
export DB_CONNECTION=mysql
export DB_HOST=127.0.0.1
export DB_PORT=3306
export DB_DATABASE=office
export DB_USERNAME=root
export DB_PASSWORD=secret
perk-workbench
```

Accepted `DB_CONNECTION` values are `sqlite`, `mysql`, and `pgsql`. SQLite requires `DB_DATABASE`; remote ports default to 3306 (MySQL) and 5432 (pgsql). A database argument passed on the command line overrides these variables. A `.env` file in the working directory is read as a fallback; real environment variables take precedence over it, and a command-line argument overrides both.

| Variable | Required | Default |
|---|---|---|
| `DB_CONNECTION` | yes | — (`sqlite`, `mysql`, or `pgsql`) |
| `DB_HOST` | `sqlite`: no · `mysql`/`pgsql`: yes | — |
| `DB_PORT` | no | `3306` (`mysql`) · `5432` (`pgsql`) |
| `DB_DATABASE` | `sqlite`: yes · `mysql`/`pgsql`: no | — |
| `DB_USERNAME` | `mysql`/`pgsql`: yes | — |
| `DB_PASSWORD` | no | empty |

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
