package workbench

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type browseFormMode uint8

const (
	browseFormNormal browseFormMode = iota
	browseFormInsert
	browseFormConfirmSave
	browseFormConfirmDiscard
)

type browseFormAction uint8

const (
	browseFormNoAction browseFormAction = iota
	browseFormSave
	browseFormDiscard
)

type browseForm struct {
	mode      browseFormMode
	focus     int
	pendingG  bool
	confirmed bool
	saving    bool
	columns   []string
	values    []*string
	nulls     []bool
	primary   []int
	inputs    []textinput.Model
}

func (m *Model) openBrowseForm() {
	row := m.browse.Cursor()
	if row < 0 || row >= len(m.browseResult.Rows) {
		m.Status = "select a row"
		return
	}
	form, err := newBrowseForm(m.browseResult.Columns, m.browseResult.Rows[row], m.structureColumns)
	if err != nil {
		m.Status = safeText(err.Error())
		return
	}
	m.browseForm = form
}

func newBrowseForm(columns []string, values []*string, info []sharedsql.ColumnInfo) (browseForm, error) {
	if len(columns) == 0 || len(columns) != len(values) {
		return browseForm{}, fmt.Errorf("selected row is unavailable")
	}
	primaryNames := make(map[string]bool, len(info))
	for _, column := range info {
		if column.PrimaryKey > 0 {
			primaryNames[strings.ToLower(column.Name)] = true
		}
	}
	form := browseForm{columns: append([]string(nil), columns...), values: append([]*string(nil), values...), nulls: make([]bool, len(values)), inputs: make([]textinput.Model, len(values))}
	for index, value := range values {
		input := textinput.New()
		input.Prompt = columns[index] + ": "
		if value == nil {
			form.nulls[index] = true
		} else {
			input.SetValue(*value)
		}
		form.inputs[index] = input
		if primaryNames[strings.ToLower(columns[index])] {
			form.primary = append(form.primary, index)
		}
	}
	if len(form.primary) == 0 {
		return browseForm{}, fmt.Errorf("cannot edit rows without a primary key")
	}
	return form, nil
}

func (f browseForm) active() bool { return len(f.columns) > 0 }

func (f browseForm) confirming() bool {
	return f.mode == browseFormConfirmSave || f.mode == browseFormConfirmDiscard
}

func (f *browseForm) Update(message tea.Msg) (tea.Cmd, browseFormAction) {
	if f.saving {
		return nil, browseFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		if f.mode == browseFormInsert {
			return f.updateInput(message), browseFormNoAction
		}
		return nil, browseFormNoAction
	}
	if f.confirming() {
		if keyPress.Key().Code == tea.KeyEscape {
			f.mode, f.confirmed = browseFormNormal, false
			return nil, browseFormNoAction
		}
		switch keyPress.String() {
		case "tab", "shift+tab":
			f.confirmed = !f.confirmed
		case "enter":
			if !f.confirmed {
				f.mode = browseFormNormal
				return nil, browseFormNoAction
			}
			if f.mode == browseFormConfirmSave {
				return nil, browseFormSave
			}
			return nil, browseFormDiscard
		}
		return nil, browseFormNoAction
	}
	if f.mode == browseFormInsert {
		if keyPress.Key().Code == tea.KeyEscape {
			f.mode = browseFormNormal
			f.blurInputs()
			return nil, browseFormNoAction
		}
		f.nulls[f.focus] = false
		return f.updateInput(message), browseFormNoAction
	}
	if keyPress.Key().Code == tea.KeyEscape {
		f.mode, f.confirmed = browseFormConfirmDiscard, false
		return nil, browseFormNoAction
	}
	switch keyPress.String() {
	case "esc", "escape":
		f.mode, f.confirmed = browseFormConfirmDiscard, false
	case "ctrl+enter", "f5":
		f.mode, f.confirmed = browseFormConfirmSave, false
	case "j", "down":
		f.focus = (f.focus + 1) % len(f.inputs)
	case "k", "up":
		f.focus = (f.focus + len(f.inputs) - 1) % len(f.inputs)
	case "g":
		if f.pendingG {
			f.focus, f.pendingG = 0, false
			return nil, browseFormNoAction
		}
		f.pendingG = true
		return nil, browseFormNoAction
	case "G":
		f.focus, f.pendingG = len(f.inputs)-1, false
	case "i", "enter":
		f.pendingG = false
		f.mode = browseFormInsert
		return f.focusInput(), browseFormNoAction
	case " ":
		f.nulls[f.focus] = !f.nulls[f.focus]
	default:
		f.pendingG = false
	}
	return nil, browseFormNoAction
}

func (f *browseForm) focusInput() tea.Cmd {
	f.blurInputs()
	return f.inputs[f.focus].Focus()
}

func (f *browseForm) blurInputs() {
	for index := range f.inputs {
		f.inputs[index].Blur()
	}
}

func (f *browseForm) updateInput(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	f.inputs[f.focus], command = f.inputs[f.focus].Update(message)
	return command
}

func (f *browseForm) setWidth(width int) {
	for index := range f.inputs {
		f.inputs[index].SetWidth(max(width-16, 1))
	}
}

func (f browseForm) updateStatement(table string) (string, error) {
	if !f.active() || len(f.primary) == 0 {
		return "", fmt.Errorf("selected row cannot be updated")
	}
	sets := make([]string, len(f.columns))
	for index, column := range f.columns {
		sets[index] = quoteBrowseIdentifier(column) + " = " + f.value(index)
	}
	where := make([]string, len(f.primary))
	for index, primary := range f.primary {
		if f.values[primary] == nil {
			where[index] = quoteBrowseIdentifier(f.columns[primary]) + " IS NULL"
		} else {
			where[index] = quoteBrowseIdentifier(f.columns[primary]) + " = " + quoteBrowseValue(*f.values[primary])
		}
	}
	return "UPDATE " + quoteBrowseIdentifier(table) + " SET " + strings.Join(sets, ", ") + " WHERE " + strings.Join(where, " AND "), nil
}

func (f browseForm) value(index int) string {
	if f.nulls[index] {
		return "NULL"
	}
	return quoteBrowseValue(f.inputs[index].Value())
}

func quoteBrowseIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func quoteBrowseValue(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func (f browseForm) View() string {
	if f.saving {
		return statusStyle.Render("saving row changes")
	}
	if f.confirming() {
		action := "Save row changes?"
		if f.mode == browseFormConfirmDiscard {
			action = "Discard row changes?"
		}
		choices := "[false] true"
		if f.confirmed {
			choices = "false [true]"
		}
		return headerStyle.Render(action) + "\n" + choices + "\n" + statusStyle.Render("Tab selects true | Enter confirms | Esc returns to the form")
	}
	fields := make([]string, len(f.inputs))
	for index, input := range f.inputs {
		value := input.View()
		if f.nulls[index] {
			value = f.columns[index] + ": NULL"
		}
		if f.focus == index && f.mode == browseFormNormal {
			value = headerStyle.Render(value)
		}
		fields[index] = value
	}
	help := "j/k fields | gg/G first/last | i edit | space toggle NULL | F5 save | Esc discard"
	if f.mode == browseFormInsert {
		help = "insert mode | Esc normal mode"
	}
	return strings.Join(fields, "\n") + "\n" + statusStyle.Render(help)
}
