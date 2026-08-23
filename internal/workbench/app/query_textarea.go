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
	"github.com/mattn/go-runewidth"
)

type queryTextarea struct {
	input textarea.Model
	// lexer tokenizes the buffer for syntax highlighting; nil renders
	// plain unstyled text (blank or unknown lexer advertisement).
	lexer chroma.Lexer
	// lexerID is the normalized advertised lexer identity. It is part of both
	// cache keys so a language switch cannot reuse another language's tokens.
	lexerID string

	lexical     queryLexicalCache
	visual      map[queryVisualCacheKey][]queryVisualLine
	visualTheme uint64
}

type queryLexicalCache struct {
	value string
	lexer string
	lines [][]queryLexicalRun
	valid bool
}

type queryVisualCacheKey struct {
	value string
	lexer string
	theme uint64
	width int
}

type queryStyleCategory uint8

const (
	queryStylePlain queryStyleCategory = iota
	queryStyleKeyword
	queryStyleString
	queryStyleNumber
	queryStyleComment
	queryStyleNormal
)

type queryLexicalRun struct {
	text     string
	category queryStyleCategory
}

type queryRun struct {
	text     string
	category queryStyleCategory
	style    lipgloss.Style
}

// styledLines returns syntax-highlighted visual lines for value, reusing the
// persistent lexical and width/theme-specific rendered caches. The returned
// lines never contain cursor decoration; View applies that only to the one
// visible cursor row.
func (t *queryTextarea) styledLines(value string, width int) []queryVisualLine {
	width = max(width, 1)
	if t.visualTheme != themeRevision {
		t.visual = nil
		t.visualTheme = themeRevision
	}
	lexerID := t.lexerID
	if !t.lexical.valid || t.lexical.value != value || t.lexical.lexer != lexerID {
		t.lexical = queryLexicalCache{
			value: value,
			lexer: lexerID,
			lines: queryTokenizeLines(value, t.lexer),
			valid: true,
		}
		// A changed value or lexer makes old visual entries unusable and keeps
		// the cache bounded to the live editor value.
		t.visual = nil
	}

	key := queryVisualCacheKey{value: value, lexer: lexerID, theme: themeRevision, width: width}
	if lines, ok := t.visual[key]; ok {
		return lines
	}
	lines := queryVisualLines(t.lexical.lines, width)
	if t.visual == nil {
		t.visual = make(map[queryVisualCacheKey][]queryVisualLine)
	}
	t.visual[key] = lines
	return lines
}

// invalidateValue drops rendered data after a textarea mutation. The next
// styledLines call also checks the exact value, covering callers that mutate
// the embedded bubbles model indirectly.
func (t *queryTextarea) invalidateValue() {
	t.lexical = queryLexicalCache{}
	t.visual = nil
}

type styledRune struct {
	rune
	style    lipgloss.Style
	category queryStyleCategory
}

// newQueryTextarea builds the query editor input for one query language:
// the placeholder comes from the language advertisement and the lexer is
// resolved once here — the per-frame rendering path only consults caches.
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
	return &queryTextarea{
		input:   input,
		lexer:   queryLexer(language),
		lexerID: normalizedQueryLexer(language.Lexer),
	}
}

// queryLexer resolves a query language's lexer hint; a blank or unknown
// hint yields nil, which renders plain unstyled text — never SQL.
func queryLexer(language sharedsql.QueryLanguage) chroma.Lexer {
	name := normalizedQueryLexer(language.Lexer)
	if name == "" {
		return nil
	}
	return lexers.Get(name)
}

