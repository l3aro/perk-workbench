package workbench

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

type cellEditor struct {
	input       *huh.Form
	confirm     *huh.Form
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
	confirmed   bool
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
	}

	e.input = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("value").
				Title("Edit " + e.columnName).
				Value(&e.editedVal),
		),
	).WithShowHelp(true).WithWidth(80)
	m.cellEditor = e
	return e.input.Init()
}

type cellEditorUpdatedMsg struct {
	statement string
	startedAt time.Time
	err       error
}

func (m Model) executeCellUpdate() tea.Cmd {
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
	e.confirmed = false
	e.confirming = true
	title := "Save cell change?"
	e.confirm = huh.NewForm(huh.NewGroup(
		huh.NewNote().Title(title).Description(statement).Height(8),
		huh.NewConfirm().Key("confirm").Affirmative("Yes").Negative("No").Value(&e.confirmed),
	)).WithShowHelp(false).WithWidth(40)
	return e.confirm.Init()
}

func (e *cellEditor) confirmContent() string {
	if e.confirming && e.confirm != nil {
		return trimDialogContent(e.confirm.View())
	}
	if !e.confirming && e.input != nil {
		raw := e.input.View()
		var b strings.Builder
		for i, line := range strings.Split(raw, "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			trimmed := strings.TrimRight(line, " ")
			if w := ansi.StringWidth(trimmed); w < 80 {
				trimmed += strings.Repeat(" ", 80-w)
			}
			b.WriteString(trimmed)
		}
		return b.String()
	}
	return ""
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
