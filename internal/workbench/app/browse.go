package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/browse"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// browseLayout builds the layout snapshot root hands to the browse
// component for one update or render.
func browseLayout(m Model) uikit.Layout {
	return uikit.Layout{
		Width:         m.layout.width,
		Height:        m.layout.height,
		ViewportWidth: m.layout.tableViewportWidth,
		PaneHeight:    m.formViewportHeight(),
	}
}

// refreshBrowseBackend builds the narrow database adapter the browse
// component receives, from the existing capability helpers. Root performs
// the type assertions once per connection and caches the result; the
// browse flows consult the cached adapter.
func (m *Model) refreshBrowseBackend() {
	m.browse.backend = browse.Backend{
		Service:       m.Database,
		Capabilities:  m.writeCapabilities(),
		RowWriter:     m.rowWriter(),
		DocumentRead:  m.documentReader(),
		DocumentWrite: m.documentWriter(),
	}
}

// browseBackend returns the cached database adapter for the open service.
func (m Model) browseBackend() browse.Backend {
	return m.browse.backend
}

// resizeScopeObjectsTable fits the object-list table to the workspace
// body: the scope view reserves its status line plus the workspace
// chrome below the data rows, mirroring the browse table sizing. Table
// targets keep the row-browse sizing (browseFooterRows).
func (m *Model) resizeScopeObjectsTable() {
	if !m.browse.component.ObjectListMode() {
		return
	}
	uikit.ResizeResultsTable(&m.browse.component.Table, m.layout.tableViewportWidth, max(m.layout.workspaceHeight-scopeObjectsFooterRows(), 2))
}

// applyBrowseEvent applies one browse component event: status transitions,
// clipboard copies, browse reloads, page ticks, and schema refreshes stay
// root-owned.
func (m Model) applyBrowseEvent(event browse.Event, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	switch e := event.(type) {
	case nil:
		return m, cmd
	case uikit.StatusChanged:
		m.setStatus(uikit.SafeText(e.Text))
		return m, cmd
	case uikit.ClipboardRequested:
		m.setStatus("copied to clipboard")
		if cmd == nil {
			return m, copyQueryLogStatement(e.Text)
		}
		return m, tea.Batch(cmd, copyQueryLogStatement(e.Text))
	case browse.DataChanged:
		m.BrowsePage, m.browse.component.Loading = 0, true
		m.browse.component.PageTag++
		m.browse.component.Page = 0
		if cmd == nil {
			return m, m.loadBrowse()
		}
		return m, tea.Batch(cmd, m.loadBrowse())
	case browse.PageRequested:
		if cmd == nil {
			return m, m.browsePageTick(e.Delta)
		}
		return m, tea.Batch(cmd, m.browsePageTick(e.Delta))
	case browse.SchemaRequested:
		if cmd == nil {
			return m, m.loadSchema()
		}
		return m, tea.Batch(cmd, m.loadSchema())
	case browse.ObjectOpenRequested:
		return m, m.selectScopeObject(e.Object)
	case browse.ObjectContextMenuRequested:
		// Place the menu on the cursor row at the workspace pane's left
		// edge, mirroring the browse row menu's geometry.
		rows := m.browse.component.Table.Rows()
		row := m.browse.component.Table.Cursor()
		rowHeight := m.browse.component.Table.Height()
		start := min(max(row-rowHeight+1, 0), max(len(rows)-rowHeight, 0))
		m.openObjectContextMenu(m.layout.schemaWidth+1, row-start+6)
		return m, cmd
	}
	return m, cmd
}

// browsePageTick advances the browse page by delta through the same
// debounce tick and staleness tag as the keypress dispatch, so a
// pager-button click and a keypress are interchangeable.
func (m Model) browsePageTick(delta int) tea.Cmd {
	tag, table := m.browse.component.PageTag, m.SelectedTable
	return tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
		return browseDebounceMsg{tag: tag, delta: delta, table: table}
	})
}

