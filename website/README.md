# Perk Workbench Site

The independent website service for Perk Workbench. This Go module serves the
product landing page, documentation pages, search, health status, and embedded
static assets; it is separate from the terminal application.

## Prerequisites

- Go 1.26.6
- Node.js 22 (when rebuilding the frontend)

## Run locally

Build the frontend bundles first (see below), then start the server:

```bash
npm run build
PORT=8080 go run ./cmd/perk-workbench-site
```

`PORT` is optional. When omitted, the server listens on port `8080`; valid
values are `1` through `65535`.

## Frontend build

The site's frontend is built with Vite + Tailwind CSS 4. Sources live in
`frontend/`:

- `site.css` — Tailwind theme tokens, base styles, and component classes
- `app.js` — site-wide behaviours (bundles HTMX for search)
- `demo.js` — the live terminal demo (bundles xterm.js)

Build the bundles:

```bash
npm ci
npm run build
```

Vite compiles the sources to `internal/site/assets/dist`, content-hashing every
filename (`assets/site-d41d8cd9.css`, `assets/demo-b6f3a2e1.js`, …) and writing
`.vite/manifest.json` with the mapping. Any frontend change therefore produces
new URLs automatically; the Go server reads the embedded manifest at startup
and emits those hashed URLs into the templates, so stale caches can never serve
an old bundle for a new page.

`internal/site/assets/dist` is a generated, gitignored artifact. The server
reads its embedded manifest at startup, so a fresh checkout must run
`npm run build` before any `go run` or `go test`. CI builds the frontend first
for the same reason, and the Dockerfile produces dist in a Node stage so the
image builds without it being in the repo. `npm run watch` rebuilds on change
during development (restart `go run` to pick up the new embed).

## Run with Docker Compose

The default Compose configuration routes the site through Traefik without
publishing the application port on the host. The image builds both this site
and the Perk Workbench TUI from the sibling `source/` repository, so the build
context is the parent workspace directory. Ensure the shared Traefik network
exists:

```bash
docker network create traefik_proxy
```

Copy the environment template to `.env` and set the hostname used by the
Traefik router:

```bash
cp .env.example .env
```

Docker Compose loads `.env` automatically from the project directory. Start the
site with:

```bash
docker compose up --build
```

The `compose.override.yaml` file adds the Traefik labels automatically. Set
`TRAEFIK_NETWORK` when the shared network uses a different name. Traefik
forwards to the container's internal port `8080`; `/healthz` is used for the
container healthcheck. Stop it with:

```bash
docker compose down
```

## Live terminal demo

The `/demo` page streams the real Bubble Tea application into a browser
terminal (xterm.js) over a WebSocket. Each session spawns the TUI against the
embedded Chinook SQLite demo database in read-only mode with an isolated
config directory.

Set `PERK_WORKBENCH_BIN` when running outside Docker and the `perk-workbench`
binary is not on `PATH`:

```bash
PERK_WORKBENCH_BIN=/path/to/perk-workbench PORT=8080 go run ./cmd/perk-workbench-site
```

## Build

Build a versioned binary with the version linker flag:

```bash
go build -ldflags "-X main.version=0.1.0" -o ./bin/perk-workbench-site ./cmd/perk-workbench-site
```

## Checks

Run the same checks used by the repository workflow (frontend build first, so
the embedded bundles exist for the Go steps):

```bash
npm ci
npm run build
go test -race ./...
go vet ./...
gofmt -l cmd internal
go build ./cmd/perk-workbench-site
```

`gofmt -l cmd internal` prints Go files that need formatting. `dist` is
gitignored, so there is no committed copy to drift from; CI always builds it
fresh immediately before compiling Go.

## Routes

- `/` — product landing page
- `/demo` — live read-only terminal demo against the Chinook SQLite database
- `/ws/tui` — WebSocket bridge that runs the real TUI in a PTY (used by `/demo`)
- `/docs/getting-started` — installation and first-query guide
- `/docs/connections` — supported database connections
- `/docs/workspace` — workspace navigation, queries, schemas, and results
- `/docs/ai` — AI assistance overview
- `/docs/plugins` — plugin and workspace-view overview
- `/search?q=...` — case-insensitive catalogue search; HTMX requests receive a fragment
- `/healthz` — health check
- `/static/` — embedded fonts and images; `/static/assets/` serves the content-hashed frontend bundles with immutable caching

## Repository boundary

This repository contains only the independent website module. It does not
contain or modify the Perk Workbench source application or the sibling
`perk-redis` repository. Changes to those repositories are outside this
module's build and validation commands.
