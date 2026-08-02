package workbench

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// chatCommands are the slash commands offered as chat input completions.
func chatCommands() []CompletionItem {
	return []CompletionItem{
		{Label: "/new", InsertText: "/new", Kind: KindCommand},
	}
}

// updateChatCompletion shows slash-command suggestions while the chat input
// starts with "/", and clears them otherwise.
func (m *Model) updateChatCompletion() {
	value := m.chat.input.Value()
	if !strings.HasPrefix(value, "/") {
		m.chat.completion = completion{}
		return
	}
	if len(m.chat.completion.items) == 0 {
		m.chat.completion = newCompletion(chatCommands())
	}
	m.chat.completion.filter(value)
}

// acceptChatCompletion replaces the chat input with the selected suggestion.
// textarea.SetValue resets and re-inserts, leaving the cursor at the end.
func (m *Model) acceptChatCompletion() {
	item := m.chat.completion.accept()
	m.chat.completion = completion{}
	if item.InsertText == "" {
		return
	}
	m.chat.input.SetValue(item.InsertText)
}

// chatCompletionOverlay renders the slash-command dropdown below the chat input.
func (m Model) chatCompletionOverlay() string {
	matches := m.chat.completion.matches
	if len(matches) == 0 {
		return ""
	}
	const viewSize = 5
	selected := m.chat.completion.selected
	offset := max(selected-viewSize/2, 0)
	offset = min(offset, max(len(matches)-viewSize, 0))
	visible := matches[offset : offset+min(viewSize, len(matches)-offset)]

	items := make([]string, 0, len(visible))
	for i, match := range visible {
		label := match.Label
		if offset+i == selected {
			label = "› " + label
		} else {
			label = "  " + label
		}
		item := completionItemStyle.Render(label)
		item += " " + completionDetailStyle.Render(match.Kind.String())
		items = append(items, item)
	}
	width := max(m.chatWidth-6, 1)
	if m.compact {
		width = max(m.width-6, 1)
	}
	return completionBoxStyle.
		MaxWidth(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, items...))
}
