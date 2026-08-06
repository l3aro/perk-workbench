package workbench

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type browseFilterCell uint8

const (
	browseFilterOperatorCell browseFilterCell = iota
	browseFilterValueCell
	browseFilterRowsCell
)

type browseFilterField struct {
	column   sharedsql.ColumnInfo
	operator sharedsql.BrowseFilterOperator
	value    string
}

type browseFilterAction uint8

const (
	browseFilterNoAction browseFilterAction = iota
	browseFilterApply
	browseFilterDiscard
)

type browseFilterForm struct {
	fields           []browseFilterField
	settings         browseSettings
	defaultPageSize  int
	limit            string
	row              int
	cell             browseFilterCell
	operatorIndex    int
	input            textinput.Model
	editing          bool
	width, height    int
	scrollOffset     int
	horizontalOffset int
}

func newBrowseFilterForm(columns []sharedsql.ColumnInfo, settings browseSettings, defaultPageSize, width, height int) *browseFilterForm {
	saved := make(map[string]sharedsql.BrowseFilter, len(settings.filters))
	for _, filter := range settings.filters {
		saved[filter.Column] = filter
	}
	form := &browseFilterForm{
		fields:          make([]browseFilterField, len(columns)),
		settings:        settings,
		defaultPageSize: defaultPageSize,
		limit:           strconv.Itoa(settings.pageSize(defaultPageSize)),
		width:           max(width, 1),
		height:          max(height, 1),
	}
	for index, column := range columns {
		form.fields[index].column = column
		if filter, ok := saved[column.Name]; ok {
			form.fields[index].operator = filter.Operator
			form.fields[index].value = filter.Value
		}
	}
	form.input = textinput.New()
	form.input.Prompt = ""
	form.resizeInput()
	return form
}
func (f *browseFilterForm) reset() {
	saved := make(map[string]sharedsql.BrowseFilter, len(f.settings.filters))
	for _, filter := range f.settings.filters {
		saved[filter.Column] = filter
	}
	for index := range f.fields {
		f.fields[index].operator, f.fields[index].value = sharedsql.BrowseFilterNone, ""
		if filter, ok := saved[f.fields[index].column.Name]; ok {
			f.fields[index].operator, f.fields[index].value = filter.Operator, filter.Value
		}
	}
	f.limit = strconv.Itoa(f.settings.pageSize(f.defaultPageSize))
	f.row, f.cell, f.scrollOffset, f.horizontalOffset = 0, browseFilterOperatorCell, 0, 0
	f.editing = false
	f.input.Blur()
	f.revealSelection()
}

func (m *Model) openBrowseFilterForm() tea.Cmd {
	if len(m.structureColumns) == 0 {
		m.Status = "table columns are loading"
		return nil
	}
	m.browseFilterForm = newBrowseFilterForm(m.structureColumns, m.browseSettings, m.browsePageSize, m.tableViewportWidth, m.formViewportHeight())
	m.formMode.mode = formModeNormal
	m.formMode.buttonsFocused = false
	if m.vimMode {
		return nil
	}
	command, _ := m.browseFilterForm.beginEdit()
	return command
}

func (f *browseFilterForm) setSize(width, height int) {
	f.width, f.height = max(width, 1), max(height, 1)
	f.resizeInput()
	f.revealSelection()
}

func (f *browseFilterForm) columns() []table.Column {
	columnWidth, typeWidth := ansi.StringWidth("Column"), ansi.StringWidth("Type")
	for _, field := range f.fields {
		columnWidth = max(columnWidth, ansi.StringWidth(field.column.Name))
		typeWidth = max(typeWidth, ansi.StringWidth(field.column.Type))
	}
	columnWidth = min(columnWidth, 20)
	typeWidth = min(typeWidth, 16)
	operatorWidth := ansi.StringWidth("IS NOT NULL")
	valueWidth := max(f.width-(columnWidth+typeWidth+operatorWidth+8*spaceCompact), 12)
	return []table.Column{
		{Title: "Column", Width: columnWidth},
		{Title: "Type", Width: typeWidth},
		{Title: "Operator", Width: operatorWidth},
		{Title: "Value", Width: valueWidth},
	}
}

func (f *browseFilterForm) resizeInput() {
	columns := f.columns()
	f.input.SetWidth(max(columns[3].Width, 1))
}

