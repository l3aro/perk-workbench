package chat

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/l3aro/perk-workbench/internal/ai"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// DatabaseTools returns tool definitions for AI assistant database
// introspection and writes. Read-only connections expose only read tools.
func (cm Model) DatabaseTools(ctx Context) []ai.ToolDefinition {
	if cm.Executor == nil {
		return nil
	}

	product := ctx.Database.Product
	tools := []ai.ToolDefinition{
		{
			Name: "sql_read",
			Description: "Execute a read-only SQL query against the " + product + " database and return tabular results. " +
				"Use this to explore schema, count rows, sample data, list tables, check server variables, and answer questions about the database content. " +
				"Only SELECT, EXPLAIN, SHOW, PRAGMA, and DESCRIBE-type queries are allowed; mutations are rejected. " +
				"If the user asks to INSERT, UPDATE, DELETE, CREATE, ALTER, or DROP, do NOT call this tool — " +
				"instead use the sql_write tool for mutations. " +
				"Returns up to 500 rows. Each cell is truncated to 40 characters.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The SQL query to execute (read-only)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_connection_info",
			Description: "Get information about the current database connection: product name, version, current user, current database, connection ID, and host details. No arguments needed.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}

	if cm.ShareResults {
		tools = append(tools, ai.ToolDefinition{
			Name: "get_visible_results",
			Description: "Return the rows currently displayed in the SQL results table, with column headers. " +
				"Use this when the user asks about the current query result. No arguments needed.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		})
	}

	if !cm.ReadOnly {
		tools = append(tools, ai.ToolDefinition{
			Name: "sql_write",
			Description: "Execute exactly one SQL write/DDL statement against the " + product + " database. " +
				"Use this for INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, and other mutations. " +
				"The statement is executed immediately and requires your confirmation before running. " +
				"Read-only queries should use sql_read instead. " +
				"Each call executes exactly one statement; batch multiple statements into separate calls.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The SQL statement to execute",
					},
				},
				"required": []string{"query"},
			},
		})
	}

	return tools
}

// ExecuteTool runs one read-only tool call and returns the result.
func (cm Model) ExecuteTool(ctx context.Context, call ai.ToolCall, snapshot Context) ai.ToolResult {
	result := ai.ToolResult{CallID: call.ID, Name: call.Name}

	switch call.Name {
	case "sql_read":
		query, _ := call.Input["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			result.Error = "query argument is required"
			return result
		}
		res, err := cm.Executor.ExecuteReadOnly(ctx, query)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Content = FormatResult(res)

	case "get_connection_info":
		content, err := cm.GatherConnectionInfo(ctx, snapshot)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Content = content

	case "get_visible_results":
		columns := make([]string, len(snapshot.Results.Columns))
		copy(columns, snapshot.Results.Columns)
		result.Content = FormatResult(sharedsql.Result{Columns: columns, Rows: snapshot.Results.Rows})

	default:
		result.Error = fmt.Sprintf("unknown tool: %s", call.Name)
	}
	return result
}

// FormatResult renders a query result as the tabular text tools and write
// results carry.
func FormatResult(res sharedsql.Result) string {
	if len(res.Columns) == 0 && len(res.Rows) == 0 {
		if res.RowsAffected > 0 {
			return fmt.Sprintf("Query OK, %d rows affected", res.RowsAffected)
		}
		return "(no results)"
	}

	const capRunes = 8000
	var b strings.Builder

	// Column header
	for i, col := range res.Columns {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(col)
	}
	b.WriteString("\n")

	budget := capRunes - utf8.RuneCountInString(b.String())
	rowBudget := sharedsql.MaxRows
	truncated := false
	count := 0

	for _, row := range res.Rows {
		if count >= rowBudget {
			truncated = true
			break
		}

		// Build the row in a temp buffer to check size before committing.
		var rowBuf strings.Builder
		for i, cell := range row {
			if i > 0 {
				rowBuf.WriteString(" | ")
			}
			if cell != nil {
				cr := []rune(*cell)
				if len(cr) > sharedsql.MaxRunes {
					rowBuf.WriteString(string(cr[:sharedsql.MaxRunes]) + "…")
				} else {
					rowBuf.WriteString(*cell)
				}
			} else {
				rowBuf.WriteString("NULL")
			}
		}
		rowBuf.WriteString("\n")

		rowRunes := utf8.RuneCountInString(rowBuf.String())
		if budget-rowRunes < 0 {
			truncated = true
			break
		}
		b.WriteString(rowBuf.String())
		budget -= rowRunes
		count++
	}

	if truncated || res.Truncated {
		b.WriteString("(results truncated)\n")
	}
	return b.String()
}

// GatherConnectionInfo builds the connection info text from the snapshot
// and one read-only query per product.
func (cm Model) GatherConnectionInfo(ctx context.Context, snapshot Context) (string, error) {
	var b strings.Builder
	info := snapshot.Database
	b.WriteString(fmt.Sprintf("Product: %s\n", info.Product))
	if info.Version != "" {
		b.WriteString(fmt.Sprintf("Version: %s\n", info.Version))
	}

	switch info.Product {
	case "MySQL":
		res, err := cm.Executor.ExecuteReadOnly(ctx, "SELECT CURRENT_USER(), DATABASE(), CONNECTION_ID(), @@hostname, @@port")
		if err != nil {
			return "", err
		}
		if len(res.Rows) > 0 {
			r := res.Rows[0]
			if len(r) > 0 && r[0] != nil {
				b.WriteString(fmt.Sprintf("Current user: %s\n", *r[0]))
			}
			if len(r) > 1 && r[1] != nil && *r[1] != "" {
				b.WriteString(fmt.Sprintf("Database: %s\n", *r[1]))
			}
			if len(r) > 2 && r[2] != nil {
				b.WriteString(fmt.Sprintf("Tool session ID: %s\n", *r[2]))
			}
			if len(r) > 3 && r[3] != nil {
				b.WriteString(fmt.Sprintf("Host: %s\n", *r[3]))
			}
			if len(r) > 4 && r[4] != nil {
				b.WriteString(fmt.Sprintf("Port: %s\n", *r[4]))
			}
		}

	case "PostgreSQL":
		res, err := cm.Executor.ExecuteReadOnly(ctx, "SELECT current_user, current_database(), pg_backend_pid(), inet_server_addr(), inet_server_port()")
		if err != nil {
			return "", err
		}
		if len(res.Rows) > 0 {
			r := res.Rows[0]
			if len(r) > 0 && r[0] != nil {
				b.WriteString(fmt.Sprintf("Current user: %s\n", *r[0]))
			}
			if len(r) > 1 && r[1] != nil && *r[1] != "" {
				b.WriteString(fmt.Sprintf("Database: %s\n", *r[1]))
			}
			if len(r) > 2 && r[2] != nil {
				b.WriteString(fmt.Sprintf("Tool session ID: %s\n", *r[2]))
			}
			if len(r) > 3 && r[3] != nil {
				b.WriteString(fmt.Sprintf("Server address: %s\n", *r[3]))
			}
			if len(r) > 4 && r[4] != nil {
				b.WriteString(fmt.Sprintf("Server port: %s\n", *r[4]))
			}
		}

	case "SQLite":
		b.WriteString(fmt.Sprintf("Database file: %s\n", cm.Target))
		res, err := cm.Executor.ExecuteReadOnly(ctx, "SELECT sqlite_version()")
		if err == nil && len(res.Rows) > 0 && len(res.Rows[0]) > 0 && res.Rows[0][0] != nil {
			b.WriteString(fmt.Sprintf("Version: %s\n", *res.Rows[0][0]))
		}
	}

	return b.String(), nil
}
