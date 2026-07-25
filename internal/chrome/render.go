package chrome

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	iconBooleanTrue  = "✓"
	iconBooleanFalse = "✗"
)

var (
	trueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	falseStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
)

func BooleanValue(value bool) string {
	if value {
		return trueStyle.Render(iconBooleanTrue)
	}
	return falseStyle.Render(iconBooleanFalse)
}

func DetailValue(value string) string {
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

func ParseHex(value string) color.Color {
	var red, green, blue uint8
	fmt.Sscanf(value, "#%02x%02x%02x", &red, &green, &blue)
	return color.RGBA{R: red, G: green, B: blue, A: 255}
}

func PaneStatus(left, right string, width int) string {
	return left + lipgloss.NewStyle().Width(max(width-lipgloss.Width(left), 0)).Align(lipgloss.Right).Render(right)
}

func FormatFooterKey(key string) string {
	if strings.HasPrefix(key, "Ctrl+") {
		return "^" + strings.ToLower(key[5:])
	}
	return strings.ToLower(key)
}
