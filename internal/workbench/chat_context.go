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
	if len(m.schemaObjects) > 0 {
		context.WriteString("Schema:\n")
		for _, object := range m.schemaObjects {
			context.WriteString(object.Type)
			context.WriteString(" ")
			context.WriteString(object.Database)
			context.WriteString(".")
			context.WriteString(object.Name)
			context.WriteString("\n")
		}
	}
	if query := strings.TrimSpace(m.editor.value); query != "" {
		context.WriteString("Current SQL:\n")
		context.WriteString(query)
		context.WriteString("\n")
	}
	if len(m.results.Rows()) > 0 {
		context.WriteString("Visible results:\n")
		for _, row := range m.results.Rows() {
			context.WriteString(strings.Join(row, " | "))
			context.WriteString("\n")
		}
	}
	return truncateChatContext(context.String())
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