// cycleBrowseSort toggles the sort on the selected column (component
// state) and reloads the browse page from the top.
func (m *Model) cycleBrowseSort() tea.Cmd {
	if !m.browse.component.CycleSort() {
		return nil
	}
	m.BrowsePage, m.browse.component.Loading = 0, true
	m.browse.component.PageTag++
	m.browse.component.Page = 0
	return m.loadBrowse()
}

// resetBrowseFilters clears the active filters and reloads the browse
// page from the top.
func (m *Model) resetBrowseFilters() tea.Cmd {
	m.browse.component.ResetFilters()
	m.BrowsePage, m.browse.component.Loading = 0, true
	m.browse.component.PageTag++
	m.browse.component.Page = 0
	return m.loadBrowse()
}

// pagerBrowseCommand advances the browse page by delta, mirroring the
// n/p keypress dispatch exactly: the same debounce tick and staleness
// tag, so a pager-button click and a keypress are interchangeable. While
// a page is already loading the click is a no-op, like the keybindings.
func (m Model) pagerBrowseCommand(delta int) (Model, tea.Cmd) {
	if m.browse.component.Loading {
		return m, nil
	}
	m.browse.component.PageTag++
	return m, m.browsePageTick(delta)
}

// openBrowseFilterForm opens the filter grid over the loaded structure
// columns.
func (m *Model) openBrowseFilterForm() tea.Cmd {
	if len(m.schema.component.Structure.Columns) == 0 {
		m.setStatus("table columns are loading")
		return nil
	}
	m.browse.component.FilterForm = browse.NewFilterForm(m.schema.component.Structure.Columns, m.browse.component.Settings, m.browse.component.PageSize, m.layout.tableViewportWidth, m.formViewportHeight())
	m.overlay.formMode.Mode = formModeNormal
	m.overlay.formMode.ButtonsFocused = false
	if m.vimMode {
		return nil
	}
	command, _ := m.browse.component.FilterForm.BeginEdit()
	return command
}

// openBrowseForm opens the row edit form for the selected row. Document
// stores edit the whole document instead of a row.
func (m *Model) openBrowseForm() tea.Cmd {
	if !m.writeCapabilities().RowWriter {
		// Document stores edit the whole document instead of a row.
		return m.openEditDocument()
	}
	row := m.browse.component.Table.Cursor()
	if row < 0 || row >= len(m.browse.component.Result.Rows) {
		m.setStatus("select a row")
		return nil
	}
	form, err := browse.NewForm(m.browse.component.Result.Columns, m.browse.component.Result.Rows[row], m.schema.component.Structure.Columns)
	if err != nil {
		m.setStatus(safeText(err.Error()))
		return nil
	}
	m.browse.component.Form = form
	m.overlay.formMode.ButtonsFocused = false
	m.browse.component.Form.Keybindings = m.keybindings
	m.browse.component.Form.Table = m.SelectedTable
	m.browse.component.Form.SetWidth(m.layout.tableViewportWidth)
	return m.openForm(m.browse.component.Form.Form.Init(), m.browse.component.Form.Focus)
}

// openInsertRowForm opens the insert-row form for the selected table. The
// form is built from the structure columns because an empty table has no
// browse rows to derive columns from. Document stores with an editable
// document capability get the document insert editor instead.
func (m *Model) openInsertRowForm() tea.Cmd {
	if !m.writeCapabilities().RowWriter {
		if m.documentCapability() != nil {
			return m.openInsertDocument()
		}
		m.setStatus(safeText(m.rowWriteUnsupportedError().Error()))
		return nil
	}
	columns := make([]string, 0, len(m.schema.component.Structure.Columns))
	for _, info := range m.schema.component.Structure.Columns {
		columns = append(columns, info.Name)
	}
	form, err := browse.NewInsertForm(columns)
	if err != nil {
		m.setStatus(safeText(err.Error()))
		return nil
	}
	m.browse.component.Form = form
	m.overlay.formMode.ButtonsFocused = false
	m.browse.component.Form.Keybindings = m.keybindings
	m.browse.component.Form.Table = m.SelectedTable
	m.browse.component.Form.SetWidth(m.layout.tableViewportWidth)
	return m.openForm(m.browse.component.Form.Form.Init(), m.browse.component.Form.Focus)
}

