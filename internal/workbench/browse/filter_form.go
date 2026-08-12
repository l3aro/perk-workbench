package browse

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// FilterCell is the edited cell of a filter row.
type FilterCell uint8

const (
	FilterOperatorCell FilterCell = iota
	FilterValueCell
	FilterRowsCell
)

// FilterField is one filter row: the column, its operator, and the value.
type FilterField struct {
	Column   sharedsql.ColumnInfo
	Operator sharedsql.BrowseFilterOperator
	Value    string
}

// FilterAction is the outcome of one filter-form update the root acts on.
type FilterAction uint8

const (
	FilterNoAction FilterAction = iota
	FilterApply
	FilterDiscard
)

// FilterForm is the browse filter grid: one row per structure column plus
// the row-limit line, with inline operator/value editing and windowed
// rendering.
type FilterForm struct {
	Fields           []FilterField
	Settings         Settings
	DefaultPageSize  int
	Limit            string
	Row              int
	Cell             FilterCell
	OperatorIndex    int
	Input            textinput.Model
	Editing          bool
	Width, Height    int
	ScrollOffset     int
	HorizontalOffset int
}

// NewFilterForm builds the filter grid over the structure columns,
// pre-filling the saved filters and the current row limit.
func NewFilterForm(columns []sharedsql.ColumnInfo, settings Settings, defaultPageSize, width, height int) *FilterForm {
	saved := make(map[string]sharedsql.BrowseFilter, len(settings.Filters))
	for _, filter := range settings.Filters {
		saved[filter.Column] = filter
	}
	form := &FilterForm{
		Fields:          make([]FilterField, len(columns)),
		Settings:        settings,
		DefaultPageSize: defaultPageSize,
		Limit:           strconv.Itoa(settings.PageSize(defaultPageSize)),
		Width:           max(width, 1),
		Height:          max(height, 1),
	}
	for index, column := range columns {
		form.Fields[index].Column = column
		if filter, ok := saved[column.Name]; ok {
			form.Fields[index].Operator = filter.Operator
			form.Fields[index].Value = filter.Value
		}
	}
	form.Input = textinput.New()
	form.Input.Prompt = ""
	form.ResizeInput()
	return form
}

// Reset returns the grid to the saved settings and clears the edit state.
func (f *FilterForm) Reset() {
	saved := make(map[string]sharedsql.BrowseFilter, len(f.Settings.Filters))
	for _, filter := range f.Settings.Filters {
		saved[filter.Column] = filter
	}
	for index := range f.Fields {
		f.Fields[index].Operator, f.Fields[index].Value = sharedsql.BrowseFilterNone, ""
		if filter, ok := saved[f.Fields[index].Column.Name]; ok {
			f.Fields[index].Operator, f.Fields[index].Value = filter.Operator, filter.Value
		}
	}
	f.Limit = strconv.Itoa(f.Settings.PageSize(f.DefaultPageSize))
	f.Row, f.Cell, f.ScrollOffset, f.HorizontalOffset = 0, FilterOperatorCell, 0, 0
	f.Editing = false
	f.Input.Blur()
	f.RevealSelection()
}

// SetSize refits the grid to the pane geometry.
func (f *FilterForm) SetSize(width, height int) {
	f.Width, f.Height = max(width, 1), max(height, 1)
	f.ResizeInput()
	f.RevealSelection()
}

// Columns sizes the grid columns from the field content and the pane
// width.
func (f *FilterForm) Columns() []table.Column {
	columnWidth, typeWidth := ansi.StringWidth("Column"), ansi.StringWidth("Type")
	for _, field := range f.Fields {
		columnWidth = max(columnWidth, ansi.StringWidth(field.Column.Name))
		typeWidth = max(typeWidth, ansi.StringWidth(field.Column.Type))
	}
	columnWidth = min(columnWidth, 20)
	typeWidth = min(typeWidth, 16)
	operatorWidth := max(ansi.StringWidth("IS NOT NULL"), ansi.StringWidth("NOT PATTERN"))
	valueWidth := max(f.Width-(columnWidth+typeWidth+operatorWidth+8*uikit.SpaceCompact), 12)
	return []table.Column{
		{Title: "Column", Width: columnWidth},
		{Title: "Type", Width: typeWidth},
		{Title: "Operator", Width: operatorWidth},
		{Title: "Value", Width: valueWidth},
	}
}

