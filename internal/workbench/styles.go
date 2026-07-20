package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

const (
	colorCanvas  = "#10151f"
	colorPanel   = "#17202e"
	colorInk     = "#e6edf3"
	colorMuted   = "#8b9bb4"
	colorAccent  = "#55d6be"
	colorBorder  = "#324155"
	spaceCompact = 1
)

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorCanvas)).
			Background(lipgloss.Color(colorAccent)).
			Bold(true).
			Padding(0, spaceCompact)
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted)).
			Padding(0, spaceCompact)
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted)).
			Padding(0, spaceCompact)
	focusStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorAccent)).
			Background(lipgloss.Color(colorPanel)).
			Foreground(lipgloss.Color(colorInk)).
			Padding(0, spaceCompact)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorBorder)).
			Background(lipgloss.Color(colorPanel)).
			Foreground(lipgloss.Color(colorInk)).
			Padding(0, spaceCompact)
)

func newList(title string, filtering bool) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(lipgloss.Color(colorInk))
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(lipgloss.Color(colorMuted))
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color(colorAccent))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color(colorAccent))
	model := list.New([]list.Item{}, delegate, 0, 0)
	model.Title = title
	model.SetFilteringEnabled(filtering)
	model.SetShowPagination(false)
	model.SetShowHelp(false)
	model.KeyMap.Quit.SetEnabled(false)
	model.KeyMap.ForceQuit.SetEnabled(false)
	model.Styles.Title = headerStyle
	model.Styles.NoItems = statusStyle
	return model
}

func newResultsTable() table.Model {
	return table.New(
		table.WithColumns([]table.Column{{Title: "Results", Width: 1}}),
		table.WithWidth(1),
		table.WithHeight(2),
		table.WithStyles(table.Styles{
			Header:   headerStyle,
			Cell:     lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk)).Padding(0, spaceCompact),
			Selected: lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)),
		}),
	)
}

func tableColumns(viewportWidth int, titles []string) []table.Column {
	if len(titles) == 0 {
		titles = []string{"Results"}
	}

	contentBudget := max(viewportWidth-len(titles)*2*spaceCompact, len(titles))
	columnWidth, remainder := contentBudget/len(titles), contentBudget%len(titles)
	columns := make([]table.Column, len(titles))
	for index, title := range titles {
		width := columnWidth
		if index < remainder {
			width++
		}
		columns[index] = table.Column{Title: title, Width: width}
	}
	return columns
}

func paneStyle(focused bool) lipgloss.Style {
	if focused {
		return focusStyle
	}
	return panelStyle
}

func compactPane(content string, width, height int) string {
	return paneStyle(true).Width(width).MaxWidth(width).Height(height).MaxHeight(height).Render(content)
}

func (m *Model) layout(width, height int) {
	m.width, m.height = max(width, 0), max(height, 0)
	contentHeight := max(m.height-4, 0)
	m.compact = m.width < compactWidth || m.height < 24
	m.schemaWidth, m.editorWidth = m.width, m.width
	if !m.compact {
		m.schemaWidth = 30
		m.editorWidth = max(m.width-32, 0)
	}
	m.editorHeight = max(7, contentHeight*2/5)
	m.resultsHeight = max(contentHeight-m.editorHeight, 0)
	m.schema.SetSize(max(m.schemaWidth-2, 0), max(contentHeight-2, 0))
	m.picker.SetSize(max(m.width-2, 0), max(contentHeight-2, 0))
	connectionWidth := m.width
	if !m.compact {
		connectionWidth = m.editorWidth
	}
	m.connection.name.SetWidth(max(connectionWidth-16, 1))
	m.connection.target.SetWidth(max(connectionWidth-16, 1))
	m.connection.host.SetWidth(max(connectionWidth-16, 1))
	m.connection.port.SetWidth(max(connectionWidth-16, 1))
	m.connection.user.SetWidth(max(connectionWidth-16, 1))
	m.connection.pass.SetWidth(max(connectionWidth-16, 1))
	m.recent.SetSize(max(m.schemaWidth-2, 0), max(contentHeight-2, 0))
	m.editor.textarea.SetWidth(max(m.editorWidth-4, 1))
	m.editor.textarea.SetHeight(max(m.editorHeight-2, 1))
	m.results.SetWidth(max(m.editorWidth-4, 1))
	m.results.SetHeight(max(m.resultsHeight-2, 2))
	m.structure.SetWidth(max(m.editorWidth-4, 1))
	m.structure.SetHeight(max(contentHeight-4, 2))
	m.browse.SetWidth(max(m.editorWidth-4, 1))
	m.browse.SetHeight(max(contentHeight-4, 2))
	for _, resultTable := range []*table.Model{&m.results, &m.structure, &m.browse} {
		columns := resultTable.Columns()
		titles := make([]string, len(columns))
		for index, column := range columns {
			titles[index] = column.Title
		}
		resultTable.SetColumns(tableColumns(resultTable.Width(), titles))
	}
}

