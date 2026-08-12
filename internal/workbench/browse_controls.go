package workbench

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type browseSort struct {
	column string
	desc   bool
}

type browseSettings struct {
	filters []sharedsql.BrowseFilter
	sorts   []browseSort
	limit   int
}

func (m *Model) cycleBrowseSort() tea.Cmd {
	if m.layout.browseColumn < 0 || m.layout.browseColumn >= len(m.browse.result.Columns) {
		return nil
	}
	column := m.browse.result.Columns[m.layout.browseColumn]
	for index, sort := range m.browse.settings.sorts {
		if sort.column != column {
			continue
		}
		if !sort.desc {
			m.browse.settings.sorts[index].desc = true
		} else {
			m.browse.settings.sorts = append(m.browse.settings.sorts[:index], m.browse.settings.sorts[index+1:]...)
		}
		m.BrowsePage, m.browse.loading = 0, true
		m.browse.pageTag++
		return m.loadBrowse()
	}
	m.browse.settings.sorts = append(m.browse.settings.sorts, browseSort{column: column})
	m.BrowsePage, m.browse.loading = 0, true
	m.browse.pageTag++
	return m.loadBrowse()
}
func (m *Model) resetBrowseFilters() tea.Cmd {
	m.browse.settings.filters = nil
	m.BrowsePage, m.browse.loading = 0, true
	m.browse.pageTag++
	return m.loadBrowse()
}

// pagerBrowseCommand advances the browse page by delta, mirroring the
// n/p keypress dispatch exactly: the same debounce tick and staleness tag,
// so a pager-button click and a keypress are interchangeable. While a page
// is already loading the click is a no-op, like the keybindings.
func (m Model) pagerBrowseCommand(delta int) (Model, tea.Cmd) {
	if m.browse.loading {
		return m, nil
	}
	m.browse.pageTag++
	tag, table := m.browse.pageTag, m.SelectedTable
	return m, tea.Tick(browseDebounceDuration, func(time.Time) tea.Msg {
		return browseDebounceMsg{tag: tag, delta: delta, table: table}
	})
}

func validateBrowseLimit(value string) error {
	limit, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || limit < 1 || limit > sharedsql.MaxRows {
		return fmt.Errorf("enter a row limit from 1 to %d", sharedsql.MaxRows)
	}
	return nil
}

func (s browseSettings) pageSize(fallback int) int {
	if s.limit < 1 {
		return fallback
	}
	return s.limit
}
