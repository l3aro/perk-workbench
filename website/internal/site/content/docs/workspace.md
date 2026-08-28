---
title: Workspace
eyebrow: Documentation / keyboard-first workflow
order: 30
lede: Move from schema to workspace to query log to AI without losing your place. Every panel is part of one focused terminal workflow.
keywords: [workspace, queries, schema, results, keyboard, shortcuts]
---

## The panes

The workbench splits into four panes, focused with the number keys:

| Key | Pane | What it shows |
| --- | --- | --- |
| <kbd>1</kbd> | Schema | Tables and collections, columns, types, indexes, and foreign keys. |
| <kbd>2</kbd> | Workspace | The tabbed editor and results area — query, structure, browse, indexes, foreign keys. |
| <kbd>3</kbd> | Query log | Every executed statement and its outcome. |
| <kbd>4</kbd> | AI chat | Natural-language assistance against the current schema and results, when configured. |

<kbd>Tab</kbd> and <kbd>Shift</kbd>+<kbd>Tab</kbd> cycle focus forward and backward between panes.

## The workspace tabs

Selecting a table or collection in the schema pane opens its tabs in the workspace:

- **Query** — write SQL or mongosh-style statements, execute them, and inspect results.
- **Structure** — the fields/columns of the selected object.
- **Browse** — explore records directly before writing a query.
- **Indexes** — index definitions for the selected object.
- **Foreign keys** — relationships between objects.

## Run, cancel, recall

| Key | Action |
| --- | --- |
| <kbd>F5</kbd>, <kbd>Ctrl</kbd>+<kbd>Enter</kbd>, <kbd>Ctrl</kbd>+<kbd>S</kbd> | Execute the current query. |
| <kbd>Escape</kbd> | Cancel a running query; also steps back from an active view. |
| <kbd>Ctrl</kbd>+<kbd>R</kbd> | Recall a previous query. |

Execution is asynchronous. A failed or cancelled query retains the prior result table, so you never lose the last good output.

## Browse records

The browse tab is filtered and paged: refine with a filter and row limit (`/`), sort a column (`s`), and page with `n`/`p`. Results are bounded — display output is capped at **500 rows**, and each cell is capped at **300 runes**.

Row and document edits are capability-gated: `enter` to edit, `a` to insert, `d` to delete appear only when the open backend actually supports writes. SQL backends edit one row; document stores (MongoDB) edit the whole document. Every write is entered in a form and confirmed with an explicit save key, and a read-only connection rejects writes entirely.

## Default keys

| Key | Action |
| --- | --- |
| <kbd>1</kbd> / <kbd>2</kbd> / <kbd>3</kbd> / <kbd>4</kbd> | Focus schema / workspace / query log / AI chat. |
| <kbd>Tab</kbd> / <kbd>Shift</kbd>+<kbd>Tab</kbd> | Cycle focus forward / backward. |
| <kbd>F5</kbd> / <kbd>Ctrl</kbd>+<kbd>Enter</kbd> / <kbd>Ctrl</kbd>+<kbd>S</kbd> | Execute the current query. |
| <kbd>Escape</kbd> | Cancel a running query. |
| <kbd>Ctrl</kbd>+<kbd>R</kbd> | Recall a previous query. |
| <kbd>Ctrl</kbd>+<kbd>P</kbd> | Open the command palette. |
| <kbd>Ctrl</kbd>+<kbd>Space</kbd> | Completion in the editor or a form. |

Most actions are context-sensitive — the same key can mean different things in the schema pane, a tab view, or a form. The command palette (<kbd>Ctrl</kbd>+<kbd>P</kbd>) lists what is available in the current context, and `config.json` keybinds can rebind any command.