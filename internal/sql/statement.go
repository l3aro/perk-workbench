package sql

import (
	"errors"
	"strings"
	"unicode"
)

func ValidateStatement(input string) error {
	const (
		normal = iota
		lineComment
		blockComment
		singleQuote
		doubleQuote
		backtick
		bracket
	)

	state := normal
	seenToken, trailingSemicolon := false, false
	triggerState := 0
	var token strings.Builder
	consumeToken := func() error {
		if token.Len() == 0 {
			return nil
		}
		word := strings.ToUpper(token.String())
		token.Reset()
		switch triggerState {
		case 0:
			if word == "CREATE" {
				triggerState = 1
			}
		case 1:
			switch word {
			case "TRIGGER":
				return errors.New("trigger statements are not supported")
			case "TEMP", "TEMPORARY":
				triggerState = 2
			case "OR":
				triggerState = 4
			case "IF":
				triggerState = 5
			default:
				triggerState = 3
			}
		case 2:
			if word == "TRIGGER" {
				return errors.New("trigger statements are not supported")
			}
			triggerState = 3
		case 4:
			if word == "REPLACE" {
				triggerState = 1
			} else {
				triggerState = 3
			}
		case 5:
			if word == "NOT" {
				triggerState = 6
			} else {
				triggerState = 3
			}
		case 6:
			if word == "EXISTS" {
				triggerState = 1
			} else {
				triggerState = 3
			}
		}
		return nil
	}

	runes := []rune(input)
	for index := 0; index < len(runes); index++ {
		current := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}

		switch state {
		case lineComment:
			if current == '\n' || current == '\r' {
				state = normal
			}
			continue
		case blockComment:
			if current == '*' && next == '/' {
				state = normal
				index++
			}
			continue
		case singleQuote:
			if current == '\'' {
				if next == '\'' {
					index++
				} else {
					state = normal
				}
			}
			continue
		case doubleQuote:
			if current == '"' {
				if next == '"' {
					index++
				} else {
					state = normal
				}
			}
			continue
		case backtick:
			if current == '`' {
				if next == '`' {
					index++
				} else {
					state = normal
				}
			}
			continue
		case bracket:
			if current == ']' {
				state = normal
			}
			continue
		}

		if unicode.IsSpace(current) {
			if err := consumeToken(); err != nil {
				return err
			}
			continue
		}
		if current == '-' && next == '-' {
			if err := consumeToken(); err != nil {
				return err
			}
			state, index = lineComment, index+1
			continue
		}
		if current == '/' && next == '*' {
			if err := consumeToken(); err != nil {
				return err
			}
			state, index = blockComment, index+1
			continue
		}
		if current == ';' {
			if err := consumeToken(); err != nil {
				return err
			}
			if !seenToken || trailingSemicolon {
				return errors.New("only one statement is allowed")
			}
			trailingSemicolon = true
			continue
		}
		if trailingSemicolon {
			return errors.New("only one statement is allowed")
		}
		seenToken = true
		switch current {
		case '\'':
			if err := consumeToken(); err != nil {
				return err
			}
			state = singleQuote
		case '"':
			if err := consumeToken(); err != nil {
				return err
			}
			state = doubleQuote
		case '`':
			if err := consumeToken(); err != nil {
				return err
			}
			state = backtick
		case '[':
			if err := consumeToken(); err != nil {
				return err
			}
			state = bracket
		default:
			if unicode.IsLetter(current) || unicode.IsDigit(current) || current == '_' {
				token.WriteRune(current)
			} else if err := consumeToken(); err != nil {
				return err
			}
		}
	}
	if err := consumeToken(); err != nil {
		return err
	}
	if !seenToken {
		return errors.New("statement is empty")
	}
	return nil
}
