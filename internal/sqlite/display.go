package sqlite

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func displayRow(values []any) []*string {
	row := make([]*string, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		text := ""
		if bytes, ok := value.([]byte); ok {
			text = SanitizeDisplay(string(bytes))
		} else {
			text = SanitizeDisplay(fmt.Sprint(value))
		}
		if len([]rune(text)) > maxRunes {
			text = string([]rune(text)[:maxRunes])
		}
		row[index] = &text
	}
	return row
}

func SanitizeDisplay(input string) string {
	var display strings.Builder
	lastWasSpace := false
	for index := 0; index < len(input); {
		runeValue, size := rune(input[index]), 1
		if runeValue >= utf8.RuneSelf {
			runeValue, size = utf8.DecodeRuneInString(input[index:])
		}
		if runeValue == '\x1b' {
			index += ansiSequenceLen(input[index:])
			continue
		}
		if runeValue == '\r' || runeValue == '\n' {
			if !lastWasSpace {
				display.WriteByte(' ')
				lastWasSpace = true
			}
		} else if !unicode.IsControl(runeValue) {
			display.WriteRune(runeValue)
			lastWasSpace = runeValue == ' '
		}
		index += size
	}
	return display.String()
}

func ansiSequenceLen(input string) int {
	if len(input) == 1 {
		return 1
	}
	switch input[1] {
	case '[':
		for index := 2; index < len(input); index++ {
			if input[index] >= 0x40 && input[index] <= 0x7e {
				return index + 1
			}
		}
	case ']', 'P', '^', '_':
		for index := 2; index < len(input); index++ {
			if input[index] == '\a' {
				return index + 1
			}
			if input[index] == '\x1b' && index+1 < len(input) && input[index+1] == '\\' {
				return index + 2
			}
		}
		return len(input)
	default:
		return 2
	}
	return len(input)
}