func normalizedQueryLexer(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (t *queryTextarea) SetValue(value string) {
	before := t.input.Value()
	t.input.SetValue(value)
	if before != value {
		t.invalidateValue()
	}
}

func (t queryTextarea) Value() string { return t.input.Value() }
func (t *queryTextarea) SetWidth(width int) {
	before := t.input.Width()
	t.input.SetWidth(width)
	if before != width {
		t.visual = nil
	}
}
func (t *queryTextarea) SetHeight(height int) { t.input.SetHeight(height) }
func (t *queryTextarea) Focus() tea.Cmd       { return t.input.Focus() }
func (t *queryTextarea) Focused() bool        { return t.input.Focused() }
func (t *queryTextarea) Blur()                { t.input.Blur() }

func (t *queryTextarea) Update(message tea.Msg) tea.Cmd {
	before := t.input.Value()
	var command tea.Cmd
	t.input, command = t.input.Update(message)
	if t.input.Value() != before {
		t.invalidateValue()
	}
	return command
}

func (t *queryTextarea) View() string {
	if t.input.Value() == "" {
		return t.input.View()
	}

	width := max(t.input.Width(), 1)
	lines := t.styledLines(t.input.Value(), width)
	info := t.input.LineInfo()
	start := min(t.input.ScrollYOffset(), len(lines))
	end := min(start+t.input.Height(), len(lines))
	var view strings.Builder
	for index := start; index < end; index++ {
		line := lines[index]
		cursorLine := line.hardLine == t.input.Line() && line.subLine == info.RowOffset
		if cursorLine {
			view.WriteString(queryRenderCursorLine(line, info.CharOffset, width))
		} else {
			view.WriteString(line.rendered)
		}
		if index+1 < end {
			view.WriteByte('\n')
		}
	}
	for index := end - start; index < t.input.Height(); index++ {
		if view.Len() > 0 {
			view.WriteByte('\n')
		}
		view.WriteString(strings.Repeat(" ", width))
	}
	return view.String()
}

type queryVisualLine struct {
	runes             []styledRune
	runs              []queryRun
	rendered          string
	hardLine, subLine int
}

// queryTokenizeLines tokenizes the complete value in one pass. Keeping the
// whole input intact is important: SQL strings and comments may cross hard
// line boundaries and Chroma carries that lexical state between them.
func queryTokenizeLines(value string, lexer chroma.Lexer) [][]queryLexicalRun {
	if lexer == nil {
		return queryPlainLexicalLines(value)
	}

	iterator, err := lexer.Tokenise(nil, value)
	if err != nil {
		return queryPlainLexicalLines(value)
	}
	lines := [][]queryLexicalRun{{}}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		category := queryTokenCategory(token.Type)
		for _, character := range token.Value {
			if character == '\n' {
				lines = append(lines, nil)
				continue
			}
			last := &lines[len(lines)-1]
			if len(*last) > 0 && (*last)[len(*last)-1].category == category {
				(*last)[len(*last)-1].text += string(character)
				continue
			}
			*last = append(*last, queryLexicalRun{text: string(character), category: category})
		}
	}
	return lines
}

func queryTokenCategory(token chroma.TokenType) queryStyleCategory {
	switch {
	case token.InCategory(chroma.Keyword):
		return queryStyleKeyword
	case token.InCategory(chroma.LiteralString):
		return queryStyleString
	case token.InCategory(chroma.LiteralNumber):
		return queryStyleNumber
	case token.InCategory(chroma.Comment):
		return queryStyleComment
	case token.InCategory(chroma.Operator), token.InCategory(chroma.Punctuation):
		return queryStyleNormal
	default:
		return queryStylePlain
	}
}

func queryPlainLexicalLines(value string) [][]queryLexicalRun {
	lines := [][]queryLexicalRun{{}}
	for _, character := range value {
		if character == '\n' {
			lines = append(lines, nil)
			continue
		}
		last := &lines[len(lines)-1]
		if len(*last) > 0 && (*last)[len(*last)-1].category == queryStylePlain {
			(*last)[len(*last)-1].text += string(character)
			continue
		}
		*last = append(*last, queryLexicalRun{text: string(character), category: queryStylePlain})
	}
	return lines
}

func queryVisualLines(lines [][]queryLexicalRun, width int) []queryVisualLine {
	visual := make([]queryVisualLine, 0, len(lines))
	for hardLine, line := range lines {
		styled := queryStyledRunes(line)
		for subLine, wrapped := range queryWrapRunes(styled, width) {
			runs := queryGroupStyledRunes(wrapped)
			visual = append(visual, queryVisualLine{
				runes:    wrapped,
				runs:     runs,
				rendered: queryRenderNormalRuns(runs, width),
				hardLine: hardLine,
				subLine:  subLine,
			})
		}
	}
	return visual
}

