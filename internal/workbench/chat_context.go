package workbench

import (
	"slices"
	"strings"

	"github.com/l3aro/perk-workbench/internal/ai"
)

func chatSQL(messages []ai.Message) string {
	for _, message := range slices.Backward(messages) {
		if message.Role != ai.RoleAssistant {
			continue
		}
		parts := strings.Split(message.Content, "```")
		for partIndex := len(parts) - 2; partIndex >= 1; partIndex -= 2 {
			block := strings.TrimSpace(parts[partIndex])
			firstLine, statement, found := strings.Cut(block, "\n")
			if found && (strings.EqualFold(firstLine, "sql") || strings.EqualFold(firstLine, "sqlite") || strings.EqualFold(firstLine, "mysql") || strings.EqualFold(firstLine, "postgresql")) {
				return strings.TrimSpace(statement)
			}
		}
	}
	return ""
}

func (m Model) chatContext() string {
	var context strings.Builder
	if m.databaseInfo.Product != "" {
		context.WriteString("Database: ")
		context.WriteString(m.databaseInfo.Product)
		if m.databaseInfo.Version != "" {
			context.WriteString(" ")
			context.WriteString(m.databaseInfo.Version)
		}
		context.WriteString("\n")
	}
	// Scan from newest to oldest for the first failed query.
	for _, entry := range m.queryLog.component.AllEntries() {
		if entry.Status == "failed" {
			context.WriteString("Last failed query:\n")
			context.WriteString(entry.Statement)
			context.WriteString("\nError:\n")
			context.WriteString(entry.Message)
			context.WriteString("\n")
			break
		}
	}
	if len(m.schema.objects) > 0 {
		context.WriteString("Schema:\n")
		for _, object := range m.schema.objects {
			context.WriteString(object.Type)
			context.WriteString(" ")
			context.WriteString(object.Database)
			context.WriteString(".")
			context.WriteString(object.Name)
			context.WriteString("\n")
		}
	}
	if query := strings.TrimSpace(m.queryLog.editor.value); query != "" {
		context.WriteString("Current SQL:\n")
		context.WriteString(query)
		context.WriteString("\n")
	}
	return truncateChatContext(context.String())
}

// chatResultsContext returns the visible results block for providers without
// tool support; tool-capable providers get get_visible_results instead.
func (m Model) chatResultsContext() string {
	if !m.chat.shareResults || len(m.queryLog.results.Rows()) == 0 {
		return ""
	}
	var context strings.Builder
	context.WriteString("Visible results:\n")
	for _, row := range m.queryLog.results.Rows() {
		context.WriteString(strings.Join(row, " | "))
		context.WriteString("\n")
	}
	return context.String()
}

func truncateChatTitle(prompt string) string {
	runes := []rune(prompt)
	if len(runes) > 60 {
		return string(runes[:60]) + "..."
	}
	return prompt
}

func truncateChatContext(value string) string {
	const limit = 12_000
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n[context truncated]"
}
