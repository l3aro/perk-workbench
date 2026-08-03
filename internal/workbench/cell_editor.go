package workbench

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

type cellEditor struct {
	input       *huh.Form
	confirm     *confirmationDialog
	confirming  bool
	table       string
	columnName  string
	columnIndex int
	primaryKeys []int
	columns     []string
	original    []*string // whole row values
	identifier  func(string) string
	originalVal *string
	editedVal   string
	width       int
}

// isLongTextType returns true when the SQL type is a long-form text type that
// should use the huh.Text multi-line / external-editor field instead of the
// single-line huh.Input.
func isLongTextType(typeName string) bool {
	upper := strings.ToUpper(typeName)
	for _, prefix := range []string{"TEXT", "MEDIUMTEXT", "LONGTEXT", "TINYTEXT", "CLOB", "JSON"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func (e *cellEditor) active() bool {
	if e.confirming {
		return e.confirm != nil
	}
	return e.input != nil
}

func (e *cellEditor) updateStatement() string {
	if !e.active() || len(e.primaryKeys) == 0 {
		return ""
	}
	id := e.identifier
	if id == nil {
		id = quoteBrowseIdentifier
	}
	set := id(e.columnName) + " = " + quoteBrowseValue(e.editedVal)
	where := make([]string, len(e.primaryKeys))
	for i, pk := range e.primaryKeys {
		if e.original[pk] == nil {
			where[i] = id(e.columns[pk]) + " IS NULL"
		} else {
			where[i] = id(e.columns[pk]) + " = " + quoteBrowseValue(*e.original[pk])
		}
	}
	return "UPDATE " + id(e.table) + " SET " + set + " WHERE " + strings.Join(where, " AND ")
}

func (m *Model) openCellEditor() tea.Cmd {
	row := m.browse.Cursor()
	if row < 0 || row >= len(m.browseResult.Rows) {
		m.Status = "select a row"
		return nil
	}
	col := m.browseColumn
	columns := m.browseResult.Columns
	if col < 0 || col >= len(columns) {
		return nil
	}
	browseRow := m.browseResult.Rows[row]

	// Find primary key indices from structure info
	var primaryKeys []int
	for _, info := range m.structureColumns {
		if info.PrimaryKey > 0 {
			for ci, name := range columns {
				if strings.EqualFold(name, info.Name) {
					primaryKeys = append(primaryKeys, ci)
					break
				}
			}
		}
	}
	if len(primaryKeys) == 0 {
		m.Status = "cannot edit: no primary key"
		return nil
	}

	originalVal := browseRow[col]
	editedVal := ""
	if originalVal != nil {
		editedVal = *originalVal
	}

	id := m.actionIdentifier
	if id == nil {
		id = quoteBrowseIdentifier
	}

	// Detect column type for choosing input vs text field
	colType := ""
	for _, info := range m.structureColumns {
		if strings.EqualFold(info.Name, columns[col]) {
			colType = info.Type
			break
		}
	}

	width := max(m.tableViewportWidth, 40)

	e := &cellEditor{
		table:       m.SelectedTable,
		columnName:  columns[col],
		columnIndex: col,
		primaryKeys: primaryKeys,
		columns:     columns,
		original:    browseRow,
		identifier:  id,
		originalVal: originalVal,
		editedVal:   editedVal,
		width:       width,
	}

	var field huh.Field
	if isLongTextType(colType) {
		field = huh.NewText().
			Key("value").
			Title("Edit " + e.columnName).
			Value(&e.editedVal)
	} else {
		field = huh.NewInput().
			Key("value").
			Title("Edit " + e.columnName).
			Value(&e.editedVal)
	}

	km := huh.NewDefaultKeyMap()
	km.Input.Submit = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	km.Input.Next = key.NewBinding(key.WithDisabled())
	km.Input.AcceptSuggestion = key.NewBinding(key.WithDisabled())
	km.Text.Submit = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	km.Text.Next = key.NewBinding(key.WithDisabled())
	e.input = newForm(
		huh.NewGroup(field),
	).WithShowHelp(true).WithWidth(width).WithKeyMap(km)
	m.cellEditor = e
	return e.input.Init()
}

type cellEditorUpdatedMsg struct {
	statement string
	startedAt time.Time
	err       error
}

func (m Model) executeCellUpdate() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return cellEditorUpdatedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	statement := m.cellEditor.updateStatement()
	if statement == "" {
		return func() tea.Msg { return cellEditorUpdatedMsg{} }
	}
	service, startedAt := m.Database, time.Now()
	return func() tea.Msg {
		result, err := service.Execute(m.appContext, statement)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("updated %d rows, want 1", result.RowsAffected)
		}
		return cellEditorUpdatedMsg{statement: statement, startedAt: startedAt, err: err}
	}
}

func (m Model) updateCellEditorUpdated(msg cellEditorUpdatedMsg) (tea.Model, tea.Cmd) {
	if msg.statement != "" {
		m.appendQueryLog(actionLogEntry(msg.statement, msg.startedAt, msg.err, "updated 1 row"))
	}
	if msg.err != nil {
		m.Status = safeText(fmt.Sprintf("updating cell: %v", msg.err))
		return m, nil
	}
	m.cellEditor = nil
	m.Status = "cell updated"
	return m, m.loadBrowse()
}

func (e *cellEditor) beginConfirmation() tea.Cmd {
	statement := e.updateStatement()
	e.confirming = true
	e.confirm = yesNoConfirmation("Save cell change?", statement, "save")
	return nil
}

func (e *cellEditor) confirmContent() string {
	if e.confirming && e.confirm != nil {
		return e.confirm.content(e.width)
	}
	if !e.confirming && e.input != nil {
		raw := e.input.View()
		var b strings.Builder
		for i, line := range strings.Split(raw, "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			trimmed := strings.TrimRight(line, " ")
			if w := ansi.StringWidth(trimmed); w < e.width {
				trimmed += strings.Repeat(" ", e.width-w)
			}
			b.WriteString(trimmed)
		}
		buttons := formButtonsBar(false, 0)
		if w := ansi.StringWidth(buttons); w < e.width {
			buttons += strings.Repeat(" ", e.width-w)
		}
		b.WriteByte('\n')
		b.WriteString(buttons)
		return b.String()
	}
	return ""
}

// cellEditorButtonAt maps a click on the cell-editor dialog to its bottom
// button ("save"/"cancel"), replicating drawConfirmDialog's centered layout.
// The buttons row is the last content line.
func (m Model) cellEditorButtonAt(x, y int) string {
	e := m.cellEditor
	if e == nil || e.confirming {
		return ""
	}
	contentLines := len(strings.Split(e.input.View(), "\n")) + 1 // + buttons row
	dialogW := min(e.width, max(m.width-6, 1))
	dialogH := min(contentLines, max(m.height-6, 1))
	boxX := max(0, (m.width-dialogW-2)/2)
	boxY := max(0, (m.height-dialogH-2)/2)
	if y != boxY+dialogH {
		return ""
	}
	return formButtonAt(x - boxX - 1)
}

func trimDialogContent(raw string) string {
	var b strings.Builder
	for i, line := range strings.Split(raw, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, " "))
	}
	return b.String()
}
