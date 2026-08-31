---
title: Keybindings
eyebrow: Documentation / keyboard reference
order: 30
lede: Every command has a stable ID, a default key, and a routing scope. Use this reference from the global shell controls down to workspace views, forms, and the SQL editor.
keywords: [keybindings, keyboard, shortcuts, workspace, schema, forms, editor]
---

## How keybindings are routed

Bindings are checked from the active context outward. Global commands are available throughout the app. View commands apply to the active workspace, connection, picker, overlay, or query-log view. Form commands take precedence while an edit form is active. Editor commands apply while the query editor has focus.

The same key can intentionally have different meanings in different views. For example, `a` adds a table in the schema view, inserts a row in Browse, and adds a profile on the connection screen. The command ID in the tables below is the key used in `config.json`.

## Global scope

Global bindings work regardless of the focused pane or active view.

<figure class="mt-8">
  <div class="overflow-hidden rounded-xl border border-line bg-[#0b0e14] shadow-deep [[data-theme=light]_&]:border-[var(--color-line-strong)] [[data-theme=light]_&]:bg-panel">
    <img class="block h-auto w-full [[data-theme=light]_&]:hidden" data-tui-theme-shot="dark" src="/static/tui.png" width="1444" height="868" alt="Perk Workbench dark TUI workspace preview for global keyboard commands">
    <img class="hidden h-auto w-full [[data-theme=light]_&]:block" data-tui-theme-shot="light" src="/static/tui-light.png" width="1444" height="868" alt="Perk Workbench light TUI workspace preview for global keyboard commands">
  </div>
  <figcaption class="mt-3 text-sm text-muted">Global controls are available from the main workbench shell.</figcaption>
</figure>

