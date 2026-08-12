package browse

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// CellEditor is the single-cell edit dialog: the huh field, its
// confirmation, and the row/column context the driver needs for the
// UpdateRow key. The root owns the confirmation overlay and the save
// execution; the component owns the editor construction and rendering.
type CellEditor struct {
	Input       *huh.Form
	Confirm     *uikit.ConfirmationDialog
	Confirming  bool
	Table       string
	ColumnName  string
	ColumnIndex int
	PrimaryKeys []int
	Columns     []string
	Original    []*string // whole row values
	OriginalVal *string
	EditedVal   string
	Width       int
}

// IsLongTextType returns true when the SQL type is a long-form text type
// that should use the huh.Text multi-line / external-editor field instead
// of the single-line huh.Input.
func IsLongTextType(typeName string) bool {
	upper := strings.ToUpper(typeName)
	for _, prefix := range []string{"TEXT", "MEDIUMTEXT", "LONGTEXT", "TINYTEXT", "CLOB", "JSON"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// Active reports whether the editor has an input or an open confirmation.
func (e *CellEditor) Active() bool {
	if e.Confirming {
		return e.Confirm != nil
	}
	return e.Input != nil
}

// KeyValues converts the whole-row primary-key values to the RowValue key
// list used by the driver's UpdateRow; NULL keys stay NULL.
func (e *CellEditor) KeyValues() ([]sharedsql.RowValue, error) {
	if len(e.PrimaryKeys) == 0 {
		return nil, fmt.Errorf("cannot edit: no primary key")
	}
	key := make([]sharedsql.RowValue, 0, len(e.PrimaryKeys))
	for _, pk := range e.PrimaryKeys {
		value := sharedsql.Value{Kind: sharedsql.ValueString}
		if e.Original[pk] == nil {
			value.Kind = sharedsql.ValueNull
		} else {
			value.String = *e.Original[pk]
		}
		key = append(key, sharedsql.RowValue{Name: e.Columns[pk], Value: value})
	}
	return key, nil
}

// Preview renders the structured cell-write preview for the confirmation
// and query-log entry: Table, Key, and the single Change.
func (e *CellEditor) Preview() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Table: %s", e.Table)
	builder.WriteString("\nKey:")
	if key, err := e.KeyValues(); err == nil {
		for _, row := range key {
			fmt.Fprintf(&builder, "\n  %s = %s", row.Name, RowValuePreview(row.Value))
		}
	}
	fmt.Fprintf(&builder, "\nChanges:\n  %s = %s", e.ColumnName, strconv.Quote(e.EditedVal))
	return builder.String()
}

// BuildCellEditor constructs the cell editor for the selected cell of the
// given table. The editor is nil with a nil error when there is nothing
// to edit at the selection; a non-nil error explains why the cell cannot
// be edited (the root shows it as a status).
func (m Model) BuildCellEditor(table string, width int) (*CellEditor, tea.Cmd, error) {
	row := m.Table.Cursor()
	if row < 0 || row >= len(m.Result.Rows) {
		return nil, nil, fmt.Errorf("select a row")
	}
	col := m.SelectedColumn
	columns := m.Result.Columns
	if col < 0 || col >= len(columns) {
		return nil, nil, nil
	}
	browseRow := m.Result.Rows[row]

	// Find primary key indices from structure info
	var primaryKeys []int
	for _, info := range m.Structure {
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
		return nil, nil, fmt.Errorf("cannot edit: no primary key")
	}

	originalVal := browseRow[col]
	editedVal := ""
	if originalVal != nil {
		editedVal = *originalVal
	}

	// Detect column type for choosing input vs text field
	colType := ""
	for _, info := range m.Structure {
		if strings.EqualFold(info.Name, columns[col]) {
			colType = info.Type
			break
		}
	}

	e := &CellEditor{
		Table:       table,
		ColumnName:  columns[col],
		ColumnIndex: col,
		PrimaryKeys: primaryKeys,
		Columns:     columns,
		Original:    browseRow,
		OriginalVal: originalVal,
		EditedVal:   editedVal,
		Width:       width,
	}

	var field huh.Field
	if IsLongTextType(colType) {
		field = huh.NewText().
			Key("value").
			Title("Edit " + e.ColumnName).
			Value(&e.EditedVal)
	} else {
		field = huh.NewInput().
			Key("value").
			Title("Edit " + e.ColumnName).
			Value(&e.EditedVal)
	}

	km := huh.NewDefaultKeyMap()
	km.Input.Submit = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	km.Input.Next = key.NewBinding(key.WithDisabled())
	km.Input.AcceptSuggestion = key.NewBinding(key.WithDisabled())
	km.Text.Submit = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	km.Text.Next = key.NewBinding(key.WithDisabled())
	e.Input = uikit.NewForm(
		huh.NewGroup(field),
	).WithShowHelp(true).WithWidth(width).WithKeyMap(km)
	return e, e.Input.Init(), nil
}

// BeginConfirmation opens the save-cell confirmation carrying the
// structured preview.
func (e *CellEditor) BeginConfirmation() tea.Cmd {
	e.Confirming = true
	e.Confirm = uikit.YesNoConfirmation("Save cell change?", e.Preview(), "save")
	return nil
}

// ConfirmContent renders the editor body: the confirmation when open,
// otherwise the padded input view with the Save/Cancel button bar.
func (e *CellEditor) ConfirmContent() string {
	if e.Confirming && e.Confirm != nil {
		return e.Confirm.Content(e.Width)
	}
	if !e.Confirming && e.Input != nil {
		raw := e.Input.View()
		var b strings.Builder
		for i, line := range strings.Split(raw, "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			trimmed := strings.TrimRight(line, " ")
			if w := ansi.StringWidth(trimmed); w < e.Width {
				trimmed += strings.Repeat(" ", e.Width-w)
			}
			b.WriteString(trimmed)
		}
		buttons := uikit.FormButtonsBar(false, 0)
		if w := ansi.StringWidth(buttons); w < e.Width {
			buttons += strings.Repeat(" ", e.Width-w)
		}
		b.WriteByte('\n')
		b.WriteString(buttons)
		return b.String()
	}
	return ""
}

// ButtonAt maps a click on the editor dialog to its bottom button
// ("save"/"cancel"), replicating the confirmation dialog's centered
// layout. The buttons row is the last content line.
func (e *CellEditor) ButtonAt(x, y int, layout uikit.Layout) string {
	if e == nil || e.Confirming {
		return ""
	}
	contentLines := len(strings.Split(e.Input.View(), "\n")) + 1 // + buttons row
	dialogW := min(e.Width, max(layout.Width-6, 1))
	dialogH := min(contentLines, max(layout.Height-6, 1))
	boxX := max(0, (layout.Width-dialogW-2)/2)
	boxY := max(0, (layout.Height-dialogH-2)/2)
	if y != boxY+dialogH {
		return ""
	}
	return uikit.FormButtonAt(x - boxX - 1)
}
