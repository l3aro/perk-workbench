package workbench

import "strings"

type completion struct {
	all      []string
	matches  []string
	prefix   string
	selected int
}

func newCompletion(values []string) completion {
	return completion{all: values}
}

func (c *completion) filter(prefix string) {
	c.prefix = prefix
	c.matches = c.matches[:0]
	for _, value := range c.all {
		if strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)) {
			c.matches = append(c.matches, value)
		}
	}
	c.selected = 0
}

func (c completion) accept() string {
	if len(c.matches) == 0 {
		return ""
	}
	return c.matches[c.selected]
}

func (c *completion) move(delta int) {
	if len(c.matches) == 0 {
		return
	}
	c.selected = (c.selected + delta + len(c.matches)) % len(c.matches)
}

func sqlCompletionPrefix(value string) string {
	for index := len(value); index > 0; index-- {
		character := value[index-1]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '.' {
			return value[index:]
		}
	}
	return value
}

var sqlKeywords = []string{
	"ALTER", "AND", "AS", "ASC", "BETWEEN", "BY", "CASE", "CREATE", "DELETE", "DESC", "DISTINCT", "DROP",
	"FROM", "GROUP", "HAVING", "INSERT", "INTO", "JOIN", "LEFT", "LIMIT", "NOT", "NULL", "ON", "OR",
	"ORDER", "RIGHT", "SELECT", "SET", "UPDATE", "VALUES", "WHERE", "WITH",
}
