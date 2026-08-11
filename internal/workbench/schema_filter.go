package workbench

import (
	"regexp"
	"strings"

	"charm.land/bubbles/v2/list"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// schemaFilterSeparator joins the name fields of schemaItem.FilterValue.
// It cannot be typed into the filter input, so glob matching can rely on
// it to recover the individual database/schema/table/kind names.
const schemaFilterSeparator = "\x00"

// schemaListFilter is the schema sidebar's list filter. A term containing
// a * or ? wildcard is treated as a shell-style glob: * matches any run of
// characters, ? matches exactly one character, and everything else — % and
// _ included — is literal. Matching is case-insensitive and anchored: the
// pattern must match one of the item's own name fields (title, schema, or
// kind). The containing database is not matched, so a table under office
// is found by its own name, not by its database. Any other term keeps the
// list's default fuzzy matching unchanged.
func schemaListFilter(term string, targets []string) []list.Rank {
	if !strings.ContainsAny(term, "*?") {
		plain := make([]string, len(targets))
		for index, target := range targets {
			plain[index] = strings.TrimSpace(strings.ReplaceAll(target, schemaFilterSeparator, " "))
		}
		return list.DefaultFilter(term, plain)
	}
	pattern := regexp.MustCompile("(?i)" + sharedsql.GlobToRegex(term))
	ranks := make([]list.Rank, 0, len(targets))
	for index, target := range targets {
		fields := strings.Split(target, schemaFilterSeparator)
		for _, part := range fields[1:] { // fields[0] is the containing database
			if pattern.MatchString(part) {
				ranks = append(ranks, list.Rank{Index: index})
				break
			}
		}
	}
	return ranks
}
