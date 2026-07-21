package workbench

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type columnFormMode uint8

const (
	columnFormNormal columnFormMode = iota
	columnFormInsert
	columnFormConfirmSave
	columnFormConfirmDiscard
)

type columnFormAction uint8

const (
	columnFormNoAction columnFormAction = iota
	columnFormSave
	columnFormDiscard
)

const (
	columnFieldName = iota
	columnFieldType
	columnFieldNullable
	columnFieldDefault
	columnFieldCount
)

type columnForm struct {
	mode              columnFormMode
	focus             int
	pendingG          bool
	confirmed         bool
	saving            bool
	previousName      string
	primaryKey        int
	nullable          bool
	name, typ, preset textinput.Model
}

func newColumnForm(column sharedsql.ColumnInfo) columnForm {
	name := textinput.New()
	name.Prompt = "Name: "
	name.SetValue(column.Name)
	typ := textinput.New()
	typ.Prompt = "Type: "
	typ.SetValue(column.Type)
	preset := textinput.New()
	preset.Prompt = "Default: "
	if column.DefaultValue != nil {
		preset.SetValue(*column.DefaultValue)
	}
	return columnForm{previousName: column.Name, primaryKey: column.PrimaryKey, nullable: column.Nullable, name: name, typ: typ, preset: preset}
}

func (m *Model) openColumnForm() {
	row := m.structure.Cursor()
	if row < 0 || row >= len(m.structureColumns) {
		m.Status = "select a column"
		return
	}
	m.columnForm = newColumnForm(m.structureColumns[row])
}

func (f columnForm) active() bool { return f.previousName != "" }

func (f columnForm) confirming() bool {
	return f.mode == columnFormConfirmSave || f.mode == columnFormConfirmDiscard
}

func (f *columnForm) Update(message tea.Msg) (tea.Cmd, columnFormAction) {
	if f.saving {
		return nil, columnFormNoAction
	}
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		if f.mode != columnFormInsert {
			return nil, columnFormNoAction
		}
		return f.updateInput(message), columnFormNoAction
	}
	if f.confirming() {
		if keyPress.Key().Code == tea.KeyEscape {
			f.mode, f.confirmed = columnFormNormal, false
			return nil, columnFormNoAction
		}
		switch keyPress.String() {
		case "tab", "shift+tab":
			f.confirmed = !f.confirmed
		case "enter":
			if !f.confirmed {
				f.mode = columnFormNormal
				return nil, columnFormNoAction
			}
			if f.mode == columnFormConfirmSave {
				return nil, columnFormSave
			}
			return nil, columnFormDiscard
		}
		return nil, columnFormNoAction
	}
	if f.mode == columnFormInsert {
		if keyPress.Key().Code == tea.KeyEscape {
			f.mode = columnFormNormal
			f.blurInputs()
			return nil, columnFormNoAction
		}
		return f.updateInput(message), columnFormNoAction
	}
	if keyPress.Key().Code == tea.KeyEscape {
		f.mode, f.confirmed = columnFormConfirmDiscard, false
		return nil, columnFormNoAction
	}
	switch keyPress.String() {
	case "esc", "escape":
		f.mode, f.confirmed = columnFormConfirmDiscard, false
	case "ctrl+enter", "f5":
		f.mode, f.confirmed = columnFormConfirmSave, false
	case "j", "down":
		f.focus = (f.focus + 1) % columnFieldCount
	case "k", "up":
		f.focus = (f.focus + columnFieldCount - 1) % columnFieldCount
	case "g":
		if f.pendingG {
			f.focus, f.pendingG = columnFieldName, false
			return nil, columnFormNoAction
		}
		f.pendingG = true
		return nil, columnFormNoAction
	case "G":
		f.focus, f.pendingG = columnFieldDefault, false
	case "i":
		f.pendingG = false
		if f.focus == columnFieldNullable {
			f.nullable = !f.nullable
			return nil, columnFormNoAction
		}
		f.mode = columnFormInsert
		return f.focusInput(), columnFormNoAction
	case "enter":
		f.pendingG = false
		if f.focus == columnFieldNullable {
			f.nullable = !f.nullable
		}
	default:
		f.pendingG = false
	}
	if keyPress.Key().Code == ' ' && f.focus == columnFieldNullable {
		f.nullable = !f.nullable
	}
	return nil, columnFormNoAction
}

func (f *columnForm) focusInput() tea.Cmd {
	f.blurInputs()
	switch f.focus {
	case columnFieldName:
		return f.name.Focus()
	case columnFieldType:
		return f.typ.Focus()
	case columnFieldDefault:
		return f.preset.Focus()
	}
	return nil
}

func (f *columnForm) blurInputs() {
	f.name.Blur()
	f.typ.Blur()
	f.preset.Blur()
}

func (f *columnForm) updateInput(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	switch f.focus {
	case columnFieldName:
		f.name, command = f.name.Update(message)
	case columnFieldType:
		f.typ, command = f.typ.Update(message)
	case columnFieldDefault:
		f.preset, command = f.preset.Update(message)
	}
	return command
}

func (f columnForm) change() sharedsql.ColumnChange {
	change := sharedsql.ColumnChange{PreviousName: f.previousName, Name: f.name.Value(), Type: f.typ.Value(), Nullable: f.nullable}
	if value := strings.TrimSpace(f.preset.Value()); value != "" {
		change.DefaultValue = &value
	}
	return change
}

func (f *columnForm) setWidth(width int) {
	for _, input := range []*textinput.Model{&f.name, &f.typ, &f.preset} {
		input.SetWidth(max(width-12, 1))
	}
}

func (f columnForm) View() string {
	if f.saving {
		return statusStyle.Render("saving column changes")
	}
	if f.confirming() {
		action := "Save column changes?"
		if f.mode == columnFormConfirmDiscard {
			action = "Discard column changes?"
		}
		choices := "[false] true"
		if f.confirmed {
			choices = "false [true]"
		}
		return headerStyle.Render(action) + "\n" + choices + "\n" + statusStyle.Render("Tab selects true | Enter confirms | Esc returns to the form")
	}
	fields := []string{f.field(columnFieldName, f.name.View()), f.field(columnFieldType, f.typ.View())}
	nullable := "Nullable: false"
	if f.nullable {
		nullable = "Nullable: true"
	}
	fields = append(fields, f.field(columnFieldNullable, nullable), f.field(columnFieldDefault, f.preset.View()), "PK: "+primaryKeyText(f.primaryKey))
	help := "j/k fields | gg/G first/last | i edit | space toggle nullable | F5 save | Esc discard"
	if f.mode == columnFormInsert {
		help = "insert mode | Esc normal mode"
	}
	return strings.Join(fields, "\n") + "\n" + statusStyle.Render(help)
}

func (f columnForm) field(index int, value string) string {
	if f.focus == index && f.mode == columnFormNormal {
		return headerStyle.Render(value)
	}
	return value
}

func primaryKeyText(position int) string {
	if position == 0 {
		return ""
	}
	return strconv.Itoa(position) + " (read-only)"
}
