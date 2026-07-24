package workbench

import (
	"bytes"
	"encoding/json"
	"strings"
)

func detailValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return value
	}
	var formatted bytes.Buffer
	if json.Indent(&formatted, []byte(trimmed), "", "  ") != nil {
		return value
	}
	return formatted.String()
}