// ResizeInput fits the inline value input to the Value column.
func (f *FilterForm) ResizeInput() {
	columns := f.Columns()
	f.Input.SetWidth(max(columns[3].Width, 1))
}

// OperatorOptions returns the operators offered for the selected row's
// column type.
func (f *FilterForm) OperatorOptions() []sharedsql.BrowseFilterOperator {
	if f.Row >= len(f.Fields) {
		return nil
	}
	typeName := ColumnType(f.Fields[f.Row].Column.Type)
	options := []sharedsql.BrowseFilterOperator{sharedsql.BrowseFilterNone}
	switch {
	case sharedsql.IsNumericColumnType(typeName), IsTemporalType(typeName):
		return append(options, sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual, sharedsql.BrowseFilterLess, sharedsql.BrowseFilterLessEqual, sharedsql.BrowseFilterGreater, sharedsql.BrowseFilterGreaterEqual, sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull)
	case IsTextType(typeName):
		return append(options, sharedsql.BrowseFilterLike, sharedsql.BrowseFilterNotLike, sharedsql.BrowseFilterPattern, sharedsql.BrowseFilterNotPattern, sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual, sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull)
	default:
		return append(options, sharedsql.BrowseFilterEqual, sharedsql.BrowseFilterNotEqual, sharedsql.BrowseFilterIsNull, sharedsql.BrowseFilterIsNotNull)
	}
}

// OperatorLabel renders an operator for the grid: the none-operator shows
// an em dash.
func OperatorLabel(operator sharedsql.BrowseFilterOperator) string {
	if operator == sharedsql.BrowseFilterNone {
		return "—"
	}
	return string(operator)
}

// ColumnType strips a SQL declaration down to its base type name.
func ColumnType(declaration string) string {
	declaration = strings.ToUpper(strings.TrimSpace(declaration))
	if index := strings.IndexAny(declaration, " ("); index >= 0 {
		return declaration[:index]
	}
	return declaration
}

// IsTextType reports whether the base type is a text type with pattern
// operators.
func IsTextType(typeName string) bool {
	switch typeName {
	case "CHAR", "VARCHAR", "TEXT", "CLOB":
		return true
	default:
		return false
	}
}

// IsTemporalType reports whether the base type is a date/time type.
func IsTemporalType(typeName string) bool {
	switch typeName {
	case "DATE", "TIME", "DATETIME", "TIMESTAMP", "TIMESTAMPTZ":
		return true
	default:
		return false
	}
}

// Update routes one message through the filter grid: apply/discard
// bindings, navigation keys, and inline editing.
func (f *FilterForm) Update(message tea.Msg, bindings uikit.KeyMatcher) (tea.Cmd, FilterAction) {
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		if f.Editing && f.Cell != FilterOperatorCell {
			var command tea.Cmd
			f.Input, command = f.Input.Update(message)
			return command, FilterNoAction
		}
		return nil, FilterNoAction
	}
	if f.Editing {
		return f.updateEditor(keyPress)
	}
	if bindings.Match(keyPress, "browse_filter.apply", []uikit.Scope{uikit.ScopeForm}) {
		return nil, FilterApply
	}
	switch keyPress.Key().Code {
	case tea.KeyEscape:
		return nil, FilterDiscard
	case 'r':
		f.Reset()
		return nil, FilterNoAction
	case 'i':
		return f.BeginEdit()
	case tea.KeyBackspace:
		if f.Row < len(f.Fields) {
			if f.Cell == FilterOperatorCell {
				f.Fields[f.Row].Operator = sharedsql.BrowseFilterNone
			} else {
				f.Fields[f.Row].Value = ""
			}
		}
		return nil, FilterNoAction
	case tea.KeyUp, 'k':
		f.Row = max(f.Row-1, 0)
	case tea.KeyDown, 'j':
		f.Row = min(f.Row+1, len(f.Fields))
	case tea.KeyLeft, 'h':
		if f.Row < len(f.Fields) {
			f.Cell = FilterOperatorCell
		}
	case tea.KeyRight, 'l':
		if f.Row < len(f.Fields) {
			f.Cell = FilterValueCell
		}
	}
	f.RevealSelection()
	return nil, FilterNoAction
}

