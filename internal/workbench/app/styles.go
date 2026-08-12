package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

const (
	spaceCompact       = 1
	sqlEditorRows      = 4
	queryLogPaneHeight = 11
	chatPaneWidth      = 34
	iconPrimaryKey     = "\uf084" // nf-fa-key
	iconUnique         = "\uee40" // nf-fa-fingerprint
	iconRegular        = "\uf0cb" // nf-fa-list_ol
	iconSuccess        = "\uf00c" // nf-fa-check
	iconFailed         = "\uf00d" // nf-fa-times
	iconCanceled       = "\uf05e" // nf-fa-ban
)

type appTheme string

const (
	themeOcean      appTheme = "ocean"
	themeDracula    appTheme = "dracula"
	themeCatppuccin appTheme = "catppuccin"
	themeNord       appTheme = "nord"
	themeMonokai    appTheme = "monokai"
	themeSolarized  appTheme = "solarized"
)

func safeText(input string) string { return sharedsql.SanitizeDisplay(input) }

// safeMarkdown is like safeText but preserves newlines so markdown structure
// (paragraphs, tables, lists) survives glamour rendering.
func safeMarkdown(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		lines[i] = sharedsql.SanitizeDisplay(line)
	}
	return strings.Join(lines, "\n")
}

// cellText truncates a cell value to MaxRunes runes and appends "…" for the
// table display. The original full value remains in browseResult for editing.
func cellText(input string) string {
	return sharedsql.SanitizeDisplay(input, sharedsql.MaxRunes)
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

func databaseSuffix(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".sqlite") || strings.HasSuffix(name, ".sqlite3")
}