func (f *browseFilterForm) operatorOptions() []sharedsql.BrowseFilterOperator {
	if f.row >= len(f.fields) {
		return nil
	}
	typeName := browseColumnType(f.fields[f.row].column.Type)
	options := []sharedsql.BrowseFilterOperator{sharedsql.BrowseFilterNone}
	switch {
	case sharedsql.IsNumericColumnType(typeName), isBrowseTemporalType(typeName):
		return append(options, sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual, sharedsql.BrowseFilterLess, sharedsql.BrowseFilterLessEqual, sharedsql.BrowseFilterGreater, sharedsql.BrowseFilterGreaterEqual, sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull)
	case isBrowseTextType(typeName):
		return append(options, sharedsql.BrowseFilterLike, sharedsql.BrowseFilterNotLike, sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual, sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull)
	default:
		return append(options, sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual, sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull)
	}
}

func browseFilterOperatorLabel(operator sharedsql.BrowseFilterOperator) string {
	if operator == sharedsql.BrowseFilterNone {
		return "—"
	}
	return string(operator)
}

func browseColumnType(declaration string) string {
	declaration = strings.ToUpper(strings.TrimSpace(declaration))
	if index := strings.IndexAny(declaration, " ("); index >= 0 {
		return declaration[:index]
	}
	return declaration
}

func isBrowseTextType(typeName string) bool {
	switch typeName {
	case "CHAR", "VARCHAR", "TEXT", "CLOB":
		return true
	default:
		return false
	}
}

func isBrowseTemporalType(typeName string) bool {
	switch typeName {
	case "DATE", "TIME", "DATETIME", "TIMESTAMP", "TIMESTAMPTZ":
		return true
	default:
		return false
	}
}

func (f *browseFilterForm) Update(message tea.Msg, bindings Keybindings) (tea.Cmd, browseFilterAction) {
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		if f.editing && f.cell != browseFilterOperatorCell {
			var command tea.Cmd
			f.input, command = f.input.Update(message)
			return command, browseFilterNoAction
		}
		return nil, browseFilterNoAction
	}
	if f.editing {
		return f.updateEditor(keyPress)
	}
	if bindings.Match(keyPress, "browse_filter.apply", []scope{scopeForm}) {
		return nil, browseFilterApply
	}
	switch keyPress.Key().Code {
	case tea.KeyEscape:
		return nil, browseFilterDiscard
	case 'r':
		f.reset()
		return nil, browseFilterNoAction
	case 'i':
		return f.beginEdit()
	case tea.KeyBackspace:
		if f.row < len(f.fields) {
			if f.cell == browseFilterOperatorCell {
				f.fields[f.row].operator = sharedsql.BrowseFilterNone
			} else {
				f.fields[f.row].value = ""
			}
		}
		return nil, browseFilterNoAction
	case tea.KeyUp, 'k':
		f.row = max(f.row-1, 0)
	case tea.KeyDown, 'j':
		f.row = min(f.row+1, len(f.fields))
	case tea.KeyLeft, 'h':
		if f.row < len(f.fields) {
			f.cell = browseFilterOperatorCell
		}
	case tea.KeyRight, 'l':
		if f.row < len(f.fields) {
			f.cell = browseFilterValueCell
		}
	}
	f.revealSelection()
	return nil, browseFilterNoAction
}

func (f *browseFilterForm) beginEdit() (tea.Cmd, browseFilterAction) {
	f.editing = true
	if f.row == len(f.fields) {
		f.cell = browseFilterRowsCell
		f.input.SetValue(f.limit)
	} else if f.cell == browseFilterOperatorCell {
		options := f.operatorOptions()
		f.operatorIndex = 0
		for index, operator := range options {
			if operator == f.fields[f.row].operator {
				f.operatorIndex = index
				break
			}
		}
		return nil, browseFilterNoAction
	} else {
		f.input.SetValue(f.fields[f.row].value)
	}
	return f.input.Focus(), browseFilterNoAction
}

