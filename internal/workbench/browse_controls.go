package workbench

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type browseSort struct {
	column string
	desc   bool
}

type browseSettings struct {
	filter string
	sorts  []browseSort
	limit  int
}

type browseControls struct {
	form     *huh.Form
	settings *browseSettings
	limit    string
	width    int
}

func newBrowseControls(settings browseSettings, width int) *browseControls {
	controls := &browseControls{settings: &settings, width: max(width, 1)}
	controls.rebuild()
	return controls
}
func (m *Model) openBrowseControls() tea.Cmd {
	if len(m.structureColumns) == 0 {
		m.Status = "table columns are loading"
		return nil
	}
	m.browseControls = newBrowseControls(m.browseSettings, m.tableViewportWidth)
	m.formMode.mode = formModeInsert
	return m.browseControls.form.Init()
}

func (m *Model) cycleBrowseSort() tea.Cmd {
	if m.browseColumn < 0 || m.browseColumn >= len(m.browseResult.Columns) {
		return nil
	}
	column := m.browseResult.Columns[m.browseColumn]
	for index, sort := range m.browseSettings.sorts {
		if sort.column != column {
			continue
		}
		if !sort.desc {
			m.browseSettings.sorts[index].desc = true
		} else {
			m.browseSettings.sorts = append(m.browseSettings.sorts[:index], m.browseSettings.sorts[index+1:]...)
		}
		m.BrowsePage, m.browseLoading = 0, true
		m.browsePageTag++
		return m.loadBrowse()
	}
	m.browseSettings.sorts = append(m.browseSettings.sorts, browseSort{column: column})
	m.BrowsePage, m.browseLoading = 0, true
	m.browsePageTag++
	return m.loadBrowse()
}

func (c *browseControls) rebuild() {
	c.limit = strconv.Itoa(c.settings.pageSize())
	c.form = newForm(huh.NewGroup(
		newEditableInput(huh.NewInput().Key("filter").Title("Filter").Description("matches any column").Value(&c.settings.filter), &c.settings.filter),
		newEditableInput(huh.NewInput().Key("limit").Title("Rows").Value(&c.limit).Validate(validateBrowseLimit), &c.limit),
	)).WithShowHelp(c.width >= 40).WithWidth(c.width)
}

func validateBrowseLimit(value string) error {
	limit, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || limit < 1 || limit > sharedsql.MaxRows {
		return fmt.Errorf("enter a row limit from 1 to %d", sharedsql.MaxRows)
	}
	return nil
}

func (s browseSettings) pageSize() int {
	if s.limit < 1 {
		return browsePageSize
	}
	return s.limit
}

func (c *browseControls) update(message tea.Msg) tea.Cmd {
	model, command := c.form.Update(message)
	c.form = model.(*huh.Form)
	return command
}

func (c *browseControls) apply() error {
	if err := validateBrowseLimit(c.limit); err != nil {
		return err
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(c.limit))
	c.settings.limit = limit
	return nil
}

func (c *browseControls) setWidth(width int) {
	c.width = max(width, 1)
	c.form.WithWidth(c.width).WithShowHelp(c.width >= 40)
}

func (c browseControls) View() string { return c.form.View() }
