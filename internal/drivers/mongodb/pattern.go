package mongodb

import (
	"regexp"
	"strings"
)

func globToRegex(pattern string) string {
	var builder strings.Builder
	builder.Grow(len(pattern) + 2)
	builder.WriteByte('^')
	for _, r := range pattern {
		switch r {
		case '*':
			builder.WriteString(".*")
		case '?':
			builder.WriteByte('.')
		default:
			builder.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	builder.WriteByte('$')
	return builder.String()
}
