# Perk Workbench Agent Guide

## Scope

- Go 1.27 module; the executables are `cmd/perk-workbench` and `cmd/perk-workbench-site`. `internal/workbench/app` owns Bubble Tea state, layout decisions, and async commands; sibling feature packages (`browse`, `chat`, `connection`, `notification`, `querylog`, `schema`, `uikit`, `profile`) are extracted from the shell. `internal/chrome` owns stateless terminal rendering helpers and must not import `workbench` packages or hold Bubble Tea state.
- `internal/database` selects SQLite, MySQL, PostgreSQL, or MongoDB; `internal/sql` defines their shared service and display contracts. Driver adapters live in `internal/drivers/` (`sqlite`, `mysql`, `postgres`, `mongodb`); keep driver-specific SQL in `internal/drivers/sqlite`, `internal/drivers/mysql`, or `internal/drivers/postgres`, and mongosh-style statement handling in `internal/drivers/mongodb`. Only `internal/database` may import concrete drivers in production code; the workbench and contract packages must not.
- Preserve the SQLite contract: only existing files open (`:memory:` is the exception); non-memory targets use read-write mode and must not create files. The shared statement validator accepts one statement and rejects trigger creation.
- Preserve query behavior in `workbench` and driver services: execution is asynchronous and cancelable, failed queries retain the prior result table, and display results cap at 500 rows and 300 runes per cell.
- Unified Go 1.27 module covering both binaries. Root `cmd/` and `internal/` patterns include the TUI (`cmd/perk-workbench`) and the product site (`cmd/perk-workbench-site`, `internal/site`, and the Vite sources in `frontend/`). Run `npm ci --include=optional` before website Go commands to install the local `vp` used by `npm run build`; the build must run first because the server embeds the generated `internal/site/assets/dist` manifest. Root `compose.yaml` is the website deployment; `demo/compose.yaml` is the database demo stack used by the Make targets.

## Website workflow

- After any website change, rebuild and recreate the Docker website service so embedded frontend assets and the Go binary are applied:
  `docker compose -p website up -d --build --force-recreate website`
- A plain container restart is insufficient for website changes because the frontend bundle is embedded in the built Go image.

## Refreshing homepage TUI screenshots

### Observed facts

- `internal/site/assets/tui.png` is the dark homepage capture. It was first added as `website/internal/site/assets/tui.png` in commit `94a0cba` (“Show real TUI capture on homepage”); after flattening, its current path is `internal/site/assets/tui.png`.
- `internal/site/assets/tui-light.png` is the light counterpart added by commit `77b7f2c` (“Add light terminal showcase theme”).
- Both are static, embedded PNG assets, exactly 1444x868 pixels. `internal/site/templates/pages/home.html` references them as `/static/tui.png` and `/static/tui-light.png`; they are separate from the live `/demo` xterm bridge.
- The old capture utility/command is not tracked in this repository. Do not invent or document a repository screenshot script.

### Practical capture method (not historical proof)

1. From the repository root, ensure `demo/chinook-sqlite.db` exists; run `make sqlite` if it does not. Frontend installation/build is not needed for a native TUI capture; run `npm ci --include=optional && npm run build` only before website Go commands or deployment.
2. Use one temporary XDG config for both captures so the real TUI has an explicit, deterministic appearance:

   ```bash
   capture_home="$(mktemp -d)"
   mkdir -p "$capture_home/perk-workbench"

   # Dark capture:
   printf '%s\n' '{"appearance":"dark","auto_theme":false}' \
     >"$capture_home/perk-workbench/config.json"
   XDG_CONFIG_HOME="$capture_home" \
     go run ./cmd/perk-workbench demo/chinook-sqlite.db

   # After capturing/quitting, change only the appearance for the light capture:
   printf '%s\n' '{"appearance":"light","auto_theme":false}' \
     >"$capture_home/perk-workbench/config.json"
   XDG_CONFIG_HOME="$capture_home" \
     go run ./cmd/perk-workbench demo/chinook-sqlite.db
   ```

   Use `go run ./cmd/perk-workbench --read-only --pin demo/chinook-sqlite.db` instead when reproducing the website bridge; direct captures may use the launch above without those flags.
3. Open the real TUI in a terminal emulator, not the `/demo` browser page. Keep the terminal at 120 columns x 36 rows and set the captured terminal surface to exactly 1444x868 pixels, matching the existing assets and the bridge PTY geometry. Use the OS/window or region screenshot facility available on the capture workstation; the historical utility and exact command are not tracked. Capture only the TUI surface—no browser chrome or surrounding terminal window—and do not claim a browser screenshot reproduces the homepage assets without checking the pixels. Preserve the same screen, selections, query text/results, scroll/cursor position, and composition in both images; only the dark/light appearance should differ.
4. Save the external captures with these exact names:
   `internal/site/assets/tui.png` (dark) and `internal/site/assets/tui-light.png` (light).
5. Verify dimensions/types and homepage references:

   ```bash
   file internal/site/assets/tui.png internal/site/assets/tui-light.png
   grep -nE 'src="/static/tui(-light)?\.png"|width="1444"|height="868"' \
     internal/site/templates/pages/home.html
   ```

   After asset or frontend changes, rebuild and recreate the website service:
   `docker compose -p website up -d --build --force-recreate website`.

## Development

```bash
# Product checks. Do not use `go test -race ./...`: it reaches ignored agent-skill examples with placeholder dependencies.
# Website checks require `npm ci --include=optional` first to install the local `vp`; run `npm run check` for frontend format, lint, and type checks, then `npm run build` so the embedded manifest exists.
go test -race ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/perk-workbench
go build ./cmd/perk-workbench-site
gofmt -l cmd internal

scripts/install.test.sh

# Focused checks
go test -race ./internal/drivers/sqlite -run 'TestServiceExecute|TestServiceRejects|TestOpenMissingFileDoesNotCreate'
go test -race ./internal/workbench/app -run 'TestExecute|TestPicker|TestResize'
go test -race ./cmd/perk-workbench -run TestParseTarget
```

## Running

```bash
go run ./cmd/perk-workbench demo/chinook-sqlite.db
go run ./cmd/perk-workbench path/to/database.db
make sqlite                 # demo SQLite database (recommended)
make mysql                  # starts MySQL and opens its office demo database
make postgres               # starts PostgreSQL and opens its employees demo database
make mongo                  # starts MongoDB and opens its atlas demo database (restaurants primer + Atlas sample datasets, re-seeded when missing)
```

- Compose mounts the source tree at `/workspace` and `demo/` at `/demo`; its default command opens `/demo/chinook-sqlite.db`.
- The TUI needs an alternate-screen terminal. For manual query QA, run `F5`; `Escape` cancels a running query.
