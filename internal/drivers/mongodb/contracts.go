package mongodb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

const (
	maxRows  = 500
	maxRunes = 300
)

const (
	browseFilterLike         = "LIKE"
	browseFilterNotLike      = "NOT LIKE"
	browseFilterPattern      = "PATTERN"
	browseFilterNotPattern   = "NOT PATTERN"
	browseFilterEqual        = "="
	browseFilterNotEqual     = "!="
	browseFilterLess         = "<"
	browseFilterLessEqual    = "<="
	browseFilterGreater      = ">"
	browseFilterGreaterEqual = ">="
	browseFilterIsNull       = "IS NULL"
	browseFilterIsNotNull    = "IS NOT NULL"
)

func sanitizeDisplay(input string, limits ...int) string {
	limit := 0
	if len(limits) > 0 {
		limit = limits[0]
	}
	var display strings.Builder
	emitted := 0
	lastWasSpace := false
	truncated := false
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
				emitted++
				lastWasSpace = true
				if limit > 0 && emitted >= limit {
					truncated = true
					break
				}
			}
		} else if !unicode.IsControl(runeValue) {
			display.WriteRune(runeValue)
			emitted++
			lastWasSpace = runeValue == ' '
			if limit > 0 && emitted >= limit {
				truncated = true
				break
			}
		}
		index += size
	}
	if truncated {
		display.WriteString("…")
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

func classifyError(kind driver.ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	if existing, ok := err.(*driver.OperationError); ok {
		return existing
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return driver.NewOperationError(driver.KindCancelled, err.Error())
	}
	return driver.NewOperationError(kind, err.Error())
}

func validationError(err error) error { return classifyError(driver.KindValidation, err) }
func operationError(err error) error  { return classifyError(driver.KindOperation, err) }
func connectionError(err error) error { return classifyError(driver.KindConnection, err) }
func unsupportedError(message string) error {
	return driver.NewOperationError(driver.KindUnsupported, message)
}

func invalidBrowseRange(options driver.BrowseOptions) error {
	return validationError(fmt.Errorf("invalid browse range: offset=%d limit=%d", options.Offset, options.Limit))
}
