package workbench

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
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

// keyValues converts the whole-row primary-key values to the RowValue key
// list used by the driver's UpdateRow; NULL keys stay NULL.
func (e *cellEditor) keyValues() ([]sharedsql.RowValue, error) {
	if len(e.primaryKeys) == 0 {
		return nil, fmt.Errorf("cannot edit: no primary key")
	}
	key := make([]sharedsql.RowValue, 0, len(e.primaryKeys))
	for _, pk := range e.primaryKeys {
		value := sharedsql.Value{Kind: sharedsql.ValueString}
		if e.original[pk] == nil {
			value.Kind = sharedsql.ValueNull
		} else {
			value.String = *e.original[pk]
		}
		key = append(key, sharedsql.RowValue{Name: e.columns[pk], Value: value})
	}
	return key, nil
}

// preview renders the structured cell-write preview for the confirmation
// and query-log entry: Table, Key, and the single Change.
func (e *cellEditor) preview() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Table: %s", e.table)
	builder.WriteString("\nKey:")
	if key, err := e.keyValues(); err == nil {
		for _, row := range key {
			fmt.Fprintf(&builder, "\n  %s = %s", row.Name, rowValuePreview(row.Value))
		}
	}
	fmt.Fprintf(&builder, "\nChanges:\n  %s = %s", e.columnName, strconv.Quote(e.editedVal))
	return builder.String()
}

func (m *Model) openCellEditor() tea.Cmd {
	if !m.writeCapabilities().RowWriter {
		// Document stores edit the whole document instead of one cell.
		return m.openEditDocument()
	}
	row := m.browse.table.Cursor()
	if row < 0 || row >= len(m.browse.result.Rows) {
		m.setStatus("select a row")
		return nil
	}
	col := m.layout.browseColumn
	columns := m.browse.result.Columns
	if col < 0 || col >= len(columns) {
		return nil
	}
	browseRow := m.browse.result.Rows[row]

	// Find primary key indices from structure info
	var primaryKeys []int
	for _, info := range m.structure.columns {
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
		m.setStatus("cannot edit: no primary key")
		return nil
	}

	originalVal := browseRow[col]
	editedVal := ""
	if originalVal != nil {
		editedVal = *originalVal
	}

	// Detect column type for choosing input vs text field
	colType := ""
	for _, info := range m.structure.columns {
		if strings.EqualFold(info.Name, columns[col]) {
			colType = info.Type
			break
		}
	}

	width := max(m.layout.tableViewportWidth, 40)

	e := &cellEditor{
		table:       m.SelectedTable,
		columnName:  columns[col],
		columnIndex: col,
		primaryKeys: primaryKeys,
		columns:     columns,
		original:    browseRow,
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
	m.browse.cellEditor = e
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
	writer := m.rowWriter()
	if writer == nil {
		return func() tea.Msg { return cellEditorUpdatedMsg{err: m.rowWriteUnsupportedError()} }
	}
	key, err := m.browse.cellEditor.keyValues()
	if err != nil {
		return func() tea.Msg { return cellEditorUpdatedMsg{err: err} }
	}
	values := []sharedsql.RowValue{{Name: m.browse.cellEditor.columnName, Value: sharedsql.Value{Kind: sharedsql.ValueString, String: m.browse.cellEditor.editedVal}}}
	preview := m.browse.cellEditor.preview()
	table, startedAt := m.SelectedTable, time.Now()
	return func() tea.Msg {
		result, err := writer.UpdateRow(m.appContext, table, key, values)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("updated %d rows, want 1", result.RowsAffected)
		}
		return cellEditorUpdatedMsg{statement: preview, startedAt: startedAt, err: err}
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
	m.browse.cellEditor = nil
	m.setStatus("cell updated")
	return m, m.loadBrowse()
}

func (e *cellEditor) beginConfirmation() tea.Cmd {
	e.confirming = true
	e.confirm = yesNoConfirmation("Save cell change?", e.preview(), "save")
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
	e := m.browse.cellEditor
	if e == nil || e.confirming {
		return ""
	}
	contentLines := len(strings.Split(e.input.View(), "\n")) + 1 // + buttons row
	dialogW := min(e.width, max(m.layout.width-6, 1))
	dialogH := min(contentLines, max(m.layout.height-6, 1))
	boxX := max(0, (m.layout.width-dialogW-2)/2)
	boxY := max(0, (m.layout.height-dialogH-2)/2)
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
