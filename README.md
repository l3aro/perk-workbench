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

Or install the prebuilt binary with Homebrew:

```bash
brew install l3aro/tap/perk-workbench
```

Or install the latest supported prebuilt binary into `~/.local/bin`:

```bash
curl -LSsf https://raw.githubusercontent.com/l3aro/perk-workbench/main/install.sh | bash
```

The installer detects Linux AMD64/ARM64, macOS ARM64, and Windows AMD64
(through Git Bash), downloads the matching GitHub Release archive, verifies its
SHA-256 checksum, and installs `perk-workbench` without requiring `jq` or
`fzf`. Pass a version to install a specific release:

```bash
curl -LSsf https://raw.githubusercontent.com/l3aro/perk-workbench/main/install.sh | bash -s -- 1.0.0
```

The script requires `curl`, `sha256sum` or `shasum`, and `tar` for Unix
targets; Windows Git Bash additionally requires `unzip`. Add `~/.local/bin` to
`PATH` if it is not already there.

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
cmd/perk-workbench/        CLI entry point and self-plugin dispatcher
cmd/perk-workbench-site/   Product site HTTP server (landing, docs, demo)
frontend/                  Vite + Tailwind sources for the site
internal/
├── workbench/             Bubble Tea models, layout, keybindings
├── site/                  Site handlers, templates, docs content, live TUI bridge
├── core/                  Workflow state machine (query lifecycle, focus, tabs)
├── database/              Plugin-aware connection dispatcher
├── database/plugin/       perk/v1 child lifecycle, loader, and shim
├── drivers/               SQLite, MySQL, PostgreSQL, MongoDB implementations
├── sql/                   Shared types and service contracts
├── chrome/                Stateless terminal rendering helpers
├── ai/                    AI clients (OpenAI, Anthropic, Gemini)
├── clipboard/             System clipboard access
└── log/                   Event logging
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

## Website

The product site — landing page, docs, search, and a live terminal demo — is
the `perk-workbench-site` binary in the same unified Go 1.27 module. Sources
live in `cmd/perk-workbench-site/`, `internal/site/`, and `frontend/`.

Prerequisites: Go 1.27 and Node.js 22 (when rebuilding the frontend). Run `npm ci --include=optional` to install the project's local `vp`; `npm run build` invokes that local CLI.

`npm run check` runs Vite+'s format, lint, and type checks against
`frontend/`. The frontend source is formatted with Oxfmt; Go formatting remains
handled by `gofmt`.

### Run locally

Build the frontend bundles first, then start the server:

```bash
npm ci --include=optional
npm run build
PORT=8080 go run ./cmd/perk-workbench-site
```

`PORT` is optional; when omitted the server listens on `8080`, and valid values
are `1` through `65535`. The frontend build is required before any site
`go run` or `go test`: `vite.config.mjs` configures Vite+ to write gitignored,
content-hashed bundles to `internal/site/assets/dist` (`.vite/manifest.json`
included), and the Go server reads that manifest from the embed at startup.
`npm run watch` rebuilds on change during development.

Build a versioned binary with the version linker flag:

```bash
go build -ldflags "-X main.version=0.1.0" -o ./bin/perk-workbench-site ./cmd/perk-workbench-site
```

### Routes

- `/` — product landing page
- `/demo` — live read-only terminal demo against the Chinook SQLite database
- `/ws/tui` — WebSocket bridge that runs the real TUI in a PTY (used by `/demo`)
- `/docs/getting-started` — installation and first-query guide
- `/docs/connections` — supported database connections
- `/docs/workspace` — workspace navigation, queries, schemas, and results
- `/docs/ai` — AI assistance overview
- `/docs/plugins` — plugin and workspace-view overview
- `/api/search?q=...` — case-insensitive catalogue search JSON backing the spotlight modal (queried as you type, keyboard-navigable)
- `/healthz` — health check
- `/static/` — embedded fonts and images; `/static/assets/` serves the content-hashed frontend bundles with immutable caching

### Deploy with Docker Compose

Root `compose.yaml` is the website deployment. The image compiles the current
repository TUI source (no pinned npm release), and `/demo` runs it with both
`--read-only` and `--pin`. Ensure the shared Traefik network exists, then copy
the environment template:

```bash
docker network create traefik_proxy
cp .env.example .env
```

Edit `.env` to set the hostname used by the Traefik router, then start:

```bash
docker compose up --build
```

`compose.override.yaml` adds the Traefik labels automatically. Set
`TRAEFIK_NETWORK` when the shared network uses a different name. Traefik
forwards to the container's internal port `8080`; `/healthz` is used for the
container healthcheck. Stop with:

```bash
docker compose down
```

Outside Docker, set `PERK_WORKBENCH_BIN` when the TUI binary is not on `PATH`
— it may point at any compatible local `perk-workbench` binary:

```bash
PERK_WORKBENCH_BIN=/path/to/perk-workbench PORT=8080 go run ./cmd/perk-workbench-site
```
## License

MIT
