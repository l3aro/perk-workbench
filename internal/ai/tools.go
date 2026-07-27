package ai

// ToolDefinition describes a tool the AI can call.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToolCall represents a tool invocation requested by the AI.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult is the outcome of executing a ToolCall.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}
