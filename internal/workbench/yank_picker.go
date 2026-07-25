package workbench

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk/internal/chrome"
)

type yankPicker struct {
	form      *huh.Form
	selection string
	entry     queryLogEntry
}

type yankOption string

const (
	yankStatement yankOption = "Copy query statement"
	yankMessage   yankOption = "Copy result message"
	yankDuration  yankOption = "Copy time spent"
)

func newYankPicker(entry queryLogEntry, width int) *yankPicker {
	choices := []huh.Option[string]{
		huh.NewOption(string(yankStatement), string(yankStatement)),
		huh.NewOption(string(yankMessage), string(yankMessage)),
		huh.NewOption(string(yankDuration), string(yankDuration)),
	}
	picker := &yankPicker{entry: entry, selection: string(yankStatement)}
	picker.form = newForm(huh.NewGroup(
		huh.NewSelect[string]().Key("yank").Title("Copy to clipboard").Options(choices...).Value(&picker.selection),
	)).WithShowHelp(width >= 40).WithWidth(max(width, 1))
	return picker
}

func (p *yankPicker) Update(message tea.Msg) tea.Cmd {
	form, command := p.form.Update(message)
	p.form = form.(*huh.Form)
	return command
}

func (p *yankPicker) completed() bool { return p.form.State == huh.StateCompleted }

func (p *yankPicker) value() string {
	switch yankOption(p.selection) {
	case yankStatement:
		return p.entry.statement
	case yankMessage:
		return chrome.DetailValue(p.entry.message)
	case yankDuration:
		return p.entry.duration.Round(time.Microsecond).String()
	}
	return ""
}
