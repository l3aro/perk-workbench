package workbench

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type indexFormMode uint8

const (
	indexFormNormal indexFormMode = iota
	indexFormInsert
	indexFormConfirmSave
	indexFormConfirmDiscard
	indexFormConfirmDelete
)

type indexFormAction uint8

const (
	indexFormNoAction indexFormAction = iota
	indexFormSave
	indexFormDiscard
	indexFormDelete
)

type indexForm struct {
	mode               indexFormMode
	focus              int
	confirmed, saving  bool
	open               bool
	previous           string
	name, columns      textinput.Model
	unique, primaryKey bool
}

func newIndexForm(index *sharedsql.IndexInfo) indexForm {
	name, columns := textinput.New(), textinput.New()
	name.Prompt, columns.Prompt = "", ""
	form := indexForm{name: name, columns: columns, open: true}
	if index != nil {
		form.previous, form.unique, form.primaryKey = index.Name, index.Unique, index.PrimaryKey
		form.name.SetValue(index.Name)
		form.columns.SetValue(strings.Join(index.Columns, ", "))
	}
	return form
}

func (f indexForm) active() bool { return f.open }

func (f indexForm) confirming() bool {
	return f.mode == indexFormConfirmSave || f.mode == indexFormConfirmDiscard || f.mode == indexFormConfirmDelete
}

func (f *indexForm) close() { f.open = false }

func (f *indexForm) Update(message tea.Msg) (tea.Cmd, indexFormAction) {
	key, ok := message.(tea.KeyPressMsg)
	if f.saving {
		return nil, indexFormNoAction
	}
	if !ok && f.mode == indexFormInsert {
		return f.updateInput(message), indexFormNoAction
	}
	if !ok {
		return nil, indexFormNoAction
	}
	if f.confirming() {
		if key.Key().Code == tea.KeyEscape {
			f.mode, f.confirmed = indexFormNormal, false
			return nil, indexFormNoAction
		}
		switch key.String() {
		case "tab", "shift+tab":
			f.confirmed = !f.confirmed
		case "enter":
			if !f.confirmed {
				f.mode = indexFormNormal
				return nil, indexFormNoAction
			}
			switch f.mode {
			case indexFormConfirmSave:
				return nil, indexFormSave
			case indexFormConfirmDelete:
				return nil, indexFormDelete
			default:
				return nil, indexFormDiscard
			}
		}
		return nil, indexFormNoAction
	}
	if f.mode == indexFormInsert {
		if key.Key().Code == tea.KeyEscape {
			f.mode = indexFormNormal
			f.name.Blur()
			f.columns.Blur()
			return nil, indexFormNoAction
		}
		return f.updateInput(message), indexFormNoAction
	}
	if key.Key().Code == tea.KeyEscape {
		f.mode = indexFormConfirmDiscard
		return nil, indexFormNoAction
	}
	switch key.String() {
	case "j", "down":
		f.focus = (f.focus + 1) % 4
	case "k", "up":
		f.focus = (f.focus + 3) % 4
	case "i", "enter":
		if f.focus == 2 {
			f.unique = !f.unique
			if f.unique {
				f.primaryKey = false
			}
		} else if f.focus == 3 {
			f.primaryKey = !f.primaryKey
			if f.primaryKey {
				f.unique = false
			}
		} else {
			f.mode = indexFormInsert
			return f.focusInput(), indexFormNoAction
		}
	case " ", "space":
		if f.focus == 2 {
			f.unique = !f.unique
			if f.unique {
				f.primaryKey = false
			}
		} else if f.focus == 3 {
			f.primaryKey = !f.primaryKey
			if f.primaryKey {
				f.unique = false
			}
		}
	case "ctrl+enter", "f5":
		f.mode, f.confirmed = indexFormConfirmSave, false
	case "d":
		if f.previous != "" {
			f.mode, f.confirmed = indexFormConfirmDelete, false
		}
	}
	return nil, indexFormNoAction
}

func (f *indexForm) focusInput() tea.Cmd {
	f.name.Blur()
	f.columns.Blur()
	if f.focus == 0 {
		return f.name.Focus()
	}
	return f.columns.Focus()
}

func (f *indexForm) updateInput(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	if f.focus == 0 {
		f.name, command = f.name.Update(message)
	} else {
		f.columns, command = f.columns.Update(message)
	}
	return command
}

func (f indexForm) change() (sharedsql.IndexChange, error) {
	columns := strings.Split(f.columns.Value(), ",")
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index])
	}
	change := sharedsql.IndexChange{Name: strings.TrimSpace(f.name.Value()), Unique: f.unique, PrimaryKey: f.primaryKey, Columns: columns}
	if change.PrimaryKey {
		change.Name = "PRIMARY"
	}
	if err := sharedsql.ValidateIndexChange(change); err != nil {
		return sharedsql.IndexChange{}, err
	}
	return change, nil
}

func (f *indexForm) setWidth(width int) {
	f.name.SetWidth(max(width-formLabelWidth-len(formFieldGap)-1, 1))
	f.columns.SetWidth(max(width-formLabelWidth-len(formFieldGap)-1, 1))
}

func (f indexForm) View() string {
	if f.saving {
		return statusStyle.Render("saving index changes")
	}
	if f.confirming() {
		action := "Discard index changes?"
		if f.mode == indexFormConfirmSave {
			action = "Save index changes?"
		}
		if f.mode == indexFormConfirmDelete {
			action = "Delete index?"
		}
		choices := "[" + booleanValue(false) + "] " + booleanValue(true)
		if f.confirmed {
			choices = booleanValue(false) + " [" + booleanValue(true) + "]"
		}
		return headerStyle.Render(action) + "\n" + choices + "\n" + statusStyle.Render("Tab selects true | Enter confirms | Esc returns to the form")
	}
	rows := []formRow{{label: "Name", value: f.name.View(), focusKey: 0}, {label: "Columns", value: f.columns.View(), focusKey: 1}, {label: "Unique", value: booleanValue(f.unique), focusKey: 2}, {label: "Primary key", value: booleanValue(f.primaryKey), focusKey: 3}}
	lines := make([]string, len(rows))
	for index, row := range rows {
		label := formLabel(row.label, formLabelWidth)
		if f.mode == indexFormNormal && f.focus == row.focusKey {
			label = focusedFormLabel(row.label, formLabelWidth)
		}
		lines[index] = label + formFieldGap + row.value
	}
	help := "j/k fields | i edit | space toggle kind | F5 save | d delete | Esc discard"
	if f.mode == indexFormInsert {
		help = "insert mode | Esc normal mode"
	}
	return strings.Join(lines, "\n") + "\n" + statusStyle.Render(help)
}