// BeginEdit starts editing the selected cell (operator picker, value
// input, or the row-limit line).
func (f *FilterForm) BeginEdit() (tea.Cmd, FilterAction) {
	f.Editing = true
	if f.Row == len(f.Fields) {
		f.Cell = FilterRowsCell
		f.Input.SetValue(f.Limit)
	} else if f.Cell == FilterOperatorCell {
		options := f.OperatorOptions()
		f.OperatorIndex = 0
		for index, operator := range options {
			if operator == f.Fields[f.Row].Operator {
				f.OperatorIndex = index
				break
			}
		}
		return nil, FilterNoAction
	} else {
		f.Input.SetValue(f.Fields[f.Row].Value)
	}
	return f.Input.Focus(), FilterNoAction
}

func (f *FilterForm) updateEditor(keyPress tea.KeyPressMsg) (tea.Cmd, FilterAction) {
	if keyPress.Key().Code == tea.KeyEscape {
		if f.Row == len(f.Fields) {
			f.Limit = f.Input.Value()
		} else if f.Cell == FilterOperatorCell {
			f.Fields[f.Row].Operator = f.OperatorOptions()[f.OperatorIndex]
		} else {
			f.Fields[f.Row].Value = f.Input.Value()
		}
		f.Editing = false
		f.Input.Blur()
		return nil, FilterNoAction
	}
	if f.Cell == FilterOperatorCell {
		options := f.OperatorOptions()
		switch keyPress.Key().Code {
		case tea.KeyUp, 'k':
			f.OperatorIndex = max(f.OperatorIndex-1, 0)
		case tea.KeyDown, 'j':
			f.OperatorIndex = min(f.OperatorIndex+1, len(options)-1)
		case tea.KeyEnter:
			f.Fields[f.Row].Operator = options[f.OperatorIndex]
			f.Editing = false
		}
		return nil, FilterNoAction
	}
	if keyPress.Key().Code == tea.KeyEnter {
		if f.Row == len(f.Fields) {
			f.Limit = f.Input.Value()
		} else {
			f.Fields[f.Row].Value = f.Input.Value()
		}
		f.Editing = false
		f.Input.Blur()
		return nil, FilterNoAction
	}
	input, command := f.Input.Update(keyPress)
	f.Input = input
	return command, FilterNoAction
}

// Apply converts the grid into the settings the next browse query runs
// with, validating the row limit.
func (f *FilterForm) Apply() (Settings, error) {
	if err := ValidateLimit(f.Limit); err != nil {
		return Settings{}, err
	}
	settings := f.Settings
	settings.Filters = settings.Filters[:0]
	for _, field := range f.Fields {
		if field.Operator != sharedsql.BrowseFilterNone {
			settings.Filters = append(settings.Filters, sharedsql.BrowseFilter{Column: field.Column.Name, Operator: field.Operator, Value: field.Value})
		}
	}
	settings.Limit, _ = strconv.Atoi(strings.TrimSpace(f.Limit))
	return settings, nil
}

// ValidateLimit checks a user-entered row limit against the driver's
// maximum.
func ValidateLimit(value string) error {
	limit, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || limit < 1 || limit > sharedsql.MaxRows {
		return fmt.Errorf("enter a row limit from 1 to %d", sharedsql.MaxRows)
	}
	return nil
}

