package sql

import (
	"regexp"
	"testing"
)

func TestGlobToLike(t *testing.T) {
	for _, test := range []struct {
		pattern string
		want    string
	}{
		{pattern: "rez_*_", want: `rez\_%\_`},
		{pattern: "a?b", want: "a_b"},
		{pattern: "*", want: "%"},
		{pattern: "plain", want: "plain"},
		{pattern: "100%", want: `100\%`},
		{pattern: "a\\b", want: `a\\b`},
		{pattern: "", want: ""},
		{pattern: "*a?b*c", want: "%a_b%c"},
	} {
		if got := GlobToLike(test.pattern); got != test.want {
			t.Errorf("GlobToLike(%q) = %q, want %q", test.pattern, got, test.want)
		}
	}
}

func TestGlobToRegex(t *testing.T) {
	for _, test := range []struct {
		pattern string
		want    string
	}{
		{pattern: "rez_*_", want: `^rez_.*_$`},
		{pattern: "a?b", want: "^a.b$"},
		{pattern: "*", want: "^.*$"},
		{pattern: "plain", want: "^plain$"},
		{pattern: "a.b", want: `^a\.b$`},
		{pattern: "100%", want: "^100%$"},
		{pattern: "a\\b", want: `^a\\b$`},
		{pattern: "", want: "^$"},
	} {
		got := GlobToRegex(test.pattern)
		if got != test.want {
			t.Errorf("GlobToRegex(%q) = %q, want %q", test.pattern, got, test.want)
		}
		if _, err := regexp.Compile(got); err != nil {
			t.Errorf("GlobToRegex(%q) = %q does not compile: %v", test.pattern, got, err)
		}
	}
}
