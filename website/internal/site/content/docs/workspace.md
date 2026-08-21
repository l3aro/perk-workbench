---
title: Workspace
eyebrow: Documentation / keyboard-first workflow
lede: Move from structure to browse to query without losing your place. Every panel is part of one focused terminal workflow.
keywords: [workspace, queries, schema, results, keyboard, shortcuts]
---

## The workbench

<dl>
<dt><strong>Schema</strong></dt>
<dd>Browse tables, views, columns, types, indexes, and foreign keys.</dd>
<dt><strong>Workspace</strong></dt>
<dd>Keep query tabs, edit SQL or mongosh-style statements, and execute or cancel them asynchronously.</dd>
<dt><strong>Structure</strong></dt>
<dd>Inspect the fields and structure of the selected table or collection.</dd>
<dt><strong>Browse</strong></dt>
<dd>Explore records directly before writing a query.</dd>
<dt><strong>Query log</strong></dt>
<dd>Review executed statements and their outcomes.</dd>
<dt><strong>Diagrams</strong></dt>
<dd>See relationships between database objects when exposed by the backend.</dd>
</dl>

## Keybindings

| Key | Action |
| --- | --- |
| <kbd>F5</kbd> | Execute the current query. |
| <kbd>Ctrl</kbd>+<kbd>Enter</kbd> | Execute the current query from the editor. |
| <kbd>Ctrl</kbd>+<kbd>S</kbd> | Execute the current query. |
| <kbd>Escape</kbd> | Cancel a running query. |

## Results that stay useful

A failed or cancelled query retains the prior result table. Display output is capped at **500 rows**, and each cell is capped at **300 runes**.
