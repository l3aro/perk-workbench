package chrome

import (
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
)

type SQLStylePalette struct {
	Ink, Accent, Insert, Number, Muted, Normal string
}

func SQLTokenStyle(token chroma.TokenType, palette SQLStylePalette) lipgloss.Style {
	switch {
	case token.InCategory(chroma.Keyword):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Accent)).Bold(true)
	case token.InCategory(chroma.LiteralString):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Insert))
	case token.InCategory(chroma.LiteralNumber):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Number))
	case token.InCategory(chroma.Comment):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Muted))
	case token.InCategory(chroma.Operator), token.InCategory(chroma.Punctuation):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Normal))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(palette.Ink))
	}
}
