package app

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/l3aro/perk-workbench/internal/chrome"
)

type sqlTextarea struct {
	input textarea.Model
	// styledLines cache: keyed by wrap width, invalidated whenever the
	// buffer value changes. Chroma tokenization + per-rune styling is the
	// dominant per-frame cost, and the value only changes on keystrokes.
	styledValue string
	styledCache map[int][]sqlVisualLine
}

// styledLines returns syntax-highlighted visual lines for value, reusing the
// cached result when neither the value nor the width changed since the last
// call. View() and completionCursorOffset() share this cache.
func (t *sqlTextarea) styledLines(value string, width int) []sqlVisualLine {
	if t.styledValue != value {
		t.styledValue = value
		t.styledCache = nil
	}
	if lines, ok := t.styledCache[width]; ok {
		return lines
	}
	lines := sqlStyledLines(value, width)
	if t.styledCache == nil {
		t.styledCache = make(map[int][]sqlVisualLine)
	}
	t.styledCache[width] = lines
	return lines
}

type styledRune struct {
	rune
	style lipgloss.Style
}

func newSQLTextarea(width, height int) *sqlTextarea {
	input := textarea.New()
	input.Prompt = ""
	input.Placeholder = "Enter a query…"
	input.ShowLineNumbers = false
	input.SetWidth(width)
	input.SetHeight(height)
	styles := input.Styles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk))
	styles.Blurred.Text = styles.Focused.Text
	styles.Focused.CursorLine = lipgloss.NewStyle()
	input.SetStyles(styles)
	return &sqlTextarea{input: input}
}

func (t *sqlTextarea) SetValue(value string) { t.input.SetValue(value) }
func (t sqlTextarea) Value() string          { return t.input.Value() }
func (t *sqlTextarea) SetWidth(width int)    { t.input.SetWidth(width) }
func (t *sqlTextarea) SetHeight(height int)  { t.input.SetHeight(height) }
func (t *sqlTextarea) Focus() tea.Cmd        { return t.input.Focus() }
func (t *sqlTextarea) Focused() bool         { return t.input.Focused() }
func (t *sqlTextarea) Blur()                 { t.input.Blur() }

func (t *sqlTextarea) Update(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	t.input, command = t.input.Update(message)
	return command
}

func (t sqlTextarea) View() string {
	if t.input.Value() == "" {
		return t.input.View()
	}

	lines := t.styledLines(t.input.Value(), max(t.input.Width(), 1))
	info := t.input.LineInfo()
	start := min(t.input.ScrollYOffset(), len(lines))
	end := min(start+t.input.Height(), len(lines))
	var view strings.Builder
	for index := start; index < end; index++ {
		line := lines[index]
		view.WriteString(sqlRenderLine(line.runes, line.hardLine == t.input.Line() && line.subLine == info.RowOffset, info.CharOffset, max(t.input.Width(), 1)))
		if index+1 < end {
			view.WriteByte('\n')
		}
	}
	for index := end - start; index < t.input.Height(); index++ {
		if view.Len() > 0 {
			view.WriteByte('\n')
		}
		view.WriteString(strings.Repeat(" ", max(t.input.Width(), 1)))
	}
	return view.String()
}

type sqlVisualLine struct {
	runes             []styledRune
	hardLine, subLine int
}

func sqlStyledLines(value string, width int) []sqlVisualLine {
	lexer := lexers.Get("sql")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := lexer.Tokenise(nil, value)
	if err != nil {
		return []sqlVisualLine{{runes: sqlPlainRunes(value)}}
	}
	lines := [][]styledRune{{}}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		style := chrome.SQLTokenStyle(token.Type, chrome.SQLStylePalette{
			Ink: colorInk, Accent: colorPrimary, Insert: colorModeInsert, Number: "#e3b341", Muted: colorMuted, Normal: colorModeNormal,
		})
		for _, character := range token.Value {
			if character == '\n' {
				lines = append(lines, nil)
				continue
			}
			lines[len(lines)-1] = append(lines[len(lines)-1], styledRune{rune: character, style: style})
		}
	}
	visual := make([]sqlVisualLine, 0, len(lines))
	for hardLine, line := range lines {
		for subLine, wrapped := range sqlWrapRunes(line, width) {
			visual = append(visual, sqlVisualLine{runes: wrapped, hardLine: hardLine, subLine: subLine})
		}
	}
	return visual
}

func sqlPlainRunes(value string) []styledRune {
	runes := make([]styledRune, 0, len(value))
	for _, character := range value {
		runes = append(runes, styledRune{rune: character, style: lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk))})
	}
	return runes
}

func sqlWrapRunes(line []styledRune, width int) [][]styledRune {
	if len(line) == 0 {
		return [][]styledRune{nil}
	}
	wrapped := [][]styledRune{{}}
	lineWidth := 0
	for _, character := range line {
		characterWidth := lipgloss.Width(string(character.rune))
		if lineWidth > 0 && lineWidth+characterWidth > width {
			wrapped = append(wrapped, nil)
			lineWidth = 0
		}
		wrapped[len(wrapped)-1] = append(wrapped[len(wrapped)-1], character)
		lineWidth += characterWidth
	}
	return wrapped
}

func sqlRenderLine(runes []styledRune, cursorLine bool, cursorOffset, width int) string {
	var view strings.Builder
	for index, character := range runes {
		if cursorLine && index == cursorOffset {
			view.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorCanvas)).Background(lipgloss.Color(colorInk)).Render(string(character.rune)))
			continue
		}
		view.WriteString(character.style.Render(string(character.rune)))
	}
	if cursorLine && cursorOffset >= len(runes) {
		view.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorCanvas)).Background(lipgloss.Color(colorInk)).Render(" "))
	}
	return lipgloss.NewStyle().Width(width).Render(view.String())
}
