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
// stripped target forms, a valid query-language advertisement, and a
// valid workspace tab advertisement. This is the side-effect-free
// ValidateShim invariant set (drivers.go), minus the registry-conflict
// half: conflicts are global registry state, which a conformance run
// must not depend on.
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
	if err := sharedsql.ValidateWorkspaceCapability(caps.Workspace); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	return nil
}

// validateQueryLanguage checks the invariant set every nonzero query
// language advertisement must hold: name, editor label, and placeholder
// must be nonblank after trimming, every example must be nonblank, and
// every optional command entry must be nonblank, bounded, control-free,
// and case-insensitively unique within the capped list. A zero value is
// not an advertisement and passes. The invariant set lives in the
// shared contract package so registration and this runner can never
// drift apart.
func validateQueryLanguage(ql *database.QueryLanguage) error {
	if ql == nil {
		return nil
	}
	return sharedsql.ValidateQueryLanguage(*ql)
}
