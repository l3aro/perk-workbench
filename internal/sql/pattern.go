package sql

import (
	"regexp"
	"strings"
)

// GlobToLike converts a shell-style wildcard pattern into a SQL LIKE
// pattern with backslash escapes. In the pattern, * matches any run of
// characters, ? matches exactly one character, and every other rune —
// including %, _ and \ — is literal. The result must be used with an
// ESCAPE '\' clause.
func GlobToLike(pattern string) string {
	var builder strings.Builder
	builder.Grow(len(pattern))
	for _, r := range pattern {
		switch r {
		case '*':
			builder.WriteByte('%')
		case '?':
			builder.WriteByte('_')
		case '\\', '%', '_':
			builder.WriteByte('\\')
			builder.WriteRune(r)
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// GlobToRegex converts a shell-style wildcard pattern into an anchored
// regular expression: * matches any run of characters, ? matches exactly
// one character, and every other rune is literal.
func GlobToRegex(pattern string) string {
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
