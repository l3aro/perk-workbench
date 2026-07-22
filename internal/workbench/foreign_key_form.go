package workbench

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type foreignKeyFormMode uint8

const (
	foreignKeyFormNormal foreignKeyFormMode = iota
	foreignKeyFormInsert
	foreignKeyFormConfirmSave
	foreignKeyFormConfirmDiscard
	foreignKeyFormConfirmDelete
)

type foreignKeyFormAction uint8

const (
	foreignKeyFormNoAction foreignKeyFormAction = iota
	foreignKeyFormSave
	foreignKeyFormDiscard
	foreignKeyFormDelete
)

type foreignKeyForm struct {
	mode                                                          foreignKeyFormMode
	focus                                                         int
	confirmed, saving, open                                       bool
	previous                                                      string
	columns, referenceTable, referenceColumns, onDelete, onUpdate textinput.Model
}

func newForeignKeyForm(foreignKey *sharedsql.ForeignKeyInfo) foreignKeyForm {
	columns, referenceTable, referenceColumns, onDelete, onUpdate := textinput.New(), textinput.New(), textinput.New(), textinput.New(), textinput.New()
	columns.Prompt, referenceTable.Prompt, referenceColumns.Prompt, onDelete.Prompt, onUpdate.Prompt = "", "", "", "", ""
	form := foreignKeyForm{columns: columns, referenceTable: referenceTable, referenceColumns: referenceColumns, onDelete: onDelete, onUpdate: onUpdate, open: true}
	form.onDelete.SetValue("NO ACTION")
	form.onUpdate.SetValue("NO ACTION")
	if foreignKey != nil {
		form.previous = foreignKey.ID
		form.columns.SetValue(strings.Join(foreignKey.Columns, ", "))
		form.referenceTable.SetValue(foreignKey.ReferenceTable)
		form.referenceColumns.SetValue(strings.Join(foreignKey.ReferenceColumns, ", "))
		form.onDelete.SetValue(foreignKey.OnDelete)
		form.onUpdate.SetValue(foreignKey.OnUpdate)
	}
	return form
}

func (f foreignKeyForm) active() bool { return f.open }

func (f foreignKeyForm) confirming() bool {
	return f.mode == foreignKeyFormConfirmSave || f.mode == foreignKeyFormConfirmDiscard || f.mode == foreignKeyFormConfirmDelete
}

func (f *foreignKeyForm) close() { f.open = false }

func (f *foreignKeyForm) Update(message tea.Msg) (tea.Cmd, foreignKeyFormAction) {
	key, ok := message.(tea.KeyPressMsg)
	if f.saving {
		return nil, foreignKeyFormNoAction
	}
	if !ok && f.mode == foreignKeyFormInsert {
		return f.updateInput(message), foreignKeyFormNoAction
	}
	if !ok {
		return nil, foreignKeyFormNoAction
	}
	if f.confirming() {
		if key.Key().Code == tea.KeyEscape {
			f.mode, f.confirmed = foreignKeyFormNormal, false
			return nil, foreignKeyFormNoAction
		}
		switch key.String() {
		case "tab", "shift+tab":
			f.confirmed = !f.confirmed
		case "enter":
			if !f.confirmed {
				f.mode = foreignKeyFormNormal
				return nil, foreignKeyFormNoAction
			}
			switch f.mode {
			case foreignKeyFormConfirmSave:
				return nil, foreignKeyFormSave
			case foreignKeyFormConfirmDelete:
				return nil, foreignKeyFormDelete
			default:
				return nil, foreignKeyFormDiscard
			}
		}
		return nil, foreignKeyFormNoAction
	}
	if f.mode == foreignKeyFormInsert {
		if key.Key().Code == tea.KeyEscape {
			f.mode = foreignKeyFormNormal
			f.blurInputs()
			return nil, foreignKeyFormNoAction
		}
		return f.updateInput(message), foreignKeyFormNoAction
	}
	if key.Key().Code == tea.KeyEscape {
		f.mode = foreignKeyFormConfirmDiscard
		return nil, foreignKeyFormNoAction
	}
	switch key.String() {
	case "j", "down":
		f.focus = (f.focus + 1) % 5
	case "k", "up":
		f.focus = (f.focus + 4) % 5
	case "i", "enter":
		f.mode = foreignKeyFormInsert
		return f.focusInput(), foreignKeyFormNoAction
	case "ctrl+enter", "f5":
		f.mode, f.confirmed = foreignKeyFormConfirmSave, false
	case "d":
		if f.previous != "" {
			f.mode, f.confirmed = foreignKeyFormConfirmDelete, false
		}
	}
	return nil, foreignKeyFormNoAction
}

