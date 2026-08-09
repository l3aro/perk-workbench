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
func (m *Model) resetBrowseFilters() tea.Cmd {
	m.browseSettings.filters = nil
	m.BrowsePage, m.browseLoading = 0, true
	m.browsePageTag++
	return m.loadBrowse()
}

// pagerBrowseCommand advances the browse page by delta, mirroring the
// n/p keypress dispatch exactly: the same debounce tick and staleness tag,
// so a pager-button click and a keypress are interchangeable. While a page
// is already loading the click is a no-op, like the keybindings.
func (m Model) pagerBrowseCommand(delta int) (Model, tea.Cmd) {
	if m.browseLoading {
		return m, nil
	}
	m.browsePageTag++
	tag, table := m.browsePageTag, m.SelectedTable
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
