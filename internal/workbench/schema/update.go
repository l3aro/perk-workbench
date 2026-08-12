package schema

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Update handles the schema sidebar pane keys: the filter input, the tree
// navigation and expansion/collapse keys, the add/rename/delete-table keys,
// and the list passthrough. The root routes focusSchema messages here and
// applies the returned events (TableSelected, ReconnectRequested, ...).
func (m Model) Update(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher, snapshot Snapshot) (Model, Event, tea.Cmd) {
	if _, ok := msg.(TreeAnimTickMsg); ok {
		model, cmd := m.UpdateTreeAnim(msg.(TreeAnimTickMsg), snapshot)
		return model, nil, cmd
	}
	if keyPress, ok := msg.(tea.KeyPressMsg); ok {
		if m.Filter.Focused() {
			switch keyPress.Code {
			case tea.KeyEscape, tea.KeyEnter:
				// Exit editing, keeping the applied filter.
				m.Filter.Blur()
				return m, nil, nil
			}
			before := m.Filter.Value()
			var filterCommand tea.Cmd
			m.Filter, filterCommand = m.Filter.Update(keyPress)
			if m.Filter.Value() != before {
				m.ApplyFilter()
			}
			return m, nil, filterCommand
		}
		switch {
		case keys.Match(keyPress, "schema.filter", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			m.Filter.Focus()
			return m, nil, nil
		case keys.Match(keyPress, "schema.context_menu", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			return m.contextMenuKey(layout, snapshot)
		case keys.Match(keyPress, "schema.add_table", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if item, ok := m.List.SelectedItem().(Item); ok {
				if target, ok := m.AddTarget(item, snapshot); ok {
					return m, TableFormRequested{Kind: TableFormTable, Database: target}, nil
				}
			}
			return m, nil, nil
		case keys.Match(keyPress, "schema.create_database", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if m.SupportsCreateDatabase(snapshot) {
				return m, TableFormRequested{Kind: TableFormDatabase}, nil
			}
			return m, nil, nil
		case keys.Match(keyPress, "schema.rename_table", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if item, ok := m.List.SelectedItem().(Item); ok && !item.Root && item.Kind == "table" {
				return m, TableFormRequested{Kind: TableFormTable, Database: item.Database, Table: item.Table}, nil
			}
			return m, nil, nil
		case keys.Match(keyPress, "schema.delete_table", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			if item, ok := m.List.SelectedItem().(Item); ok && !item.Root && item.Kind == "table" {
				return m, DeleteTableRequested{Database: item.Database, Table: item.Table}, nil
			}
			return m, nil, nil
		case keys.Match(keyPress, "schema.select_table", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			return m.SchemaSelect(snapshot)
		case keys.Match(keyPress, "schema.expand", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			next, cmd := m.SchemaExpand(snapshot)
			return next, nil, cmd
		case keys.Match(keyPress, "schema.collapse", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
			next, cmd := m.SchemaCollapse(snapshot)
			return next, nil, cmd
		}
		var command tea.Cmd
		m.List, command = m.List.Update(msg)
		// The list's own keymap can clear the filter (esc in tree
		// navigation); keep the visible input in sync.
		if !m.List.IsFiltered() && m.Filter.Value() != "" {
			m.Filter.SetValue("")
		}
		return m, nil, command
	}
	var command tea.Cmd
	m.List, command = m.List.Update(msg)
	return m, nil, command
}

// UpdateWorkspace handles the structure/index/foreign-key tab pane keys:
// the filter/reset keys, the edit/add/create/delete keys (the form and
// delete actions emit requests the root applies through its form-mode and
// overlay wrappers), the table navigation, and the table passthrough.
// offset is the root-owned horizontal pan offset of the active tab's table.
func (m Model) UpdateWorkspace(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher, tab core.Tab, snapshot Snapshot, offset *int) (Model, Event, tea.Cmd) {
	var targetTable *table.Model
	guarded := false
	switch tab {
	case core.TabStructure:
		if m.Structure.ColumnForm.Active() {
			return m, nil, nil
		}
		targetTable = &m.Structure.Table
		if keyPress, ok := msg.(tea.KeyPressMsg); ok {
			switch {
			case keys.Match(keyPress, "structure.filter", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				return m, nil, m.OpenTableFilter(tab)
			case keys.Match(keyPress, "structure.reset", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.ResetTableFilter(tab)
				return m, nil, nil
			case keys.Match(keyPress, "structure.edit", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				return m, ColumnFormRequested{}, nil
			case keys.Match(keyPress, "structure.add", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				return m, NewColumnFormRequested{}, nil
			case keys.Match(keyPress, "structure.delete", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				if column := m.SelectedColumn(); column != nil {
					return m, ColumnDeleteRequested{Name: column.Name}, nil
				}
				return m, nil, nil
			}
		}
	case core.TabIndexes:
		if m.Structure.IndexForm.Active() {
			return m, nil, nil
		}
		targetTable = &m.Structure.Indexes
		if keyPress, ok := msg.(tea.KeyPressMsg); ok {
			switch {
			case keys.Match(keyPress, "indexes.toggle_diagram", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.Structure.IndexDiagram = !m.Structure.IndexDiagram
				if m.Structure.IndexDiagram {
					m.Structure.RelationshipDiagram = false
				}
				return m, nil, nil
			case m.Structure.IndexDiagram && keys.Match(keyPress, "diagram.depth_up", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.Structure.DiagramDepth = min(m.Structure.DiagramDepth+1, MaxDiagramDepth)
				return m, nil, nil
			case m.Structure.IndexDiagram && keys.Match(keyPress, "diagram.depth_down", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.Structure.DiagramDepth = max(m.Structure.DiagramDepth-1, 1)
				return m, nil, nil
			case keys.Match(keyPress, "indexes.filter", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				return m, nil, m.OpenTableFilter(tab)
			case keys.Match(keyPress, "indexes.reset", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.ResetTableFilter(tab)
				return m, nil, nil
			case keys.Match(keyPress, "indexes.create", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				return m, IndexFormRequested{}, nil
			case keys.Match(keyPress, "indexes.edit", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				if m.SelectedIndex() != nil {
					return m, IndexFormRequested{Selected: true}, nil
				}
				return m, nil, nil
			case keys.Match(keyPress, "indexes.delete", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				if index := m.SelectedIndex(); index != nil {
					return m, IndexDeleteRequested{Name: index.Name}, nil
				}
				return m, nil, nil
			}
		}
	case core.TabForeignKeys:
		if m.Structure.ForeignKeyForm.Active() {
			return m, nil, nil
		}
		targetTable = &m.Structure.ForeignKeys
		if keyPress, ok := msg.(tea.KeyPressMsg); ok {
			switch {
			case keys.Match(keyPress, "foreign_keys.filter", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				return m, nil, m.OpenTableFilter(tab)
			case keys.Match(keyPress, "foreign_keys.reset", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.ResetTableFilter(tab)
				return m, nil, nil
			case keys.Match(keyPress, "foreign_keys.toggle_diagram", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.Structure.RelationshipDiagram = !m.Structure.RelationshipDiagram
				if m.Structure.RelationshipDiagram {
					m.Structure.IndexDiagram = false
				}
				return m, nil, nil
			case m.Structure.RelationshipDiagram && keys.Match(keyPress, "diagram.depth_up", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.Structure.DiagramDepth = min(m.Structure.DiagramDepth+1, MaxDiagramDepth)
				return m, nil, nil
			case m.Structure.RelationshipDiagram && keys.Match(keyPress, "diagram.depth_down", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				m.Structure.DiagramDepth = max(m.Structure.DiagramDepth-1, 1)
				return m, nil, nil
			case keys.Match(keyPress, "foreign_keys.create", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				return m, ForeignKeyFormRequested{}, nil
			case keys.Match(keyPress, "foreign_keys.edit", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				if m.SelectedForeignKey() != nil {
					return m, ForeignKeyFormRequested{Selected: true}, nil
				}
				return m, nil, nil
			case keys.Match(keyPress, "foreign_keys.delete", []uikit.Scope{uikit.ScopeView, uikit.ScopeGlobal}):
				if foreignKey := m.SelectedForeignKey(); foreignKey != nil {
					return m, ForeignKeyDeleteRequested{ID: foreignKey.ID}, nil
				}
				return m, nil, nil
			}
		}
	default:
		return m, nil, nil
	}
	if keyPress, ok := msg.(tea.KeyPressMsg); ok && uikit.MoveTableRow(targetTable, offset, layout.ViewportWidth, keyPress) {
		return m, nil, nil
	}
	if guarded {
		return m, nil, nil
	}
	var command tea.Cmd
	*targetTable, command = targetTable.Update(msg)
	return m, nil, command
}

// UpdateColumnForm drives the open column form: key routing through the
// form, the saving flag, and close-on-discard. The returned action tells
// the root which DDL flow to execute (add/alter/delete column).
func (m Model) UpdateColumnForm(msg tea.Msg, layout uikit.Layout, formMode *uikit.FormModeController) (Model, ColumnFormAction, tea.Cmd) {
	m.Structure.ColumnForm.Height = layout.Height
	command, action := m.Structure.ColumnForm.Update(msg, formMode)
	switch action {
	case ColumnFormSave:
		m.Structure.ColumnForm.Saving = true
	case ColumnFormDiscard:
		m.Structure.ColumnForm = ColumnForm{}
	case ColumnFormDelete:
		m.Structure.ColumnForm.Saving = true
	}
	return m, action, command
}

// UpdateIndexForm drives the open index form: key routing through the
// form, the saving flag, and close-on-discard. The returned action tells
// the root which DDL flow to execute (save or delete index).
func (m Model) UpdateIndexForm(msg tea.Msg, layout uikit.Layout, formMode *uikit.FormModeController) (Model, IndexFormAction, tea.Cmd) {
	m.Structure.IndexForm.Height = layout.Height
	command, action := m.Structure.IndexForm.Update(msg, formMode)
	switch action {
	case IndexFormSave:
		m.Structure.IndexForm.Saving = true
	case IndexFormDelete:
		m.Structure.IndexForm.Saving = true
	case IndexFormDiscard:
		m.Structure.IndexForm.Close()
	}
	return m, action, command
}

// UpdateForeignKeyForm drives the open foreign-key form: key routing
// through the form, the saving flag, and close-on-discard. The returned
// action tells the root which DDL flow to execute (save or delete foreign
// key).
func (m Model) UpdateForeignKeyForm(msg tea.Msg, layout uikit.Layout, formMode *uikit.FormModeController) (Model, ForeignKeyFormAction, tea.Cmd) {
	m.Structure.ForeignKeyForm.Height = layout.Height
	command, action := m.Structure.ForeignKeyForm.Update(msg, formMode)
	switch action {
	case ForeignKeyFormSave:
		m.Structure.ForeignKeyForm.Saving = true
	case ForeignKeyFormDelete:
		m.Structure.ForeignKeyForm.Saving = true
	case ForeignKeyFormDiscard:
		m.Structure.ForeignKeyForm.Close()
	}
	return m, action, command
}

// contextMenuKey opens the schema context menu at the sidebar's center for
// the selected item, or the blank-sidebar menu on server products.
func (m Model) contextMenuKey(layout uikit.Layout, snapshot Snapshot) (Model, Event, tea.Cmd) {
	x := layout.ViewportWidth / 2
	if item, ok := m.List.SelectedItem().(Item); ok {
		menu, ok := m.ItemMenu(item, x, m.RowY(m.List.Index(), layout)+1, snapshot)
		if !ok {
			return m, nil, nil
		}
		return m, ContextMenuRequested{Menu: menu}, nil
	}
	if m.SupportsCreateDatabase(snapshot) {
		return m, ContextMenuRequested{Menu: m.BlankMenu(x, m.RowY(0, layout)+1)}, nil
	}
	return m, nil, nil
}

// SchemaSelect runs the schema.select_table binding: toggling a schema or
// root (with the accordion), reconnecting to a non-connected PostgreSQL
// root, or selecting a table.
func (m Model) SchemaSelect(snapshot Snapshot) (Model, Event, tea.Cmd) {
	item, ok := m.List.SelectedItem().(Item)
	if !ok {
		return m, nil, nil
	}
	if item.Kind == "schema" {
		return m, nil, treeToggleCmd(m.ToggleSchema(item.Database, item.Schema, snapshot), m.RebuildTree(snapshot))
	}
	if item.Root {
		if snapshot.Database.Product == "PostgreSQL" && !m.databaseRootConnected(item.Database, snapshot) {
			return m, ReconnectRequested{Database: item.Database}, nil
		}
		return m, nil, treeToggleCmd(m.ToggleDatabase(item.Database, snapshot), m.RebuildTree(snapshot))
	}
	return m, TableSelected{Table: m.TableName(item, snapshot)}, nil
}

// SchemaExpand expands the selected node when collapsed (with the accordion
// animation) or moves the cursor to its first child when already expanded.
// Leaves are a no-op.
func (m Model) SchemaExpand(snapshot Snapshot) (Model, tea.Cmd) {
	item, ok := m.List.SelectedItem().(Item)
	if !ok {
		return m, nil
	}
	switch {
	case item.Root:
		if m.ExpandedDatabases[item.Database] {
			return m.schemaSelectFirstChild(item)
		}
		return m, treeToggleCmd(m.ToggleDatabase(item.Database, snapshot), m.RebuildTree(snapshot))
	case item.Kind == "schema":
		if m.ExpandedSchemas[m.schemaExpansionKey(item.Database, item.Schema)] {
			return m.schemaSelectFirstChild(item)
		}
		return m, treeToggleCmd(m.ToggleSchema(item.Database, item.Schema, snapshot), m.RebuildTree(snapshot))
	default:
		return m, nil // table/view leaf
	}
}

// SchemaCollapse collapses the selected node when expanded (with the
// accordion animation) or moves the cursor up to its parent row: the schema
// for a PostgreSQL table, the database root for anything else. Roots that
// are already collapsed are a no-op.
func (m Model) SchemaCollapse(snapshot Snapshot) (Model, tea.Cmd) {
	item, ok := m.List.SelectedItem().(Item)
	if !ok {
		return m, nil
	}
	switch {
	case item.Root:
		if !m.ExpandedDatabases[item.Database] {
			return m, nil
		}
		return m, treeToggleCmd(m.ToggleDatabase(item.Database, snapshot), m.RebuildTree(snapshot))
	case item.Kind == "schema":
		key := m.schemaExpansionKey(item.Database, item.Schema)
		if m.ExpandedSchemas[key] {
			return m, treeToggleCmd(m.ToggleSchema(item.Database, item.Schema, snapshot), m.RebuildTree(snapshot))
		}
		return m.schemaSelectParent(item, snapshot)
	default:
		return m.schemaSelectParent(item, snapshot)
	}
}

// schemaSelectFirstChild moves the cursor to the first visible child row of
// an expanded node: the next row when it belongs to the node's subtree.
func (m Model) schemaSelectFirstChild(item Item) (Model, tea.Cmd) {
	items := m.List.Items()
	index := m.List.Index()
	if index+1 >= len(items) {
		return m, nil
	}
	next, ok := items[index+1].(Item)
	if !ok || next.Database != item.Database {
		return m, nil
	}
	if item.Kind == "schema" && next.Schema != item.Schema {
		return m, nil
	}
	m.List.Select(index + 1)
	return m, nil
}

// schemaSelectParent moves the cursor to the parent row of the selected
// item: the schema row for a PostgreSQL table, the database root otherwise.
func (m Model) schemaSelectParent(item Item, snapshot Snapshot) (Model, tea.Cmd) {
	items := m.List.Items()
	for index := m.List.Index() - 1; index >= 0; index-- {
		parent, ok := items[index].(Item)
		if !ok || parent.Database != item.Database {
			continue
		}
		if snapshot.Database.Product == "PostgreSQL" && (item.Kind == "table" || item.Kind == "view") {
			if parent.Kind == "schema" && parent.Schema == item.Schema {
				m.List.Select(index)
				return m, nil
			}
			continue
		}
		if parent.Root {
			m.List.Select(index)
			return m, nil
		}
	}
	return m, nil
}

// HandleSchemaClick maps a schema-pane click to its item. A double-click on
// a PostgreSQL root that is not the connected database reconnects to it
// (matching the recent list's double-click-to-load); any other root or
// schema click toggles the subtree, and a table click selects it.
// recordFormClick is the root's double-click detector, consulted only for
// root items like the original click path.
func (m Model) HandleSchemaClick(x, contentY int, layout uikit.Layout, snapshot Snapshot, recordFormClick func(x, y int) bool) (Model, Event, tea.Cmd) {
	// The filter box is the first body rows; clicking it focuses the
	// input for typing.
	if m.FilterShown(layout) && contentY >= 1 && contentY <= 3 {
		m.Filter.Focus()
		return m, nil, nil
	}
	item, ok := m.ItemAt(contentY, layout)
	if !ok {
		m.Filter.Blur()
		return m, nil, nil
	}
	// Item clicks leave filter editing so navigation keys work again.
	m.Filter.Blur()
	if item.Kind == "schema" {
		return m, nil, treeToggleCmd(m.ToggleSchema(item.Database, item.Schema, snapshot), m.RebuildTree(snapshot))
	}
	if item.Root {
		if recordFormClick(x, contentY+1) && snapshot.Database.Product == "PostgreSQL" && !m.databaseRootConnected(item.Database, snapshot) {
			return m, ReconnectRequested{Database: item.Database}, nil
		}
		return m, nil, treeToggleCmd(m.ToggleDatabase(item.Database, snapshot), m.RebuildTree(snapshot))
	}
	return m, TableSelected{Table: m.TableName(item, snapshot)}, nil
}

// ClickClock is the root's shared click-tracking state for the workspace
// tables: the last press position, tab, and row. The root stores it in its
// layout state; the component reads and updates it during table clicks so
// double-click detection stays identical to the pre-refactor root logic.
type ClickClock struct {
	Time time.Time
	X, Y int
	Tab  core.Tab
	Row  int
}

// TableRowActivated asks the root to open the edit form for a
// double-clicked structure/index/foreign-key row, matching the enter/i
// keybinding behavior.
type TableRowActivated struct{}

// HandleTableClick handles a left-click on the structure, indexes, or
// foreignKeys table: row-level selection, and a double-click on the same
// tab and row emits TableRowActivated so the root opens the row's edit
// form. hit reports whether the click landed on a data row (the root resets
// the form mode and blurs the editor only then, matching the original).
func (m Model) HandleTableClick(absX, absY int, layout uikit.Layout, tab core.Tab, vimMode bool, schemaWidth int, compact bool, clock ClickClock, snapshot Snapshot) (Model, Event, tea.Cmd, ClickClock, bool) {
	var targetTable *table.Model
	switch tab {
	case core.TabStructure:
		if m.Structure.ColumnForm.Active() {
			return m, nil, nil, clock, false
		}
		targetTable = &m.Structure.Table
	case core.TabIndexes:
		if m.Structure.IndexForm.Active() {
			return m, nil, nil, clock, false
		}
		targetTable = &m.Structure.Indexes
	case core.TabForeignKeys:
		if m.Structure.ForeignKeyForm.Active() || m.Structure.RelationshipDiagram {
			return m, nil, nil, clock, false
		}
		targetTable = &m.Structure.ForeignKeys
	default:
		return m, nil, nil, clock, false
	}

	rows := targetTable.Rows()
	if len(rows) == 0 {
		return m, nil, nil, clock, false
	}

	// Workspace X: skip schema pane (left) and pane left border (1).
	workspaceX := absX - 1 // Skip pane left border.
	if !compact {
		workspaceX = max(absX-schemaWidth, 0) - 1
	}
	if workspaceX < 0 || workspaceX >= layout.ViewportWidth {
		return m, nil, nil, clock, false
	}

	contentY := absY - 1
	if contentY < 0 {
		return m, nil, nil, clock, false
	}

	// Workspace pane: contentY=0 border, contentY=1 tab row, contentY=2 blank, contentY=3+ = table view.
	tableLine := contentY - 3 // 0=header, 1..N=data rows
	if tableLine < 1 {
		return m, nil, nil, clock, false // Header or above.
	}

	rowHeight := targetTable.Height()
	start := min(max(targetTable.Cursor()-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
	dataRow := start + tableLine - 1
	if dataRow < 0 || dataRow >= len(rows) {
		return m, nil, nil, clock, false
	}

	targetTable.SetCursor(dataRow)

	// Non-vim mode: the clicked table owns focus, so leave any text editing.
	if !vimMode {
		targetTable.Focus()
	}

	// Check for double-click at the same position on the same tab and row:
	// open the row's edit form, matching the enter/i keybinding behavior.
	now := time.Now()
	if !clock.Time.IsZero() && now.Sub(clock.Time) < doubleClickTimeout &&
		clock.X == absX && clock.Y == absY &&
		clock.Tab == tab && clock.Row == dataRow {
		clock.Time = time.Time{}
		return m, TableRowActivated{}, nil, clock, true
	}

	// Single click: select the row.
	clock.Time = now
	clock.X = absX
	clock.Y = absY
	clock.Tab = tab
	clock.Row = dataRow
	return m, nil, nil, clock, true
}

const doubleClickTimeout = 500 * time.Millisecond

// OpenColumnForm opens the edit form for the selected structure column,
// returning the form's init command (or nil when no column is selected).
func (m Model) OpenColumnForm(snapshot Snapshot, layout uikit.Layout, keys uikit.KeyMatcher) (Model, tea.Cmd) {
	column := m.SelectedColumn()
	if column == nil {
		return m, nil
	}
	form := NewColumnForm(*column, sharedsql.ColumnTypes(snapshot.Database))
	form.SetKeys(keys)
	form.SetWidth(layout.ViewportWidth)
	form.SetHeight(layout.PaneHeight)
	m.Structure.ColumnForm = form
	return m, form.Form.Init()
}

// OpenNewColumnForm opens the add-column form, returning its init command.
func (m Model) OpenNewColumnForm(snapshot Snapshot, layout uikit.Layout, keys uikit.KeyMatcher) (Model, tea.Cmd) {
	form := NewEmptyColumnForm(sharedsql.ColumnTypes(snapshot.Database))
	form.SetKeys(keys)
	form.SetWidth(layout.ViewportWidth)
	form.SetHeight(layout.PaneHeight)
	m.Structure.ColumnForm = form
	return m, form.Form.Init()
}

// OpenIndexForm opens the create/edit index form, returning its init
// command.
func (m Model) OpenIndexForm(index *sharedsql.IndexInfo, layout uikit.Layout, keys uikit.KeyMatcher) (Model, tea.Cmd) {
	form := NewIndexForm(index)
	form.SetKeys(keys)
	form.SetWidth(layout.ViewportWidth)
	m.Structure.IndexForm = form
	return m, form.Form.Init()
}

// OpenForeignKeyForm opens the create/edit foreign-key form, returning its
// init command.
func (m Model) OpenForeignKeyForm(foreignKey *sharedsql.ForeignKeyInfo, layout uikit.Layout, keys uikit.KeyMatcher) (Model, tea.Cmd) {
	form := NewForeignKeyForm(foreignKey)
	form.SetKeys(keys)
	form.SetWidth(layout.ViewportWidth)
	m.Structure.ForeignKeyForm = form
	return m, form.Form.Init()
}

// OpenTableForm opens the create/rename table popup. The popup's init
// command is dropped, like the original openPopup; the root starts insert
// mode through the form-mode controller.
func (m Model) OpenTableForm(database, table string, layout uikit.Layout, keys uikit.KeyMatcher) (Model, tea.Cmd) {
	form := NewTableForm(database, table)
	form.SetKeys(keys)
	form.SetWidth(layout.ViewportWidth)
	form.SetHeight(layout.PaneHeight)
	form.Form.Init()
	m.Structure.TableForm = form
	return m, nil
}

// OpenDatabaseForm opens the create/rename database popup.
func (m Model) OpenDatabaseForm(originalName string, layout uikit.Layout, keys uikit.KeyMatcher) (Model, tea.Cmd) {
	form := NewDatabaseForm(originalName)
	form.SetKeys(keys)
	form.SetWidth(layout.ViewportWidth)
	form.SetHeight(layout.PaneHeight)
	form.Form.Init()
	m.Structure.TableForm = form
	return m, nil
}

// OpenSchemaForm opens the create/rename schema popup.
func (m Model) OpenSchemaForm(originalName string, layout uikit.Layout, keys uikit.KeyMatcher) (Model, tea.Cmd) {
	form := NewSchemaForm(originalName)
	form.SetKeys(keys)
	form.SetWidth(layout.ViewportWidth)
	form.SetHeight(layout.PaneHeight)
	form.Form.Init()
	m.Structure.TableForm = form
	return m, nil
}

// OpenTableFilter starts editing the active tab's table filter.
func (m *Model) OpenTableFilter(tab core.Tab) tea.Cmd {
	m.Structure.TableFiltering = true
	m.Structure.TableFilterTab = tab
	m.Structure.TableFilterInput = textinput.New()
	m.Structure.TableFilterInput.Prompt = "Filter: "
	m.Structure.TableFilterInput.SetValue(m.TableFilterValue(tab))
	return m.Structure.TableFilterInput.Focus()
}

// CloseTableFilter ends table-filter editing.
func (m *Model) CloseTableFilter() {
	m.Structure.TableFiltering = false
	m.Structure.TableFilterInput.Blur()
}

// UpdateTableFilter routes one key through the active table filter input
// and re-applies the filter.
func (m *Model) UpdateTableFilter(message tea.KeyPressMsg) tea.Cmd {
	if code := message.Key().Code; code == tea.KeyEscape || code == tea.KeyEnter {
		m.CloseTableFilter()
		return nil
	}
	var command tea.Cmd
	m.Structure.TableFilterInput, command = m.Structure.TableFilterInput.Update(message)
	m.SetTableFilterValue(m.Structure.TableFilterTab, m.Structure.TableFilterInput.Value())
	m.ApplyTableFilter(m.Structure.TableFilterTab)
	return command
}

// ResetTableFilter clears the active tab's table filter.
func (m *Model) ResetTableFilter(tab core.Tab) {
	m.SetTableFilterValue(tab, "")
	m.ApplyTableFilter(tab)
}

// TableFilterValue returns the active filter text for the tab.
func (m Model) TableFilterValue(tab core.Tab) string {
	switch tab {
	case core.TabStructure:
		return m.Structure.Filter
	case core.TabIndexes:
		return m.Structure.IndexesFilter
	case core.TabForeignKeys:
		return m.Structure.ForeignKeysFilter
	default:
		return ""
	}
}

// SetTableFilterValue records the active filter text for the tab.
func (m *Model) SetTableFilterValue(tab core.Tab, value string) {
	switch tab {
	case core.TabStructure:
		m.Structure.Filter = value
	case core.TabIndexes:
		m.Structure.IndexesFilter = value
	case core.TabForeignKeys:
		m.Structure.ForeignKeysFilter = value
	}
}

// ApplyTableFilter re-filters the tab's table from its source rows.
func (m *Model) ApplyTableFilter(tab core.Tab) {
	var result *table.Model
	var source []table.Row
	switch tab {
	case core.TabStructure:
		result, source = &m.Structure.Table, m.Structure.Rows
	case core.TabIndexes:
		result, source = &m.Structure.Indexes, m.Structure.IndexRows
	case core.TabForeignKeys:
		result, source = &m.Structure.ForeignKeys, m.Structure.ForeignKeyRows
	default:
		return
	}
	query := strings.ToLower(strings.TrimSpace(m.TableFilterValue(tab)))
	if query == "" {
		result.SetRows(source)
		return
	}
	rows := make([]table.Row, 0, len(source))
	for _, row := range source {
		if strings.Contains(strings.ToLower(strings.Join(row, " ")), query) {
			rows = append(rows, row)
		}
	}
	result.SetRows(rows)
}

// TableFilterStatus renders the tab's table filter status line.
func (m Model) TableFilterStatus(tab core.Tab) string {
	if m.Structure.TableFiltering && m.Structure.TableFilterTab == tab {
		return m.Structure.TableFilterInput.View() + " | enter/esc done"
	}
	if query := m.TableFilterValue(tab); query != "" {
		return "/ filter | r reset | " + query
	}
	return "/ filter | r reset"
}

// SelectedColumn returns the structure column at the table cursor.
func (m Model) SelectedColumn() *sharedsql.ColumnInfo {
	row := m.Structure.Table.Cursor()
	rows := m.Structure.Table.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.Structure.Columns {
		if m.Structure.Columns[index].Name == rows[row][0] {
			return &m.Structure.Columns[index]
		}
	}
	return nil
}

// SelectedIndex returns the index at the indexes table cursor.
func (m Model) SelectedIndex() *sharedsql.IndexInfo {
	row := m.Structure.Indexes.Cursor()
	rows := m.Structure.Indexes.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.Structure.IndexInfo {
		if m.Structure.IndexInfo[index].Name == rows[row][0] {
			return &m.Structure.IndexInfo[index]
		}
	}
	return nil
}

// SelectedForeignKey returns the foreign key at the foreign-keys table
// cursor.
func (m Model) SelectedForeignKey() *sharedsql.ForeignKeyInfo {
	row := m.Structure.ForeignKeys.Cursor()
	rows := m.Structure.ForeignKeys.Rows()
	if row < 0 || row >= len(rows) || len(rows[row]) == 0 {
		return nil
	}
	for index := range m.Structure.ForeignKeyInfo {
		if m.Structure.ForeignKeyInfo[index].ID == rows[row][0] {
			return &m.Structure.ForeignKeyInfo[index]
		}
	}
	return nil
}