// openCellEditor opens the inline cell editor for the selected cell.
func (m *Model) openCellEditor() tea.Cmd {
	if !m.writeCapabilities().RowWriter {
		// Document stores edit the whole document instead of one cell.
		return m.openEditDocument()
	}
	width := max(m.layout.tableViewportWidth, 40)
	m.browse.component.Structure = m.schema.component.Structure.Columns
	editor, command, err := m.browse.component.BuildCellEditor(m.SelectedTable, width)
	if err != nil {
		m.setStatus(safeText(err.Error()))
		return nil
	}
	if editor == nil {
		return nil
	}
	m.browse.component.CellEditor = editor
	return command
}

// openInsertDocument opens the document insert editor. The known Mongo
// format starts at {}; any other non-empty textual format opens a raw-text
// editor.
func (m *Model) openInsertDocument() tea.Cmd {
	capability := m.documentCapability()
	if capability == nil {
		m.setStatus(safeText(m.rowWriteUnsupportedError().Error()))
		return nil
	}
	if !capability.Text || capability.Format == "" {
		m.setStatus(fmt.Sprintf("document editing is unsupported for format %s", capability.Format))
		return nil
	}
	initial := ""
	if capability.Format == sharedsql.DocumentFormatMongoExtendedJSON {
		initial = "{}"
	}
	m.browse.component.DocumentEditor = browse.NewDocumentEditor(m.SelectedTable, true, *capability, nil, initial, max(m.layout.tableViewportWidth, 40))
	return m.openForm(m.browse.component.DocumentEditor.Form.Init(), m.browse.component.DocumentEditor.Focus)
}

// openEditDocument opens the document editor for the selected row: the
// full document is loaded asynchronously via DocumentReader, then the same
// text form opens titled "Edit document".
func (m *Model) openEditDocument() tea.Cmd {
	capability := m.documentCapability()
	if capability == nil {
		m.setStatus(safeText(m.rowWriteUnsupportedError().Error()))
		return nil
	}
	if !capability.Text || capability.Format == "" {
		m.setStatus(fmt.Sprintf("document editing is unsupported for format %s", capability.Format))
		return nil
	}
	identity := m.browseDocumentIdentity()
	if identity == nil {
		m.setStatus("document identity unavailable")
		return nil
	}
	reader := m.documentReader()
	if reader == nil {
		m.setStatus(fmt.Sprintf("document editing is unsupported for format %s", capability.Format))
		return nil
	}
	m.browse.component.DocumentEditor = &browse.DocumentEditor{
		Collection: m.SelectedTable,
		Capability: *capability,
		Identity:   identity,
		Width:      max(m.layout.tableViewportWidth, 40),
		Loading:    true,
	}
	collection := m.SelectedTable
	id := *identity
	return func() tea.Msg {
		payload, err := reader.ReadDocument(m.appContext, collection, id)
		return documentEditorLoadedMsg{payload: payload, err: err}
	}
}

type cellEditorUpdatedMsg struct {
	statement string
	startedAt time.Time
	err       error
}

// executeCellUpdate runs the confirmed cell edit through the row writer.
func (m Model) executeCellUpdate() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return cellEditorUpdatedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	writer := m.rowWriter()
	if writer == nil {
		return func() tea.Msg { return cellEditorUpdatedMsg{err: m.rowWriteUnsupportedError()} }
	}
	key, err := m.browse.component.CellEditor.KeyValues()
	if err != nil {
		return func() tea.Msg { return cellEditorUpdatedMsg{err: err} }
	}
	values := []sharedsql.RowValue{{Name: m.browse.component.CellEditor.ColumnName, Value: sharedsql.Value{Kind: sharedsql.ValueString, String: m.browse.component.CellEditor.EditedVal}}}
	preview := m.browse.component.CellEditor.Preview()
	table, startedAt := m.SelectedTable, time.Now()
	return func() tea.Msg {
		result, err := writer.UpdateRow(m.appContext, table, key, values)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("updated %d rows, want 1", result.RowsAffected)
		}
		return cellEditorUpdatedMsg{statement: writeLogStatement(preview, result), startedAt: startedAt, err: err}
	}
}

