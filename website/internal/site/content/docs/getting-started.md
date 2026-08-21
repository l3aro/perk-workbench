---
title: Getting started
eyebrow: Documentation / first run
lede: Install the CLI, point it at a database, and make your first query from an alternate-screen terminal.
keywords: [install, quickstart, first query, terminal, database]
---

## Install and launch

The fastest path uses `npx`:

```sh
npx perk-workbench path/to/database.db
```

For a command available everywhere, install globally:

```sh
npm install -g perk-workbench
perk-workbench path/to/database.db
```

## Open a demo database

Start the Chinook SQLite demo locally with:

```sh
npx perk-workbench demo/chinook-sqlite.db
```

Or use the Make target:

```sh
make sqlite
```

Other demo targets are `make mysql` for Office, `make postgres` for Employees, and `make mongo` for Restaurants.

## Your first query

1. Choose a database target at launch.
2. Find a table or collection in the schema pane.
3. Write a query and run it with <kbd>F5</kbd> or <kbd>Ctrl</kbd>+<kbd>Enter</kbd>.
4. Review the results; press <kbd>Escape</kbd> to cancel a running query.

Execution is asynchronous and cancelable. If a query fails, the previous result table stays visible.