| Command ID | Default key | Action |
| --- | --- | --- |
| `app.quit` | <kbd>Ctrl</kbd>+<kbd>c</kbd> | Quit immediately. |
| `app.quit_dialog` | <kbd>Ctrl</kbd>+<kbd>q</kbd> | Quit with confirmation. |
| `editor.external` | <kbd>Ctrl</kbd>+<kbd>e</kbd> | Edit the current value in an external editor. |
| `query.execute` | <kbd>F5</kbd>, <kbd>Ctrl</kbd>+<kbd>enter</kbd>, <kbd>Ctrl</kbd>+<kbd>s</kbd> | Run the current query. |
| `query.cancel` | <kbd>Esc</kbd> | Cancel a running query. |
| `query.history` | <kbd>Ctrl</kbd>+<kbd>r</kbd> | Recall a previous query. |
| `focus.schema` | <kbd>1</kbd> | Focus the schema pane. |
| `app.palette` | <kbd>Ctrl</kbd>+<kbd>p</kbd> | Open the command palette. |
| `focus.workspace` | <kbd>2</kbd> | Focus the workspace pane. |
| `focus.query_log` | <kbd>3</kbd> | Focus the query log pane. |
| `focus.chat` | <kbd>4</kbd> | Focus the AI chat pane. |
| `ai.toggle` | <kbd>Ctrl</kbd>+<kbd>g</kbd> | Toggle the AI pane. |
| `focus.toggle_fullscreen` | <kbd>f</kbd> | Toggle fullscreen. |
| `focus.cycle_forward` | <kbd>Tab</kbd>, <kbd>]</kbd> | Focus the next pane. |
| `focus.cycle_backward` | <kbd>Shift</kbd>+<kbd>Tab</kbd>, <kbd>[</kbd> | Focus the previous pane. |

## Workspace and view scope

View bindings apply to the currently active workspace view. The available commands change with the selected tab or overlay.

<figure class="mt-8">
  <div class="overflow-hidden rounded-xl border border-line bg-[#0b0e14] shadow-deep [[data-theme=light]_&]:border-[var(--color-line-strong)] [[data-theme=light]_&]:bg-panel">
    <img class="block h-auto w-full [[data-theme=light]_&]:hidden" data-tui-theme-shot="dark" src="/static/tui.png" width="1444" height="868" alt="Perk Workbench dark TUI workspace and schema view preview">
    <img class="hidden h-auto w-full [[data-theme=light]_&]:block" data-tui-theme-shot="light" src="/static/tui-light.png" width="1444" height="868" alt="Perk Workbench light TUI workspace and schema view preview">
  </div>
  <figcaption class="mt-3 text-sm text-muted">Workspace and view commands follow the active tab and selected object.</figcaption>
</figure>

### Workspace navigation

| Command ID | Default key | Action |
| --- | --- | --- |
| `workspace.escape_to_schema` | <kbd>Esc</kbd> | Return from the workspace to the schema pane. |
| `workspace.tab_next` | <kbd>L</kbd> | Select the next workspace tab. |
| `workspace.tab_prev` | <kbd>H</kbd> | Select the previous workspace tab. |
| `workspace.view_reload` | <kbd>r</kbd> | Reload the active view. |

### Schema view

| Command ID | Default key | Action |
| --- | --- | --- |
| `schema.filter` | <kbd>/</kbd> | Filter tables or collections. |
| `schema.select_table` | <kbd>Enter</kbd> | Open the selected table or collection. |
| `schema.expand` | <kbd>Right</kbd>, <kbd>l</kbd> | Expand one tree level. |
| `schema.collapse` | <kbd>Left</kbd>, <kbd>h</kbd> | Collapse one tree level. |
| `schema.add_table` | <kbd>a</kbd> | Add a table. |
| `schema.create_database` | <kbd>Shift</kbd>+<kbd>A</kbd> | Create a database. |
| `schema.rename_table` | <kbd>m</kbd>, <kbd>r</kbd> | Rename the selected table. |
| `schema.delete_table` | <kbd>d</kbd> | Delete the selected table. |
| `schema.context_menu` | <kbd>,</kbd> | Open the schema context menu. |

### Structure view

| Command ID | Default key | Action |
| --- | --- | --- |
| `structure.filter` | <kbd>/</kbd> | Filter columns or fields. |
| `structure.reset` | <kbd>r</kbd> | Reset the column filter. |
| `structure.edit` | <kbd>Enter</kbd>, <kbd>i</kbd> | Edit the selected column or field. |
| `structure.add` | <kbd>a</kbd> | Add a column or field. |
| `structure.delete` | <kbd>d</kbd> | Delete the selected column or field. |

### Browse view

| Command ID | Default key | Action |
| --- | --- | --- |
| `browse.edit` | <kbd>Enter</kbd>, <kbd>e</kbd> | Edit the selected row. |
| `browse.insert_row` | <kbd>a</kbd> | Insert a row. |
| `browse.delete_row` | <kbd>d</kbd> | Delete the selected row. |
| `browse.add_table` | <kbd>a</kbd> | Add a table. |
| `browse.rename_table` | <kbd>e</kbd> | Rename or edit the table. |
| `browse.delete_table` | <kbd>d</kbd> | Delete the table. |
| `browse.edit_cell` | <kbd>i</kbd> | Edit the selected cell. |
| `cell.view` | <kbd>v</kbd> | View the complete cell value. |
| `browse.refine` | <kbd>/</kbd> | Set a filter and row limit. |
| `browse.reset` | <kbd>r</kbd> | Reset Browse filters. |
| `browse.sort` | <kbd>s</kbd> | Sort by the selected column. |
| `browse.next_page` | <kbd>n</kbd> | Go to the next page. |
| `browse.prev_page` | <kbd>p</kbd> | Go to the previous page. |
| `cell.yank` | <kbd>y</kbd> | Copy the selected cell. |
| `browse.context_menu` | <kbd>,</kbd> | Open the Browse context menu. |

### Indexes and foreign keys

| Command ID | Default key | Action |
| --- | --- | --- |
| `indexes.filter` | <kbd>/</kbd> | Filter indexes. |
| `indexes.reset` | <kbd>r</kbd> | Reset the index filter. |
| `indexes.toggle_diagram` | <kbd>g</kbd> | Toggle the index diagram. |
| `indexes.create` | <kbd>n</kbd> | Create an index. |
| `indexes.edit` | <kbd>Enter</kbd>, <kbd>i</kbd> | Edit the selected index. |
| `indexes.delete` | <kbd>d</kbd> | Delete the selected index. |
| `diagram.depth_up` | <kbd>}</kbd> | Increase diagram focus depth. |
| `diagram.depth_down` | <kbd>{</kbd> | Decrease diagram focus depth. |
| `foreign_keys.filter` | <kbd>/</kbd> | Filter foreign keys. |
| `foreign_keys.reset` | <kbd>r</kbd> | Reset the foreign-key filter. |
| `foreign_keys.toggle_diagram` | <kbd>g</kbd> | Toggle the foreign-key diagram. |
| `foreign_keys.create` | <kbd>n</kbd> | Create a foreign key. |
| `foreign_keys.edit` | <kbd>Enter</kbd>, <kbd>i</kbd> | Edit the selected foreign key. |
| `foreign_keys.delete` | <kbd>d</kbd> | Delete the selected foreign key. |

### Query log and overlays

| Command ID | Default key | Action |
| --- | --- | --- |
| `query_log.yank` | <kbd>y</kbd> | Copy the selected query-log value. |
| `query_log.explain` | <kbd>e</kbd> | Explain the selected query. |
| `query_log.detail` | <kbd>Enter</kbd> | Open query details. |
| `query_log.context_menu` | <kbd>,</kbd> | Open the query-log context menu. |
| `query_log.top_first` | <kbd>g</kbd> | Jump to the first query-log entry. |
| `query_log.top_last` | <kbd>G</kbd> | Jump to the last query-log entry. |
| `query_log.next_page` | <kbd>n</kbd> | Go to the next query-log page. |
| `query_log.prev_page` | <kbd>p</kbd> | Go to the previous query-log page. |
| `chat.delete` | <kbd>Ctrl</kbd>+<kbd>d</kbd> | Delete the selected chat. |
| `chat.clear` | <kbd>Ctrl</kbd>+<kbd>l</kbd> | Clear chats. |
| `chat.apply_sql` | <kbd>Ctrl</kbd>+<kbd>a</kbd> | Apply generated SQL. |
| `detail.yank` | <kbd>y</kbd> | Copy the detail value. |
| `detail.explain` | <kbd>e</kbd> | Explain the selected statement. |
| `detail.close` | <kbd>Enter</kbd>, <kbd>Esc</kbd> | Close the detail overlay. |

### Pickers and connection lists

| Command ID | Default key | Action |
| --- | --- | --- |
| `picker.reload` | <kbd>r</kbd> | Reload the picker. |
| `picker.select` | <kbd>Enter</kbd> | Open the selected item. |
| `failure.return_to_picker` | <kbd>Enter</kbd>, <kbd>Esc</kbd> | Return to the picker after a failure. |
| `connection.switch_to_form` | <kbd>2</kbd> | Switch from profiles to the connection form. |
| `connection.filter` | <kbd>/</kbd> | Filter saved profiles. |
| `connection.add` | <kbd>a</kbd> | Add a profile. |
| `connection.edit` | <kbd>e</kbd>, <kbd>Enter</kbd> | Edit the selected profile. |
| `connection.delete` | <kbd>d</kbd> | Delete the selected profile. |
| `connection.context_menu` | <kbd>,</kbd> | Open the profile context menu. |

### Connection form view

| Command ID | Default key | Action |
| --- | --- | --- |
| `connection.execute` | <kbd>F5</kbd>, <kbd>Ctrl</kbd>+<kbd>enter</kbd>, <kbd>Ctrl</kbd>+<kbd>s</kbd> | Connect using the current form values. |
| `connection.edit_field` | <kbd>Enter</kbd> | Edit the selected connection field. |
| `connection.field_next` | <kbd>j</kbd>, <kbd>Down</kbd> | Move to the next connection field. |
| `connection.field_prev` | <kbd>k</kbd>, <kbd>Up</kbd> | Move to the previous connection field. |
| `connection.switch_to_list` | <kbd>1</kbd> | Switch from the form to saved profiles. |
| `connection.action_enter` | <kbd>Enter</kbd> | Activate the focused connection action. |

### Conditional Browse form view

| Command ID | Default key | Action |
| --- | --- | --- |
| `browse_form.set_null` | <kbd>n</kbd> | Set the selected value to `NULL`. |
| `browse_form.set_default` | <kbd>N</kbd> | Set the selected value to its default. |
| `browse_form.field_top` | <kbd>g</kbd> | Jump to the first form field. |
| `browse_form.field_bottom` | <kbd>G</kbd> | Jump to the last form field. |

## Form scope

Form bindings apply while editing columns, rows, indexes, foreign keys, or filters. They take precedence over the view binding for the same key.

<figure class="mt-8">
  <div class="overflow-hidden rounded-xl border border-line bg-[#0b0e14] shadow-deep [[data-theme=light]_&]:border-[var(--color-line-strong)] [[data-theme=light]_&]:bg-panel">
    <img class="block h-auto w-full [[data-theme=light]_&]:hidden" data-tui-theme-shot="dark" src="/static/connection.png" width="1444" height="868" alt="Perk Workbench dark connection form preview for form keyboard commands">
    <img class="hidden h-auto w-full [[data-theme=light]_&]:block" data-tui-theme-shot="light" src="/static/connection-light.png" width="1444" height="868" alt="Perk Workbench light connection form preview for form keyboard commands">
  </div>
  <figcaption class="mt-3 text-sm text-muted">Forms use the same navigation and save controls across editable views.</figcaption>
</figure>

| Command ID | Default key | Action |
| --- | --- | --- |
| `editor.complete` | <kbd>Ctrl</kbd>+<kbd>Space</kbd> | Complete a value in a form. |
| `form.edit` | <kbd>Enter</kbd> | Begin editing the focused field. |
| `form.save` | <kbd>Ctrl</kbd>+<kbd>enter</kbd>, <kbd>Ctrl</kbd>+<kbd>s</kbd>, <kbd>F5</kbd> | Save the form. |
| `form.discard` | <kbd>Esc</kbd> | Discard form changes. |
| `form.field_next` | <kbd>j</kbd>, <kbd>Down</kbd> | Move to the next form field. |
| `form.field_prev` | <kbd>k</kbd>, <kbd>Up</kbd> | Move to the previous form field. |
| `browse_filter.apply` | <kbd>F5</kbd>, <kbd>Ctrl</kbd>+<kbd>s</kbd> | Apply Browse filters. |
| `form.delete` | <kbd>d</kbd> | Delete the selected index or foreign key from its form. |

## Editor scope

The query editor has one editor-specific binding. Query execution remains global, so it is also available while the editor is focused.

<figure class="mt-8">
  <div class="overflow-hidden rounded-xl border border-line bg-[#0b0e14] shadow-deep [[data-theme=light]_&]:border-[var(--color-line-strong)] [[data-theme=light]_&]:bg-panel">
    <img class="block h-auto w-full [[data-theme=light]_&]:hidden" data-tui-theme-shot="dark" src="/static/tui.png" width="1444" height="868" alt="Perk Workbench dark SQL editor preview for editor keyboard commands">
    <img class="hidden h-auto w-full [[data-theme=light]_&]:block" data-tui-theme-shot="light" src="/static/tui-light.png" width="1444" height="868" alt="Perk Workbench light SQL editor preview for editor keyboard commands">
  </div>
  <figcaption class="mt-3 text-sm text-muted">The workspace editor keeps completion local while execution and cancellation remain global.</figcaption>
</figure>

| Command ID | Default key | Action |
| --- | --- | --- |
| `query.complete` | <kbd>Ctrl</kbd>+<kbd>Space</kbd> | Complete SQL or mongosh-style input. |

## Customizing bindings

Add a `keybinds` object to `config.json` to replace a command's defaults. The command ID is the object key and the value is an array of keystrokes. An empty array disables that command. Flat and nested forms are both accepted:

```json
{
  "keybinds": {
    "app.quit": ["ctrl+q"],
    "browse.next_page": ["n", "pgdown"],
    "form.save": []
  }
}
```

Use lowercase names for modifiers and named keys (`ctrl`, `shift`, `enter`, `escape`, and so on). Uppercase rune keys such as `L`, `A`, `N`, and `G` represent shifted letter input. Unknown command IDs, invalid keystrokes, and duplicate modifiers are rejected when the app starts.

The command palette (<kbd>Ctrl</kbd>+<kbd>p</kbd>) remains the quickest way to discover what the active context can do. Commands that are not listed in `config.json` retain their defaults.
