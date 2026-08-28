---
title: Getting started
eyebrow: Documentation / first run
order: 10
lede: Install the Perk Workbench CLI, point it at a database, and run your first query from the terminal.
keywords: [install, quickstart, first query, terminal, database]
---

## Install the CLI

The fastest path uses `npx`, no global install required:

```sh
npx perk-workbench path/to/database.db
```

For a command available everywhere, install globally:

```sh
npm install -g perk-workbench
perk-workbench path/to/database.db
```

If you already have the repository checked out, `go run ./cmd/perk-workbench path/to/database.db` builds and runs the same binary.

## Try the bundled demo databases

The repository ships four demo databases. The SQLite one runs anywhere; the others start a self-hosted container service:

```sh
make sqlite      # Chinook (SQLite)
make mysql       # Office (MySQL via Docker)
make postgres    # Employees (PostgreSQL via Docker)
make mongo       # Restaurants (MongoDB via Docker)
```

Launch the SQLite demo directly with:

```sh
npx perk-workbench demo/chinook-sqlite.db
```

## Your first query

1. **Launch** — start the workbench with a database target. See [Connections](/docs/connections) for file paths, remote targets, and environment-variable configuration.
2. **Select a schema object** — find a table or collection in the schema pane (`1`).
3. **Write a statement** — type SQL or a mongosh-style statement in the workspace editor (`2`).
4. **Execute** — run it with <kbd>F5</kbd>, <kbd>Ctrl</kbd>+<kbd>Enter</kbd>, or <kbd>Ctrl</kbd>+<kbd>S</kbd> and review the results.

Execution is asynchronous and cancelable with <kbd>Escape</kbd>. If a query fails, the previous result table stays visible.

## CLI options

| Option | Effect |
| --- | --- |
| `--select` | Choose a saved connection interactively; cannot be combined with a database target. |
| `--pin` | Lock the session: every quit affordance (keys, header button, palette entry) is disabled. |
| `--version`, `-v` | Print the build version (`perk-workbench <version>`, or `perk-workbench devel` for an uninjected build). |
| `-h`, `--help` | Show usage and the full option list. |

## The live demo website

The `/demo` page on this site streams the real TUI in a **read-only, pinned** session against the Chinook SQLite demo: queries run, but writes are rejected and the session cannot be quit. It is safe for exploration. For write-capable actions — editing rows, inserting documents, running mutations — install the CLI and open a local database instead.