// RevealSelection keeps the selected/edited row and column inside the
// visible window.
func (f *FilterForm) RevealSelection() {
	selectedLine := f.Row + 1
	if selectedLine < f.ScrollOffset {
		f.ScrollOffset = selectedLine
	} else if selectedLine >= f.ScrollOffset+max(f.Height, 1) {
		f.ScrollOffset = selectedLine - max(f.Height, 1) + 1
	}
	columns := f.Columns()
	selectedColumn := 3
	if f.Row < len(f.Fields) {
		selectedColumn = int(f.Cell) + 2
	}
	start := 0
	for index, column := range columns {
		end := start + column.Width + 2*uikit.SpaceCompact
		if index == selectedColumn {
			if start < f.HorizontalOffset {
				f.HorizontalOffset = start
			} else if end > f.HorizontalOffset+f.Width {
				f.HorizontalOffset = end - f.Width
			}
			break
		}
		start = end
	}
	f.HorizontalOffset = max(f.HorizontalOffset, 0)
}

// View renders the windowed filter grid: the header, the visible field
// rows, the Rows/limit line, and the mode/hint line.
func (f FilterForm) View() string {
	columns := f.Columns()
	lines := []string{uikit.HeaderStyle.Padding(0, 0).Render(uikit.TableLineWithSelection(columns, nil, nil, f.HorizontalOffset, f.Width, -1, false))}
	// Render only the visible field window; RevealSelection keeps the
	// selected/edited row inside it, so up to 500-column tables render
	// at most one screenful per frame.
	rowHeight := max(f.Height, 1)
	lastLine := min(len(f.Fields), f.ScrollOffset+rowHeight-1)
	for index := f.ScrollOffset; index < lastLine; index++ {
		field := f.Fields[index]
		operator, value := OperatorLabel(field.Operator), field.Value
		if f.Editing && f.Row == index {
			if f.Cell == FilterOperatorCell {
				operator = OperatorLabel(f.OperatorOptions()[f.OperatorIndex])
			} else {
				value = f.Input.View()
			}
		}
		lines = append(lines, uikit.TableLineWithSelection(columns, table.Row{field.Column.Name, field.Column.Type, operator, value}, nil, f.HorizontalOffset, f.Width, int(f.Cell)+2, f.Row == index))
	}
	// The Rows/limit line sits at index len(f.Fields)+1; render it only
	// when it falls inside the visible window.
	if f.ScrollOffset <= len(f.Fields)+1 && len(f.Fields)+1 < f.ScrollOffset+rowHeight {
		limit := f.Limit
		if f.Editing && f.Row == len(f.Fields) {
			limit = f.Input.View()
		}
		lines = append(lines, uikit.TableLineWithSelection(columns, table.Row{"Rows", "", "", limit}, nil, f.HorizontalOffset, f.Width, 3, f.Row == len(f.Fields)))
	}
	mode := "NORMAL"
	if f.Editing {
		mode = "INSERT"
	}
	hint := ""
	if f.Row < len(f.Fields) {
		operator := f.Fields[f.Row].Operator
		if f.Editing && f.Cell == FilterOperatorCell {
			operator = f.OperatorOptions()[f.OperatorIndex]
		}
		if operator == sharedsql.BrowseFilterPattern || operator == sharedsql.BrowseFilterNotPattern {
			hint = " | PATTERN: * any, ? one char"
		}
	}
	lines = append(lines, uikit.CropTableLine(fmt.Sprintf("%s | i edit | r reset | F5/Ctrl+S apply | Esc cancel%s", mode, hint), 0, f.Width))
	return strings.Join(lines, "\n")
}

// CellAtX maps a click x (relative to the filter form) to the operator or
// value cell of a row. Column and type cells fall back to the value cell.
func (f *FilterForm) CellAtX(relX int) FilterCell {
	start := 0
	for index, column := range f.Columns() {
		end := start + column.Width + 2*uikit.SpaceCompact
		if index == 2 && relX >= start && relX < end {
			return FilterOperatorCell
		}
		start = end
	}
	return FilterValueCell
}