func (f *foreignKeyForm) blurInputs() {
	f.columns.Blur()
	f.referenceTable.Blur()
	f.referenceColumns.Blur()
	f.onDelete.Blur()
	f.onUpdate.Blur()
}

func (f *foreignKeyForm) focusInput() tea.Cmd {
	f.blurInputs()
	switch f.focus {
	case 0:
		return f.columns.Focus()
	case 1:
		return f.referenceTable.Focus()
	case 2:
		return f.referenceColumns.Focus()
	case 3:
		return f.onDelete.Focus()
	default:
		return f.onUpdate.Focus()
	}
}

func (f *foreignKeyForm) updateInput(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	switch f.focus {
	case 0:
		f.columns, command = f.columns.Update(message)
	case 1:
		f.referenceTable, command = f.referenceTable.Update(message)
	case 2:
		f.referenceColumns, command = f.referenceColumns.Update(message)
	case 3:
		f.onDelete, command = f.onDelete.Update(message)
	default:
		f.onUpdate, command = f.onUpdate.Update(message)
	}
	return command
}

func (f foreignKeyForm) change() (sharedsql.ForeignKeyChange, error) {
	change := sharedsql.ForeignKeyChange{
		Columns:          splitForeignKeyColumns(f.columns.Value()),
		ReferenceTable:   strings.TrimSpace(f.referenceTable.Value()),
		ReferenceColumns: splitForeignKeyColumns(f.referenceColumns.Value()),
		OnDelete:         strings.ToUpper(strings.TrimSpace(f.onDelete.Value())),
		OnUpdate:         strings.ToUpper(strings.TrimSpace(f.onUpdate.Value())),
	}
	if err := sharedsql.ValidateForeignKeyChange(change); err != nil {
		return sharedsql.ForeignKeyChange{}, err
	}
	return change, nil
}

func splitForeignKeyColumns(value string) []string {
	columns := strings.Split(value, ",")
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	return columns
}

func (f *foreignKeyForm) setWidth(width int) {
	fieldWidth := max(width-formLabelWidth-len(formFieldGap)-1, 1)
	f.columns.SetWidth(fieldWidth)
	f.referenceTable.SetWidth(fieldWidth)
	f.referenceColumns.SetWidth(fieldWidth)
	f.onDelete.SetWidth(fieldWidth)
	f.onUpdate.SetWidth(fieldWidth)
}

func (f foreignKeyForm) View() string {
	if f.saving {
		return statusStyle.Render("saving foreign-key changes")
	}
	if f.confirming() {
		action := "Discard foreign-key changes?"
		if f.mode == foreignKeyFormConfirmSave {
			action = "Save foreign-key changes?"
		}
		if f.mode == foreignKeyFormConfirmDelete {
			action = "Delete foreign key?"
		}
		choices := "[" + booleanValue(false) + "] " + booleanValue(true)
		if f.confirmed {
			choices = booleanValue(false) + " [" + booleanValue(true) + "]"
		}
		return headerStyle.Render(action) + "\n" + choices + "\n" + statusStyle.Render("Tab selects true | Enter confirms | Esc returns to the form")
	}
	rows := []formRow{{label: "Columns", value: f.columns.View(), focusKey: 0}, {label: "Reference table", value: f.referenceTable.View(), focusKey: 1}, {label: "Reference columns", value: f.referenceColumns.View(), focusKey: 2}, {label: "On delete", value: f.onDelete.View(), focusKey: 3}, {label: "On update", value: f.onUpdate.View(), focusKey: 4}}
	lines := make([]string, len(rows))
	for index, row := range rows {
		label := formLabel(row.label, formLabelWidth)
		if f.mode == foreignKeyFormNormal && f.focus == row.focusKey {
			label = focusedFormLabel(row.label, formLabelWidth)
		}
		lines[index] = label + formFieldGap + row.value
	}
	help := "j/k fields | i edit | F5 save | d delete | Esc discard"
	if f.mode == foreignKeyFormInsert {
		help = "insert mode | Esc normal mode"
	}
	return strings.Join(lines, "\n") + "\n" + statusStyle.Render(help)
}
