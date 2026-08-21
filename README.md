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
go run ./cmd/perk-workbench demo/chinook-sqlite.db
```

The same command is the normal installed-binary form:

```bash
npx perk-workbench path/to/database.db
```

The four bundled database modes are self-hosted child processes speaking
perk/v1 over NDJSON. They are not in-process drivers:

```bash
perk-workbench --plugin sqlite
perk-workbench --plugin mysql
perk-workbench --plugin postgres
perk-workbench --plugin mongodb
```

Connect to MySQL, PostgreSQL, and MongoDB:

```bash
perk-workbench 'mysql:user:pass@tcp(host:3306)/db'
perk-workbench 'postgres://user:pass@host:5432/db'
perk-workbench 'mongodb://user:pass@host:27017/db'
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

## Credential storage

Connection profiles are saved to `connections.json` under your user
config directory (`~/.config/perk-workbench/`). Literal passwords are
encrypted at rest with AES-256-GCM using a key in `secret.key` inside
the same directory; each ciphertext is bound to its profile and field,
and the directory and files are locked down to 0700/0600. Connection
targets that carry credentials — `redis://user:pass@host`,
`mongodb://user:pass@host/db`, `postgres://user@host/db?password=…`,
`mysql:user:pass@tcp(host:3306)/db` — are encrypted the same way;
non-credential targets (file paths, database names) stay plaintext and
readable.

**Threat model.** The key and the ciphertext live under the same
user-owned directory, so encryption protects against accidental
disclosure and copies of your config (backups, shared screenshots,
misplaced files) — **not** against an attacker with your account
access, who can read both. Env and file references are the stronger
separation:

```bash
perk-workbench                # with DB_PASSWORD set, or
# ${MY_PASSWORD} / file:///path/to/secret in the connection form
```

A stored password that cannot be decrypted (tampered file, replaced
key) is never shown as plaintext and is never rewritten: the app
reports it and refuses to save until you re-enter the value, so a
transient key problem cannot silently destroy your stored ciphertext.

## Features

| | |
|---|---|
| **Browse schemas** | Tables, views, columns, types, indexes, foreign keys |
| **Run queries** | Write, execute, and cancel SQL or mongosh-style queries |
| **AI assist** | Natural language to SQL (OpenAI, Claude, Gemini) |
| **4 backends** | SQLite, MySQL, PostgreSQL, MongoDB via a shared query interface |
| **Configurable** | TLS support for MySQL/Postgres, custom keybindings, `config.json` defaults |

## Architecture

```
cmd/perk-workbench/    CLI entry point and self-plugin dispatcher
internal/
├── workbench/         Bubble Tea models, layout, keybindings
├── core/              Workflow state machine (query lifecycle, focus, tabs)
├── database/          Plugin-aware connection dispatcher
├── database/plugin/   perk/v1 child lifecycle, loader, and shim
├── drivers/           SQLite, MySQL, PostgreSQL, MongoDB implementations
├── sql/               Shared types and service contracts
├── chrome/            Stateless terminal rendering helpers
├── ai/                AI clients (OpenAI, Anthropic, Gemini)
├── clipboard/         System clipboard access
└── log/               Event logging
```

Built-in config entries are `{"builtin":"sqlite"}` (and the other three
families). External entries are `{"path":"…","sha256":"…"}`; the digest is
optional and applies only to external executables. Plugin identity is
separate from database family, so multiple plugin IDs can advertise
`driver: "mysql"` and remain independently selectable. The four bundled
implementations ship in every host binary; the official driver repositories
do not publish independent release assets.

## Development

```bash
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
gofmt -l cmd internal
```

The build version is injected explicitly — never baked in:

```bash
go build -ldflags "-X main.version=<version>" ./cmd/perk-workbench
perk-workbench --version
```

`--version` prints `perk-workbench <version>`; a build without the
injection honestly reports `perk-workbench devel`.

Demo databases for testing:

```bash
make sqlite      # direct SQLite self-plugin with Chinook
make mysql       # Office demo (MySQL via Docker)
make postgres    # Employees demo (PostgreSQL via Docker)
make mongo       # Restaurants demo (MongoDB via Docker)
```

## License

MIT