func (f *browseFilterForm) updateEditor(keyPress tea.KeyPressMsg) (tea.Cmd, browseFilterAction) {
	if keyPress.Key().Code == tea.KeyEscape {
		if f.row == len(f.fields) {
			f.limit = f.input.Value()
		} else if f.cell == browseFilterOperatorCell {
			f.fields[f.row].operator = f.operatorOptions()[f.operatorIndex]
		} else {
			f.fields[f.row].value = f.input.Value()
		}
		f.editing = false
		f.input.Blur()
		return nil, browseFilterNoAction
	}
	if f.cell == browseFilterOperatorCell {
		options := f.operatorOptions()
		switch keyPress.Key().Code {
		case tea.KeyUp, 'k':
			f.operatorIndex = max(f.operatorIndex-1, 0)
		case tea.KeyDown, 'j':
			f.operatorIndex = min(f.operatorIndex+1, len(options)-1)
		case tea.KeyEnter:
			f.fields[f.row].operator = options[f.operatorIndex]
			f.editing = false
		}
		return nil, browseFilterNoAction
	}
	if keyPress.Key().Code == tea.KeyEnter {
		if f.row == len(f.fields) {
			f.limit = f.input.Value()
		} else {
			f.fields[f.row].value = f.input.Value()
		}
		f.editing = false
		f.input.Blur()
		return nil, browseFilterNoAction
	}
	input, command := f.input.Update(keyPress)
	f.input = input
	return command, browseFilterNoAction
}

func (f *browseFilterForm) apply() (browseSettings, error) {
	if err := validateBrowseLimit(f.limit); err != nil {
		return browseSettings{}, err
	}
	settings := f.settings
	settings.filters = settings.filters[:0]
	for _, field := range f.fields {
		if field.operator != sharedsql.BrowseFilterNone {
			settings.filters = append(settings.filters, sharedsql.BrowseFilter{Column: field.column.Name, Operator: field.operator, Value: field.value})
		}
	}
	settings.limit, _ = strconv.Atoi(strings.TrimSpace(f.limit))
	return settings, nil
}

func (f *browseFilterForm) revealSelection() {
	selectedLine := f.row + 1
	if selectedLine < f.scrollOffset {
		f.scrollOffset = selectedLine
	} else if selectedLine >= f.scrollOffset+max(f.height, 1) {
		f.scrollOffset = selectedLine - max(f.height, 1) + 1
	}
	columns := f.columns()
	selectedColumn := 3
	if f.row < len(f.fields) {
		selectedColumn = int(f.cell) + 2
	}
	start := 0
	for index, column := range columns {
		end := start + column.Width + 2*spaceCompact
		if index == selectedColumn {
			if start < f.horizontalOffset {
				f.horizontalOffset = start
			} else if end > f.horizontalOffset+f.width {
				f.horizontalOffset = end - f.width
			}
			break
		}
		start = end
	}
	f.horizontalOffset = max(f.horizontalOffset, 0)
}

func (f browseFilterForm) View() string {
	columns := f.columns()
	lines := []string{headerStyle.Padding(0, 0).Render(tableLineWithSelection(columns, nil, nil, f.horizontalOffset, f.width, -1, false))}
	// Render only the visible field window; revealSelection keeps the
	// selected/edited row inside it, so up to 500-column tables render
	// at most one screenful per frame.
	rowHeight := max(f.height, 1)
	lastLine := min(len(f.fields), f.scrollOffset+rowHeight-1)
	for index := f.scrollOffset; index < lastLine; index++ {
		field := f.fields[index]
		operator, value := browseFilterOperatorLabel(field.operator), field.value
		if f.editing && f.row == index {
			if f.cell == browseFilterOperatorCell {
				operator = browseFilterOperatorLabel(f.operatorOptions()[f.operatorIndex])
			} else {
				value = f.input.View()
			}
		}
		lines = append(lines, tableLineWithSelection(columns, table.Row{field.column.Name, field.column.Type, operator, value}, nil, f.horizontalOffset, f.width, int(f.cell)+2, f.row == index))
	}
	// The Rows/limit line sits at index len(f.fields)+1; render it only
	// when it falls inside the visible window.
	if f.scrollOffset <= len(f.fields)+1 && len(f.fields)+1 < f.scrollOffset+rowHeight {
		limit := f.limit
		if f.editing && f.row == len(f.fields) {
			limit = f.input.View()
		}
		lines = append(lines, tableLineWithSelection(columns, table.Row{"Rows", "", "", limit}, nil, f.horizontalOffset, f.width, 3, f.row == len(f.fields)))
	}
	mode := "NORMAL"
	if f.editing {
		mode = "INSERT"
	}
	lines = append(lines, cropTableLine(fmt.Sprintf("%s | i edit | r reset | F5/Ctrl+S apply | Esc cancel", mode), 0, f.width))
	return strings.Join(lines, "\n")
}