func (m Model) updateCellEditorUpdated(msg cellEditorUpdatedMsg) (tea.Model, tea.Cmd) {
	if msg.statement != "" {
		m.appendQueryLog(actionLogEntry(msg.statement, msg.startedAt, msg.err, "updated 1 row"))
	}
	if msg.err != nil {
		m.setStatus(safeText(fmt.Sprintf("updating cell: %v", msg.err)))
		return m, nil
	}
	m.browse.component.CloseCellEditor()
	m.setStatus("cell updated")
	return m, m.loadBrowse()
}

type documentEditorLoadedMsg struct {
	payload sharedsql.DocumentPayload
	err     error
}

type documentEditorSavedMsg struct {
	statement string
	inserting bool
	startedAt time.Time
	err       error
}

// executeDocumentSave runs the confirmed insert or whole-document replace.
func (m Model) executeDocumentSave() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return documentEditorSavedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	e := m.browse.component.DocumentEditor
	payload := sharedsql.DocumentPayload{Format: e.Capability.Format, Data: []byte(e.Edited)}
	preview := e.Preview()
	collection, startedAt := e.Collection, time.Now()
	writer := m.documentWriter()
	if writer == nil {
		return func() tea.Msg { return documentEditorSavedMsg{err: m.rowWriteUnsupportedError()} }
	}
	if e.Inserting {
		return func() tea.Msg {
			result, err := writer.InsertDocument(m.appContext, collection, payload)
			if err == nil && result.RowsAffected != 1 {
				err = fmt.Errorf("inserted %d rows, want 1", result.RowsAffected)
			}
			return documentEditorSavedMsg{statement: writeLogStatement(preview, result), inserting: true, startedAt: startedAt, err: err}
		}
	}
	identity := *e.Identity
	return func() tea.Msg {
		result, err := writer.ReplaceDocument(m.appContext, collection, identity, payload)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("updated %d rows, want 1", result.RowsAffected)
		}
		return documentEditorSavedMsg{statement: writeLogStatement(preview, result), startedAt: startedAt, err: err}
	}
}

// updateDocumentEditorLoaded completes an edit-open: the full document
// arrives and the editor opens with it. Invalid UTF-8 disables editing.
func (m Model) updateDocumentEditorLoaded(message documentEditorLoadedMsg) (tea.Model, tea.Cmd) {
	component, outcome, cmd := m.browse.component.ApplyDocumentLoaded(message.payload, message.err)
	m.browse.component = component
	switch {
	case outcome.Err != nil:
		m.setStatus(safeText(fmt.Sprintf("loading document: %v", outcome.Err)))
		return m, nil
	case outcome.Format != "":
		m.setStatus(fmt.Sprintf("document editing is unsupported for format %s", outcome.Format))
		return m, nil
	}
	if !outcome.Opened || cmd == nil {
		return m, nil
	}
	return m, m.openForm(cmd, m.browse.component.DocumentEditor.Focus)
}

// updateDocumentEditorSaved completes a save. Success closes the editor
// and reloads browse; failure keeps the editor open so the rejected text
// survives, and restores the form from its confirming state.
func (m Model) updateDocumentEditorSaved(message documentEditorSavedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		text := "updated 1 row"
		if message.inserting {
			text = "inserted 1 row"
		}
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, text))
	}
	if message.err != nil {
		m.browse.component.DocumentSaveFailed()
		m.setStatus(safeText(fmt.Sprintf("saving document: %v", message.err)))
		return m, nil
	}
	m.browse.component.CloseDocumentEditor()
	m.setStatus("document saved")
	return m, m.loadBrowse()
}
