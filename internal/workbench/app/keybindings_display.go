package app

import "strings"

// DisplayKey returns the first configured key label for a command.
func (b Keybindings) DisplayKey(id CommandID) string {
	def, ok := b.commands[id]
	if !ok || len(def.keys) == 0 {
		return ""
	}
	return displayKey(def.keys[0])
}

// DisplayKeys returns all configured key labels for a command, comma-separated.
func (b Keybindings) DisplayKeys(id CommandID) string {
	def, ok := b.commands[id]
	if !ok || len(def.keys) == 0 {
		return ""
	}
	labels := make([]string, len(def.keys))
	for i, k := range def.keys {
		labels[i] = displayKey(k)
	}
	return strings.Join(labels, ", ")
}

func displayKey(stroke string) string {
	switch stroke {
	case "enter":
		return "Enter"
	case "esc":
		return "Esc"
	case "tab":
		return "Tab"
	case "space":
		return "Space"
	case "backspace":
		return "BS"
	case "shift+tab":
		return "Shift+Tab"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	default:
		if len(stroke) == 1 {
			return stroke
		}
		return titledStroke(stroke)
	}
}

func titledStroke(s string) string {
	parts := strings.Split(s, "+")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "+")
}
