// Package connection owns the connection-screen feature: the profile form
// (values, validation, TLS modes, DSN building), the action buttons, the
// recent-profiles list and filter, profile add/edit/delete operations, and
// the pane rendering. The root shell supplies the persistence path, screen
// geometry, keybindings, and modal form-mode routing; the component owns
// every connection-screen interaction that does not need a root overlay
// and requests database actions through typed events.
package connection

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/l3aro/perk-workbench/internal/chrome"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Driver and TLS mode types are the persisted profile types; the form
// aliases them so form values and profiles share one representation.
type Driver = profile.Driver
type MySQLTLS = profile.MySQLTLS
type PostgreSQLTLS = profile.PostgreSQLTLS

const (
	DriverSQLite     = profile.DriverSQLite
	DriverMySQL      = profile.DriverMySQL
	DriverPostgreSQL = profile.DriverPostgreSQL

	MySQLTLSVerify     = profile.MySQLTLSVerify
	MySQLTLSSkipVerify = profile.MySQLTLSSkipVerify
	MySQLTLSDisabled   = profile.MySQLTLSDisabled

	PostgreSQLTLSVerifyFull = profile.PostgreSQLTLSVerifyFull
	PostgreSQLTLSEncrypt    = profile.PostgreSQLTLSEncrypt
	PostgreSQLTLSDisabled   = profile.PostgreSQLTLSDisabled
)

// Pane focus values: the profiles list pane or the form pane.
const (
	FocusRecent = iota
	FocusForm
)

// Action values of the Test/Connect buttons.
const (
	ActionTest    = "Test connection"
	ActionConnect = "Connect"
)

// Model is the connection-screen feature component: the profile form with
// its values, the recent profiles list and filter, the persisted profile
// list and path, and the pane geometry. Root keeps the file picker, modal
// confirmations, context menus, and database opening.
type Model struct {
	Form         Form
	Recent       list.Model
	RecentFilter textinput.Model
	Profiles     []profile.Profile
	Path         string
}

// New builds the connection component with an empty recent list.
func New() Model {
	m := Model{
		Form:         NewForm(),
		Recent:       newRecentList(),
		RecentFilter: uikit.NewFilterInput(),
	}
	m.RecentFilter.Placeholder = "filter profiles"
	return m
}

// newRecentList builds the profiles list: no title, filter bar, pagination,
// or help (the pane renders its own filter input), with the shared delegate
// theme.
func newRecentList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(lipgloss.Color(uikit.ColorInk))
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(lipgloss.Color(uikit.ColorMuted))
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color(uikit.ColorPrimary))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color(uikit.ColorPrimary))
	model := list.New([]list.Item{}, delegate, 0, 0)
	model.Styles.Title = uikit.HeaderStyle
	model.Styles.NoItems = uikit.StatusStyle
	model.SetFilteringEnabled(false)
	model.SetShowPagination(false)
	model.SetShowHelp(false)
	model.DisableQuitKeybindings()
	model.KeyMap.Filter = key.NewBinding(key.WithDisabled())
	return model
}

// SetProfiles replaces the persisted profile list and refreshes the list
// items; it never writes through (the root owns the save boundary).
func (m *Model) SetProfiles(profiles []profile.Profile) {
	m.Profiles = profiles
	_ = m.Recent.SetItems(RecentListItems(profiles))
}

// Save persists the current profile list, best-effort like the original
// callers.
func (m Model) Save() {
	if m.Path != "" {
		_ = profile.Save(m.Path, m.Profiles)
	}
}

// Selected returns the profile under the recent list cursor, if any.
func (m Model) Selected() (profile.Profile, bool) {
	item, ok := m.Recent.SelectedItem().(RecentProfile)
	return item.Profile, ok
}

// ApplyFilter pushes the visible filter input's value into the profiles
// list, which filters its items and reports the committed state the status
// bar mirrors.
func (m *Model) ApplyFilter() {
	if query := strings.TrimSpace(m.RecentFilter.Value()); query != "" {
		m.Recent.SetFilterText(query)
		return
	}
	m.Recent.ResetFilter()
}

// SyncFilter clears the visible input when the list's own keymap dropped
// the filter (esc in list navigation).
func (m *Model) SyncFilter() {
	if !m.Recent.IsFiltered() && m.RecentFilter.Value() != "" {
		m.RecentFilter.SetValue("")
	}
}

// FocusFilter enters the filter input.
func (m *Model) FocusFilter() tea.Cmd { return m.RecentFilter.Focus() }

// BlurFilter exits the filter input, keeping the applied filter.
func (m *Model) BlurFilter() { m.RecentFilter.Blur() }

// ConnectionTarget returns the full opener target for the form's current
// values: server drivers gain their URL scheme prefix.
func (m Model) ConnectionTarget() string {
	return m.Form.ConnectionTarget()
}

// FormPaneView renders the form pane body: the form view plus the mode
// badge footer supplied by root (vim mode and modal form mode are root
// concerns).
func (m Model) FormPaneView(height int, modeBadge string) string {
	content := m.Form.View()
	return content + strings.Repeat("\n", max(height-strings.Count(content, "\n")-1, 1)) + modeBadge
}

// RecentPaneView renders the profiles list with its pane-local action
// hints; the layout reserves rows for the filter box (3) and the hint
// line (1). filterRow is the rendered filter box (root supplies it from
// the shared layout), width the pane content width.
func (m Model) RecentPaneView(filterRow string, width int) string {
	body := m.Recent.View()
	if filterRow != "" {
		body = filterRow + "\n" + body
	}
	return body + "\n" + chrome.PaneStatus("a add | e edit | d delete | / filter", "", max(width-6, 0))
}