func queryStyledRunes(line []queryLexicalRun) []styledRune {
	palette := chrome.SQLStylePalette{
		Ink: colorInk, Accent: colorPrimary, Insert: colorModeInsert, Number: "#e3b341", Muted: colorMuted, Normal: colorModeNormal,
	}
	styled := make([]styledRune, 0)
	for _, run := range line {
		style := queryCategoryStyle(run.category, palette)
		for _, character := range run.text {
			styled = append(styled, styledRune{rune: character, style: style, category: run.category})
		}
	}
	return styled
}

func queryCategoryStyle(category queryStyleCategory, palette chrome.SQLStylePalette) lipgloss.Style {
	switch category {
	case queryStyleKeyword:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Accent)).Bold(true)
	case queryStyleString:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Insert))
	case queryStyleNumber:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Number))
	case queryStyleComment:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Muted))
	case queryStyleNormal:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Normal))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Ink))
	}
}

// queryStyledLines is retained as the small helper used by focused callers;
// queryTextarea uses the persistent two-level cache above.
func queryStyledLines(value string, width int, lexer chroma.Lexer) []queryVisualLine {
	return queryVisualLines(queryTokenizeLines(value, lexer), max(width, 1))
}

// querySplitPlainLines styles every rune with plain ink, one slice per hard
// line, so the plain path keeps the same visual-line structure as the token
// path (the cursor code relies on hardLine/subLine).
func querySplitPlainLines(value string) [][]styledRune {
	lines := queryPlainLexicalLines(value)
	result := make([][]styledRune, len(lines))
	for index, line := range lines {
		result[index] = queryStyledRunes(line)
	}
	return result
}

func queryWrapRunes(line []styledRune, width int) [][]styledRune {
	width = max(width, 1)
	if len(line) == 0 {
		return [][]styledRune{nil}
	}
	wrapped := [][]styledRune{{}}
	lineWidth := 0
	for _, character := range line {
		characterWidth := queryRuneWidth(character.rune)
		if lineWidth > 0 && lineWidth+characterWidth > width {
			wrapped = append(wrapped, nil)
			lineWidth = 0
		}
		wrapped[len(wrapped)-1] = append(wrapped[len(wrapped)-1], character)
		lineWidth += characterWidth
	}
	return wrapped
}

func queryRuneWidth(character rune) int {
	return runewidth.RuneWidth(character)
}

func queryGroupStyledRunes(runes []styledRune) []queryRun {
	if len(runes) == 0 {
		return nil
	}
	runs := make([]queryRun, 0)
	for _, character := range runes {
		if len(runs) > 0 && runs[len(runs)-1].category == character.category {
			runs[len(runs)-1].text += string(character.rune)
			continue
		}
		runs = append(runs, queryRun{text: string(character.rune), category: character.category, style: character.style})
	}
	return runs
}

func queryRenderNormalRuns(runs []queryRun, width int) string {
	var view strings.Builder
	for _, run := range runs {
		view.WriteString(run.style.Render(run.text))
	}
	return lipgloss.NewStyle().Width(width).Render(view.String())
}

func queryCursorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colorCanvas)).Background(lipgloss.Color(colorInk))
}

func queryRenderCursorLine(line queryVisualLine, cursorOffset, width int) string {
	if cursorOffset < 0 {
		cursorOffset = 0
	}
	var view strings.Builder
	remaining := cursorOffset
	cursorRendered := false
	for _, run := range line.runs {
		runes := []rune(run.text)
		if !cursorRendered && remaining <= len(runes) {
			if remaining > 0 {
				view.WriteString(run.style.Render(string(runes[:remaining])))
			}
			if remaining < len(runes) {
				view.WriteString(queryCursorStyle().Render(string(runes[remaining])))
				view.WriteString(run.style.Render(string(runes[remaining+1:])))
				cursorRendered = true
				continue
			}
			cursorRendered = true
			view.WriteString(queryCursorStyle().Render(" "))
			continue
		}
		view.WriteString(run.style.Render(run.text))
		remaining -= len(runes)
	}
	if !cursorRendered {
		view.WriteString(queryCursorStyle().Render(" "))
	}
	return lipgloss.NewStyle().Width(width).Render(view.String())
}

func queryRenderLine(runes []styledRune, cursorLine bool, cursorOffset, width int) string {
	runs := queryGroupStyledRunes(runes)
	if cursorLine {
		return queryRenderCursorLine(queryVisualLine{runes: runes, runs: runs}, cursorOffset, max(width, 1))
	}
	return queryRenderNormalRuns(runs, max(width, 1))
}
