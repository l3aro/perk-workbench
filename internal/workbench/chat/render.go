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
	width := max(cm.Viewport.Width(), 1)
	if width != run.CachedWidth {
		run.resetRenderCache()
		run.resetStreamCache()
		run.CachedWidth = width
	}

	// Preserve only the longest equal front prefix. Tool rounds and history
	// loads can replace an arbitrary suffix, while a role-only change must
	// invalidate the corresponding block too.
	keep := min(len(run.BlockCache), len(run.Messages))
	for index := 0; index < keep; index++ {
		message := run.Messages[index]
		source := run.BlockCache[index].Source
		if source.Role != message.Role || source.Content != message.Content {
			keep = index
			break
		}
	}
	run.BlockCache = run.BlockCache[:keep]
	for index := keep; index < len(run.Messages); index++ {
		message := run.Messages[index]
		run.BlockCache = append(run.BlockCache, Block{
			Source: blockSource{Role: message.Role, Content: message.Content},
			Block:  cm.MessageBlock(message),
		})
	}

	blocks := make([]string, 0, len(run.Messages)+2)
	for _, cached := range run.BlockCache {
		blocks = append(blocks, cached.Block)
	}

	// Append streaming content as the last assistant message.
	if run.Loading {
		label := "\u2022 thinking..."
		if run.StreamBuffer != "" {
			label = "\u2022 streaming..."
		}
		blocks = append(blocks, uikit.ThinkingStyle.Render(label))
		if run.StreamBuffer != "" {
			blocks = append(blocks, cm.renderStream(run, width))
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, uikit.StatusStyle.Render("Ask about the selected database, query, or results."))
	}
	cm.Viewport.SetContent(strings.Join(blocks, "\n\n"))
	cm.Viewport.GotoBottom()
}

// renderStream incrementally renders completed paragraphs and the mutable
// tail. Any markdown state that crosses the paragraph boundary falls back to
// the existing whole-buffer renderer, preserving glamour/table semantics.
func (cm *Model) renderStream(run *Run, width int) string {
	cache := &run.stream
	if cache.Width != width {
		run.resetStreamCache()
		cache = &run.stream
		cache.Width = width
	}
	content := run.StreamBuffer
	boundary := strings.LastIndex(content, "\n\n")
	if boundary < 0 {
		if cache.TailSource != "" && !strings.HasPrefix(content, cache.TailSource) {
			run.resetStreamCache()
			cache = &run.stream
			cache.Width = width
		}
		cache.SourcePrefix = ""
		cache.RenderedPrefix = ""
		cache.TailSource = content
		cache.TailRendered = cm.StreamBlock(content)
		if !safeStreamMarkdown(content) {
			cache.TailRendered = cm.StreamBlock(content)
		}
		return cache.TailRendered
	}

	prefix, tail := content[:boundary], content[boundary+2:]
	previous := cache.SourcePrefix
	if cache.TailSource != "" {
		if previous != "" {
			previous += "\n\n"
		}
		previous += cache.TailSource
	}
	if previous != "" && !strings.HasPrefix(content, previous) {
		run.resetStreamCache()
		cache = &run.stream
		cache.Width = width
	}
	if !safeStreamMarkdown(prefix) || !safeStreamMarkdown(tail) {
		cache.SourcePrefix = ""
		cache.RenderedPrefix = ""
		cache.TailSource = content
		cache.TailRendered = cm.StreamBlock(content)
		return cache.TailRendered
	}

	renderedPrefix := cache.RenderedPrefix
	oldPrefix := cache.SourcePrefix
	if oldPrefix == "" || !strings.HasPrefix(prefix, oldPrefix) {
		renderedPrefix = renderStreamParagraphs(cm, prefix)
	} else if len(prefix) > len(oldPrefix) {
		extra := strings.TrimPrefix(prefix[len(oldPrefix):], "\n\n")
		if extra != "" {
			added := renderStreamParagraphs(cm, extra)
			if renderedPrefix != "" {
				renderedPrefix += "\n\n"
			}
			renderedPrefix += added
		}
	}
	cache.Width = width
	cache.SourcePrefix = prefix
	cache.RenderedPrefix = renderedPrefix
	cache.TailSource = tail
	cache.TailRendered = cm.StreamBlock(tail)
	if renderedPrefix == "" {
		return cache.TailRendered
	}
	if cache.TailRendered == "" {
		return renderedPrefix
	}
	return renderedPrefix + "\n\n" + cache.TailRendered
}

func renderStreamParagraphs(cm *Model, content string) string {
	if content == "" {
		return ""
	}
	paragraphs := strings.Split(content, "\n\n")
	rendered := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		rendered = append(rendered, cm.StreamBlock(paragraph))
	}
	return strings.Join(rendered, "\n\n")
}

func safeStreamMarkdown(content string) bool {
	if content == "" {
		return true
	}
	fenceCount := 0
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenceCount++
		}
	}
	if fenceCount%2 != 0 {
		return false
	}
	for _, paragraph := range strings.Split(content, "\n\n") {
		lines := strings.Split(paragraph, "\n")
		hasFence := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				hasFence = true
				break
			}
		}
		if hasFence {
			continue
		}
		tableLike := 0
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "|") {
				tableLike++
			}
		}
		if tableLike > 0 && !isGFMTable(lines) {
			return false
		}
	}
	return true
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
