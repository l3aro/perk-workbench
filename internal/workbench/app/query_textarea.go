package app

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/l3aro/perk-workbench/internal/chrome"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type queryTextarea struct {
	input textarea.Model
	// lexer tokenizes the buffer for syntax highlighting; nil renders
	// plain unstyled text (blank or unknown lexer advertisement).
	lexer chroma.Lexer
	// styledLines cache: keyed by wrap width, invalidated whenever the
	// buffer value changes. Chroma tokenization + per-rune styling is the
	// dominant per-frame cost, and the value only changes on keystrokes.
	styledValue string
	styledCache map[int][]queryVisualLine
}

// styledLines returns syntax-highlighted visual lines for value, reusing the
// cached result when neither the value nor the width changed since the last
// call. View() and completionCursorOffset() share this cache.
func (t *queryTextarea) styledLines(value string, width int) []queryVisualLine {
	if t.styledValue != value {
		t.styledValue = value
		t.styledCache = nil
	}
	if lines, ok := t.styledCache[width]; ok {
		return lines
	}
	lines := queryStyledLines(value, width, t.lexer)
	if t.styledCache == nil {
		t.styledCache = make(map[int][]queryVisualLine)
	}
	t.styledCache[width] = lines
	return lines
}

type styledRune struct {
	rune
	style lipgloss.Style
}

// newQueryTextarea builds the query editor input for one query language:
// the placeholder comes from the language advertisement and the lexer is
// resolved once here — the per-frame rendering path only consults the
// cached visual lines.
func newQueryTextarea(width, height int, language sharedsql.QueryLanguage) *queryTextarea {
	input := textarea.New()
	input.Prompt = ""
	input.Placeholder = language.Placeholder
	input.ShowLineNumbers = false
	input.SetWidth(width)
	input.SetHeight(height)
	styles := input.Styles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk))
	styles.Blurred.Text = styles.Focused.Text
	styles.Focused.CursorLine = lipgloss.NewStyle()
	input.SetStyles(styles)
	return &queryTextarea{input: input, lexer: queryLexer(language)}
}

// queryLexer resolves a query language's lexer hint; a blank or unknown
// hint yields nil, which renders plain unstyled text — never SQL.
func queryLexer(language sharedsql.QueryLanguage) chroma.Lexer {
	if language.Lexer == "" {
		return nil
	}
	lexer := lexers.Get(language.Lexer)
	if lexer == nil {
		return nil
	}
	return lexer
}

func (t *queryTextarea) SetValue(value string) { t.input.SetValue(value) }
func (t queryTextarea) Value() string          { return t.input.Value() }
func (t *queryTextarea) SetWidth(width int)    { t.input.SetWidth(width) }
func (t *queryTextarea) SetHeight(height int)  { t.input.SetHeight(height) }
func (t *queryTextarea) Focus() tea.Cmd        { return t.input.Focus() }
func (t *queryTextarea) Focused() bool         { return t.input.Focused() }
func (t *queryTextarea) Blur()                 { t.input.Blur() }

func (t *queryTextarea) Update(message tea.Msg) tea.Cmd {
	var command tea.Cmd
	t.input, command = t.input.Update(message)
	return command
}

func (t queryTextarea) View() string {
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
		view.WriteString(queryRenderLine(line.runes, line.hardLine == t.input.Line() && line.subLine == info.RowOffset, info.CharOffset, max(t.input.Width(), 1)))
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

type queryVisualLine struct {
	runes             []styledRune
	hardLine, subLine int
}

// queryStyledLines tokenizes value with lexer and styles every rune. SQL
// keeps the SQL token palette; other known lexers render through the
// same small token-category palette. A nil lexer renders plain ink text.
func queryStyledLines(value string, width int, lexer chroma.Lexer) []queryVisualLine {
	var lines [][]styledRune
	palette := chrome.SQLStylePalette{
		Ink: colorInk, Accent: colorPrimary, Insert: colorModeInsert, Number: "#e3b341", Muted: colorMuted, Normal: colorModeNormal,
	}
	switch {
	case lexer == nil:
		lines = querySplitPlainLines(value)
	default:
		iterator, err := lexer.Tokenise(nil, value)
		if err != nil {
			lines = querySplitPlainLines(value)
			break
		}
		lines = [][]styledRune{{}}
		for token := iterator(); token != chroma.EOF; token = iterator() {
			style := chrome.SQLTokenStyle(token.Type, palette)
			for _, character := range token.Value {
				if character == '\n' {
					lines = append(lines, nil)
					continue
				}
				lines[len(lines)-1] = append(lines[len(lines)-1], styledRune{rune: character, style: style})
			}
		}
	}
	visual := make([]queryVisualLine, 0, len(lines))
	for hardLine, line := range lines {
		for subLine, wrapped := range queryWrapRunes(line, width) {
			visual = append(visual, queryVisualLine{runes: wrapped, hardLine: hardLine, subLine: subLine})
		}
	}
	return visual
}

// querySplitPlainLines styles every rune with plain ink, one slice per
// hard line, so the plain path keeps the same visual-line structure as
// the token path (the cursor code relies on hardLine/subLine).
func querySplitPlainLines(value string) [][]styledRune {
	lines := [][]styledRune{{}}
	for _, character := range value {
		if character == '\n' {
			lines = append(lines, nil)
			continue
		}
		lines[len(lines)-1] = append(lines[len(lines)-1], styledRune{rune: character, style: lipgloss.NewStyle().Foreground(lipgloss.Color(colorInk))})
	}
	return lines
}

func queryWrapRunes(line []styledRune, width int) [][]styledRune {
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

func queryRenderLine(runes []styledRune, cursorLine bool, cursorOffset, width int) string {
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