func (m Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	if m.height < 4 || m.width < 1 {
		view.SetContent(headerStyle.Render("BUBBLE WORKBENCH"))
		return view
	}
	content := m.contentView()
	view.SetContent(lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render("BUBBLE WORKBENCH"), content, footerStyle.Render(m.footer())))
	if m.State == stateReady && m.Focus == focusWorkspace && m.Tab == tabSQL {
		if cursor := m.editor.textarea.Cursor(); cursor != nil {
			cursor.Position.X += 2
			cursor.Position.Y += 2
			if !m.compact {
				cursor.Position.X += m.schemaWidth - 2
			}
			view.Cursor = cursor
		}
	}
	return view
}

func (m Model) contentView() string {
	switch m.State {
	case stateConnection:
		if m.compact {
			content := m.connectionView()
			if m.connection.focus == connectionFocusRecent {
				content = m.recent.View()
			}
			return compactPane(content, max(m.width-2, 0), max(m.height-4, 0))
		}
		left := paneStyle(m.connection.focus == connectionFocusRecent).Width(max(m.schemaWidth-2, 0)).Height(max(m.height-4, 0)).Render(m.recent.View())
		right := paneStyle(m.connection.focus != connectionFocusRecent).Width(max(m.editorWidth-2, 0)).Height(max(m.height-4, 0)).Render(m.connectionView())
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	case statePicking:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(m.picker.View())
	case stateOpening:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(statusStyle.Render("opening database"))
	case stateFailure:
		return paneStyle(true).Width(max(m.width-2, 0)).Height(max(m.height-4, 0)).Render(statusStyle.Render(m.Status + "\npress enter to return to the picker"))
	}
	if m.compact {
		width, height := max(1, m.width-2), max(1, m.height-4)
		switch m.Focus {
		case focusSchema:
			return compactPane(m.schema.View(), width, height)
		case focusWorkspace:
			return compactPane(m.workspaceView(), width, height)
		}
	}
	left := paneStyle(m.Focus == focusSchema).Width(max(m.schemaWidth-2, 0)).Height(max(m.height-4, 0)).Render(m.schema.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, left, m.rightView())
}

func (m Model) rightView() string {
	return paneStyle(m.Focus == focusWorkspace).Width(max(m.editorWidth-2, 0)).Height(max(m.height-4, 0)).Render(m.workspaceView())
}

func (m Model) workspaceView() string {
	tabs := []string{"Structure", "Browse", "SQL"}
	for index := range tabs {
		if workspaceTab(index) == m.Tab {
			tabs[index] = headerStyle.Render(tabs[index])
		} else {
			tabs[index] = statusStyle.Render(tabs[index])
		}
	}
	var content string
	switch m.Tab {
	case tabStructure:
		content = m.structure.View()
	case tabBrowse:
		content = m.browse.View()
	case tabSQL:
		editor := m.editor.textarea.View()
		results := m.results.View()
		content = lipgloss.JoinVertical(lipgloss.Left, editor, results)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tabs...), content)
}

func (m Model) footer() string {
	if m.State == stateConnection {
		return safeText(m.Status + " | 1 recent | 2 form | tab controls | a add | e edit | d delete | / filter | q quit")
	}
	if m.State == stateReady {
		return safeText(m.Status + " | 1 tables | 2 tabs | tab switch view | q quit")
	}
	return safeText(m.Status + " | q quit")
}

func readDirectory(dir string) tea.Cmd {
	return func() tea.Msg {
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return directoryReadMsg{err: err}
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return directoryReadMsg{dir: absolute, err: err}
		}
		entries, err := os.ReadDir(resolved)
		if err != nil {
			return directoryReadMsg{dir: resolved, err: err}
		}
		items := []pickerItem{{raw: ":memory:", title: "In-memory database", description: "temporary SQLite database"}}
		if parent := filepath.Dir(resolved); parent != resolved {
			items = append(items, pickerItem{raw: parent, title: "..", description: "parent directory"})
		}
		for _, entry := range entries {
			name := entry.Name()
			target, err := filepath.EvalSymlinks(filepath.Join(resolved, name))
			if err != nil {
				continue
			}
			info, err := os.Stat(target)
			if err != nil {
				continue
			}
			kind := "directory"
			if !info.IsDir() {
				if !info.Mode().IsRegular() || !databaseSuffix(name) {
					continue
				}
				kind = "database"
			}
			items = append(items, pickerItem{raw: target, title: safeText(name), description: kind})
		}
		return directoryReadMsg{dir: resolved, items: items}
	}
}

func selectPickerItem(raw string) tea.Cmd {
	return func() tea.Msg {
		if raw == ":memory:" {
			return pickerSelectionMsg{target: raw}
		}
		resolved, err := filepath.EvalSymlinks(raw)
		if err != nil {
			return pickerSelectionMsg{err: err}
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return pickerSelectionMsg{err: err}
		}
		return pickerSelectionMsg{target: resolved, dir: info.IsDir()}
	}
}

func resolveTarget(target string) (string, error) {
	if target == ":memory:" {
		return target, nil
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("database target is not a regular file")
	}
	return resolved, nil
}

func databaseSuffix(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".sqlite") || strings.HasSuffix(name, ".sqlite3")
}

func safeText(input string) string { return sharedsql.SanitizeDisplay(input) }
