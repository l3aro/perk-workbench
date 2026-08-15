package conformance

import (
	"errors"
	"fmt"
	"strings"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// validateCapabilities checks the registration invariants of one
// initialize capabilities advertisement: a nonblank name, at least one
// target form with nonblank prefixes, a form whose prefix is one of the
// stripped target forms, and a valid query-language advertisement. This
// is the side-effect-free ValidateShim invariant set (drivers.go),
// minus the registry-conflict half: conflicts are global registry
// state, which a conformance run must not depend on.
func validateCapabilities(caps database.Capabilities) error {
	switch {
	case strings.TrimSpace(caps.Name) == "":
		return errors.New("driver needs a name")
	case len(caps.Targets) == 0:
		return errors.New("driver has no target forms")
	}
	for i, pattern := range caps.Targets {
		if strings.TrimSpace(pattern.Prefix) == "" {
			return fmt.Errorf("target %d has an empty prefix", i)
		}
	}
	if caps.Form != nil && caps.Form.Prefix != "" {
		routed := false
		for _, pattern := range caps.Targets {
			if pattern.Prefix == caps.Form.Prefix && !pattern.KeepTarget {
				routed = true
				break
			}
		}
		if !routed {
			return fmt.Errorf("form prefix %q must be one of its stripped target forms", caps.Form.Prefix)
		}
	}
	if err := validateQueryLanguage(caps.QueryLanguage); err != nil {
		return fmt.Errorf("query language: %w", err)
	}
	return nil
}

// validateQueryLanguage checks the invariant set every nonzero query
// language advertisement must hold: name, editor label, and placeholder
// must be nonblank after trimming, and every example must be nonblank.
// A zero value is not an advertisement and passes.
func validateQueryLanguage(ql *database.QueryLanguage) error {
	if ql == nil || sharedsql.IsZeroQueryLanguage(*ql) {
		return nil
	}
	switch {
	case strings.TrimSpace(ql.Name) == "":
		return errors.New("needs a name")
	case strings.TrimSpace(ql.EditorLabel) == "":
		return fmt.Errorf("%q needs an editor label", ql.Name)
	case strings.TrimSpace(ql.Placeholder) == "":
		return fmt.Errorf("%q needs a placeholder", ql.Name)
	}
	for i, example := range ql.Examples {
		if strings.TrimSpace(example) == "" {
			return fmt.Errorf("%q example %d must not be blank", ql.Name, i)
		}
	}
	return nil
}
