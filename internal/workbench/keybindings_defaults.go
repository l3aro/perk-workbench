package workbench

// defaultDefs is the definition list for all built-in commands.
// Stored as a list (not a Keybindings value) to break the init cycle:
// defaultBindings is built from defaultDefs via buildFromDefs, and
// NewKeybindings also uses defaultDefs as a base before applying overrides.
var defaultDefs = []commandDef{
	// ── Global ──────────────────────────────────────────────────
	{id: "app.quit", scope: scopeGlobal, keys: []string{"ctrl+c"}, label: "quit"},
	{id: "app.quit_dialog", scope: scopeGlobal, keys: []string{"ctrl+q"}, label: "quit with confirm"},
	{id: "editor.external", scope: scopeGlobal, keys: []string{"ctrl+e"}, label: "edit"},
	{id: "editor.complete", scope: scopeForm, keys: []string{"ctrl+space"}, label: "complete"},
	{id: "query.execute", scope: scopeGlobal, keys: []string{"f5", "ctrl+enter", "ctrl+s"}, label: "run"},
	{id: "query.cancel", scope: scopeGlobal, keys: []string{"esc"}, label: "cancel"},
	{id: "query.history", scope: scopeGlobal, keys: []string{"ctrl+r"}, label: "recall query"},
	{id: "focus.schema", scope: scopeGlobal, keys: []string{"1"}, label: "schema"},
	{id: "app.palette", scope: scopeGlobal, keys: []string{"ctrl+p"}, label: "palette"},
	{id: "focus.workspace", scope: scopeGlobal, keys: []string{"2"}, label: "workspace"},
	{id: "focus.query_log", scope: scopeGlobal, keys: []string{"3"}, label: "log"},
	{id: "focus.chat", scope: scopeGlobal, keys: []string{"4"}, label: "AI chat"},
	{id: "ai.toggle", scope: scopeGlobal, keys: []string{"ctrl+g"}, label: "toggle AI"},
	{id: "focus.toggle_fullscreen", scope: scopeGlobal, keys: []string{"f"}, label: "fullscreen"},
	{id: "focus.cycle_forward", scope: scopeGlobal, keys: []string{"tab", "]"}, label: "next"},
	{id: "focus.cycle_backward", scope: scopeGlobal, keys: []string{"shift+tab", "["}, label: "prev"},

	// ── Workspace (active when focus=workspace, no form active) ─
	{id: "workspace.escape_to_schema", scope: scopeView, keys: []string{"esc"}, label: "back"},
	{id: "workspace.tab_next", scope: scopeView, keys: []string{"L"}, label: "next tab"},
	{id: "workspace.tab_prev", scope: scopeView, keys: []string{"H"}, label: "prev tab"},

	// ── Schema tab (view) ───────────────────────────────────────
	{id: "schema.select_table", scope: scopeView, keys: []string{"enter"}, label: "open"},

	// ── Structure tab (view) ────────────────────────────────────
	{id: "structure.filter", scope: scopeView, keys: []string{"/"}, label: "filter columns"},
	{id: "structure.reset", scope: scopeView, keys: []string{"r"}, label: "reset column filter"},
	{id: "structure.edit", scope: scopeView, keys: []string{"enter", "i"}, label: "edit column"},

	// ── Browse tab (view) ───────────────────────────────────────
	{id: "browse.edit", scope: scopeView, keys: []string{"enter"}, label: "edit row"},
	{id: "browse.edit_cell", scope: scopeView, keys: []string{"i"}, label: "edit cell"},
	{id: "browse.refine", scope: scopeView, keys: []string{"/"}, label: "filter and row limit"},
	{id: "browse.reset", scope: scopeView, keys: []string{"r"}, label: "reset filters"},
	{id: "browse.sort", scope: scopeView, keys: []string{"s"}, label: "sort column"},
	{id: "browse.next_page", scope: scopeView, keys: []string{"n"}, label: "next"},
	{id: "browse.prev_page", scope: scopeView, keys: []string{"p"}, label: "prev"},
	{id: "browse.yank_cell", scope: scopeView, keys: []string{"y"}, label: "copy cell"},
	{id: "browse.context_menu", scope: scopeView, keys: []string{","}, label: "context menu"},

	// ── Indexes tab (view) ──────────────────────────────────────
	{id: "indexes.filter", scope: scopeView, keys: []string{"/"}, label: "filter indexes"},
	{id: "indexes.reset", scope: scopeView, keys: []string{"r"}, label: "reset index filter"},
	{id: "indexes.create", scope: scopeView, keys: []string{"n"}, label: "new index"},
	{id: "indexes.edit", scope: scopeView, keys: []string{"enter", "i"}, label: "edit index"},
	{id: "indexes.delete", scope: scopeView, keys: []string{"d"}, label: "delete index"},

	// ── Foreign Keys tab (view) ─────────────────────────────────
	{id: "foreign_keys.filter", scope: scopeView, keys: []string{"/"}, label: "filter foreign keys"},
	{id: "foreign_keys.reset", scope: scopeView, keys: []string{"r"}, label: "reset foreign key filter"},
	{id: "foreign_keys.toggle_diagram", scope: scopeView, keys: []string{"g"}, label: "diagram"},
	{id: "foreign_keys.create", scope: scopeView, keys: []string{"n"}, label: "new FK"},
	{id: "foreign_keys.edit", scope: scopeView, keys: []string{"enter", "i"}, label: "edit FK"},
	{id: "foreign_keys.delete", scope: scopeView, keys: []string{"d"}, label: "delete FK"},

	// ── Browse form (view, conditional) ─────────────────────────
	{id: "browse_form.set_null", scope: scopeView, keys: []string{"n"}, label: "null"},
	{id: "browse_form.field_top", scope: scopeView, keys: []string{"g"}, label: "top"},
	{id: "browse_form.field_bottom", scope: scopeView, keys: []string{"G"}, label: "bottom"},

	// ── Query Log pane (view) ───────────────────────────────────
	{id: "query_log.yank", scope: scopeView, keys: []string{"y"}, label: "copy"},
	{id: "query_log.explain", scope: scopeView, keys: []string{"e"}, label: "explain"},
	{id: "query_log.detail", scope: scopeView, keys: []string{"enter"}, label: "detail"},
	{id: "query_log.top_first", scope: scopeView, keys: []string{"g"}, label: "top"},
	{id: "query_log.top_last", scope: scopeView, keys: []string{"G"}, label: "bottom"},
	{id: "query_log.next_page", scope: scopeView, keys: []string{"n"}, label: "next page"},
	{id: "query_log.prev_page", scope: scopeView, keys: []string{"p"}, label: "prev page"},
	{id: "chat.new", scope: scopeView, keys: []string{"ctrl+n"}, label: "new chat"},
	{id: "chat.history", scope: scopeView, keys: []string{"ctrl+h"}, label: "chat history"},
	{id: "chat.delete", scope: scopeView, keys: []string{"ctrl+d"}, label: "delete chat"},
	{id: "chat.clear", scope: scopeView, keys: []string{"ctrl+l"}, label: "clear chats"},
	{id: "chat.apply_sql", scope: scopeView, keys: []string{"ctrl+a"}, label: "apply SQL"},
	{id: "chat.share_results", scope: scopeView, keys: []string{"ctrl+shift+r"}, label: "share result rows"},

	// ── Query Log Detail overlay (view) ─────────────────────────
	{id: "detail.yank", scope: scopeView, keys: []string{"y"}, label: "copy"},
	{id: "detail.explain", scope: scopeView, keys: []string{"e"}, label: "explain"},
	{id: "detail.close", scope: scopeView, keys: []string{"enter", "esc"}, label: "close"},

	// ── Picker (view) ───────────────────────────────────────────
	{id: "picker.reload", scope: scopeView, keys: []string{"r"}, label: "reload"},
	{id: "picker.select", scope: scopeView, keys: []string{"enter"}, label: "open"},

	// ── Failure state (view) ────────────────────────────────────
	{id: "failure.return_to_picker", scope: scopeView, keys: []string{"enter", "esc"}, label: "back"},

	// ── Connection: recent list (view) ──────────────────────────
	{id: "connection.switch_to_form", scope: scopeView, keys: []string{"2"}, label: "form"},
	{id: "connection.add", scope: scopeView, keys: []string{"a"}, label: "add"},
	{id: "connection.edit", scope: scopeView, keys: []string{"e", "enter"}, label: "edit"},
	{id: "connection.delete", scope: scopeView, keys: []string{"d"}, label: "delete"},

	// ── Connection: form (view) ─────────────────────────────────
	{id: "connection.execute", scope: scopeView, keys: []string{"f5", "ctrl+enter", "ctrl+s"}, label: "connect"},
	{id: "connection.edit_field", scope: scopeView, keys: []string{"enter"}, label: "edit"},
	{id: "connection.field_next", scope: scopeView, keys: []string{"j", "down"}, label: "↓"},
	{id: "connection.field_prev", scope: scopeView, keys: []string{"k", "up"}, label: "↑"},
	{id: "connection.switch_to_list", scope: scopeView, keys: []string{"1"}, label: "profiles"},

	// ── Connection: action buttons (view, narrower focus) ───────
	{id: "connection.action_enter", scope: scopeView, keys: []string{"enter"}, label: "action"},

	// ── Forms (edit forms: columnForm, browseForm, indexForm, FK) ─
	{id: "form.edit", scope: scopeForm, keys: []string{"enter"}, label: "edit"},
	{id: "form.save", scope: scopeForm, keys: []string{"ctrl+enter", "ctrl+s", "f5"}, label: "save"},
	{id: "form.discard", scope: scopeForm, keys: []string{"esc"}, label: "discard"},
	{id: "form.field_next", scope: scopeForm, keys: []string{"j", "down"}, label: "↓"},
	{id: "form.field_prev", scope: scopeForm, keys: []string{"k", "up"}, label: "↑"},
	{id: "browse_filter.apply", scope: scopeForm, keys: []string{"f5", "ctrl+s"}, label: "apply filters"},

	// ── Form delete (index + FK forms) ──────────────────────────
	{id: "form.delete", scope: scopeForm, keys: []string{"d"}, label: "delete"},
}

// defaultBindings is the built-in default keybinding registry, built once.
var defaultBindings = func() Keybindings {
	b, err := buildFromDefs(defaultDefs)
	if err != nil {
		panic("invalid default keybindings: " + err.Error())
	}
	return b
}()
