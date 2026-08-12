package chat

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// newTermRenderer builds the glamour renderer for the chat viewport width.
func newTermRenderer(width int) (*glamour.TermRenderer, error) {
	return glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
}

// RefreshView re-renders the visible conversation into the viewport.
func (cm *Model) RefreshView() {
	run := cm.ActiveRun()
	width := cm.Viewport.Width()
	if width != run.CachedWidth {
		run.CachedWidth = width
		run.BlockCache = nil
		run.StreamBlock = ""
		run.StreamSource = ""
	}
	// Keep the cached prefix whose content still matches its message;
	// tool-round boundaries replace the tail of run.Messages wholesale.
	keep := min(len(run.BlockCache), len(run.Messages))
	for keep > 0 && run.BlockCache[keep-1].Content != run.Messages[keep-1].Content {
		keep--
	}
	run.BlockCache = run.BlockCache[:keep]

	blocks := make([]string, 0, len(run.Messages)+2)
	for index := keep; index < len(run.Messages); index++ {
		message := run.Messages[index]
		block := cm.MessageBlock(message)
		run.BlockCache = append(run.BlockCache, Block{Content: message.Content, Block: block})
	}
	for _, cached := range run.BlockCache {
		blocks = append(blocks, cached.Block)
	}

	// Append streaming content as the last assistant message.
	if run.Loading {
		// Adaptive label: "thinking..." before content, "streaming..."
		// during.
		label := "\u2022 thinking..."
		if run.StreamBuffer != "" {
			label = "\u2022 streaming..."
		}
		blocks = append(blocks, uikit.ThinkingStyle.Render(label))

		if run.StreamBuffer != "" {
			// Re-render only when the buffer changed; unchanged buffers
			// (spinner ticks, tool phases) reuse the cached block.
			block := run.StreamBlock
			if run.StreamSource != run.StreamBuffer {
				block = cm.StreamBlock(run.StreamBuffer)
				run.StreamBlock, run.StreamSource = block, run.StreamBuffer
			}
			blocks = append(blocks, block)
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, uikit.StatusStyle.Render("Ask about the selected database, query, or results."))
	}
	cm.Viewport.SetContent(strings.Join(blocks, "\n\n"))
	cm.Viewport.GotoBottom()
}

// MessageBlock renders one message into its viewport block.
func (cm *Model) MessageBlock(message ai.Message) string {
	var content string
	if message.Role == ai.RoleAssistant {
		content = safeMarkdown(message.Content)
	} else {
		content = uikit.SafeText(message.Content)
	}
	if message.Role == ai.RoleAssistant && cm.glamour != nil {
		content = cm.RenderContent(content)
	}
	if message.Role == ai.RoleUser {
		contentWidth := max(cm.Viewport.Width()-2, 1)
		lines := strings.Split(lipgloss.Wrap(content, contentWidth, ""), "\n")
		for i, line := range lines {
			line = " " + line + strings.Repeat("\u00a0", max(contentWidth-lipgloss.Width(line), 0))
			lines[i] = uikit.UserMessageAccentStyle.Render("\u258c") + uikit.UserMessageStyle.Render(line)
		}
		return strings.Join(lines, "\n")
	}
	return content
}

// StreamBlock renders the streaming tail of the active assistant turn.
func (cm *Model) StreamBlock(content string) string {
	content = safeMarkdown(content)
	if cm.glamour != nil {
		content = cm.RenderContent(content)
	}
	return content
}

// safeMarkdown preserves newlines through sanitization so markdown
// structure (paragraphs, tables, lists) survives glamour rendering.
func safeMarkdown(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		lines[i] = uikit.SafeText(line)
	}
	return strings.Join(lines, "\n")
}

func (cm *Model) initGlamour(width int) {
	if width < 1 {
		width = 80
	}
	r, err := newTermRenderer(width)
	if err != nil {
		cm.glamour = nil
		return
	}
	cm.glamour = r
}

// RenderContent renders assistant message content. Non-table markdown goes
// through glamour; GFM table blocks are rendered with lipgloss/v2/table
// for proper column alignment within the chat viewport width.
func (cm *Model) RenderContent(content string) string {
	if cm.glamour == nil || !strings.Contains(content, "\n|") && !strings.HasPrefix(content, "|") {
		// No table likely — use glamour directly.
		if cm.glamour != nil {
			if rendered, err := cm.glamour.Render(content); err == nil {
				return strings.TrimRight(rendered, "\n")
			}
		}
		return content
	}

	// Split by blank lines to find table paragraphs.
	paragraphs := strings.Split(content, "\n\n")
	width := cm.Viewport.Width()
	if width < 1 {
		width = 1
	}
	var out strings.Builder

	for i, para := range paragraphs {
		if i > 0 {
			out.WriteString("\n\n")
		}

		lines := strings.Split(para, "\n")
		if isGFMTable(lines) {
			out.WriteString(renderTableLines(lines, width))
		} else if cm.glamour != nil {
			if rendered, err := cm.glamour.Render(para); err == nil {
				out.WriteString(strings.TrimRight(rendered, "\n"))
			} else {
				out.WriteString(para)
			}
		} else {
			out.WriteString(para)
		}
	}
	return out.String()
}

// isGFMTable checks whether a set of lines forms a GFM table block.
func isGFMTable(lines []string) bool {
	if len(lines) < 3 {
		return false
	}
	// All lines must be non-empty and start with '|' (possibly after
	// whitespace).
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed == "" || trimmed[0] != '|' {
			return false
		}
	}
	// Second line must be a separator: |[-: ]+|
	sep := strings.TrimSpace(lines[1])
	withoutPipes := strings.ReplaceAll(sep, "|", "")
	if len(withoutPipes) == 0 {
		return false
	}
	for _, r := range withoutPipes {
		if r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return true
}

// renderTableLines renders a parsed GFM table with lipgloss/v2/table.
func renderTableLines(lines []string, width int) string {
	headCells := parseTableRow(lines[0])
	tbl := table.New().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(uikit.ColorSecondary)).Bold(true)
			}
			return lipgloss.NewStyle()
		})

	tbl.Headers(headCells...)
	for _, line := range lines[2:] {
		cells := parseTableRow(line)
		tbl.Row(cells...)
	}

	rendered := tbl.Render()
	return rendered
}

// parseTableRow splits a GFM table row line into cells, stripping outer
// pipes and trimming whitespace.
func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	// Remove leading and trailing pipe.
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = line[:len(line)-1]
	}
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}
