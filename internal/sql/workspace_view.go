package sql

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// StandardWorkspaceTab is one standard (built-in) workspace tab a driver
// may explicitly advertise support for. Query and Browse are never part
// of the advertisement: those tabs keep their existing per-scope policy
// for every driver.
type StandardWorkspaceTab string

// The fixed standard tab set. Drivers advertise a subset; absent
// metadata keeps the legacy per-product policy unchanged.
const (
	StandardWorkspaceTabColumns     StandardWorkspaceTab = "columns"
	StandardWorkspaceTabIndexes     StandardWorkspaceTab = "indexes"
	StandardWorkspaceTabForeignKeys StandardWorkspaceTab = "foreign_keys"
	StandardWorkspaceTabDiagram     StandardWorkspaceTab = "diagram"
)

// WorkspaceViewKind is the structured-target kind of a workspace view
// request, mirroring the workbench's workspace scope kinds.
type WorkspaceViewKind string

// The target kinds a workspace view may serve.
const (
	WorkspaceViewDatabase WorkspaceViewKind = "database"
	WorkspaceViewSchema   WorkspaceViewKind = "schema"
	WorkspaceViewTable    WorkspaceViewKind = "table"
)

// CustomWorkspaceView is one plain-data tab a driver advertises: a
// stable nonblank id, a human label rendered in the workspace tab row,
// and the scopes it serves (one or more of database/schema/table). It
// carries no code and no UI: the workbench owns lifecycle, rendering,
// input, and cancellation; the driver only answers bounded table data
// for the view.
type CustomWorkspaceView struct {
	ID     string              `json:"id"`
	Label  string              `json:"label"`
	Scopes []WorkspaceViewKind `json:"scopes"`
}

// WorkspaceCapability is the optional workspace tab advertisement of a
// driver: the subset of standard tabs it supports (Columns, Indexes,
// Foreign Keys, Diagram) and its ordered custom plain-data views. A nil
// capability carries no advertisement: the workbench keeps the legacy
// per-product tab policy exactly, and old plugins and built-in drivers
// keep their current behavior unchanged. When the capability is present
// it is authoritative: standard tabs are filtered by the explicit
// advertisement, and custom views are appended after them in advertised
// order, filtered by their scopes.
type WorkspaceCapability struct {
	StandardTabs []StandardWorkspaceTab `json:"standard_tabs,omitempty"`
	CustomViews  []CustomWorkspaceView  `json:"custom_views,omitempty"`
}

// Bounds every workspace capability advertisement must respect, so a
// plugin can never force an unbounded tab row or handshake frame. The
// values are conservative: a tab label renders in the tab row, and a
// view id travels on every workspace_view request.
const (
	// MaxCustomWorkspaceViews caps the advertised custom tab list.
	MaxCustomWorkspaceViews = 8
	// MaxWorkspaceViewIDRunes caps one custom view id.
	MaxWorkspaceViewIDRunes = 64
	// MaxWorkspaceViewLabelRunes caps one custom view label.
	MaxWorkspaceViewLabelRunes = 32
)

// IsZeroWorkspaceCapability reports whether ws carries no advertisement
// at all — no standard tabs and no custom views. A nil pointer is not an
// advertisement either; the helper exists for pointer-free callers.
func IsZeroWorkspaceCapability(ws WorkspaceCapability) bool {
	return len(ws.StandardTabs) == 0 && len(ws.CustomViews) == 0
}

// ValidateWorkspaceCapability checks the invariant set every workspace
// capability advertisement must hold: standard tabs come from the fixed
// set with no duplicates, and the custom view list is capped with every
// view carrying a nonblank bounded control-free id and label, unique
// case-insensitively within the list, plus a nonempty scope list of
// valid kinds with no duplicates. A nil (or all-empty) capability is
// not an advertisement and passes. This is the single Go invariant set
// shared by driver registration and the plugin conformance runner.
func ValidateWorkspaceCapability(ws *WorkspaceCapability) error {
	if ws == nil {
		return nil
	}
	seenTabs := make(map[StandardWorkspaceTab]bool, len(ws.StandardTabs))
	for _, tab := range ws.StandardTabs {
		switch tab {
		case StandardWorkspaceTabColumns, StandardWorkspaceTabIndexes,
			StandardWorkspaceTabForeignKeys, StandardWorkspaceTabDiagram:
		default:
			return fmt.Errorf("workspace standard tab %q is not one of columns, indexes, foreign_keys, diagram", tab)
		}
		if seenTabs[tab] {
			return fmt.Errorf("workspace standard tab %q is repeated", tab)
		}
		seenTabs[tab] = true
	}
	if len(ws.CustomViews) > MaxCustomWorkspaceViews {
		return fmt.Errorf("workspace advertises %d custom views, cap is %d", len(ws.CustomViews), MaxCustomWorkspaceViews)
	}
	seenIDs := make(map[string]bool, len(ws.CustomViews))
	seenLabels := make(map[string]bool, len(ws.CustomViews))
	for i, view := range ws.CustomViews {
		if err := validateWorkspaceViewID(i, view.ID); err != nil {
			return err
		}
		if err := validateWorkspaceViewLabel(i, view.Label); err != nil {
			return err
		}
		lowerID := strings.ToLower(view.ID)
		if seenIDs[lowerID] {
			return fmt.Errorf("workspace custom view %d id %q is repeated case-insensitively", i, view.ID)
		}
		seenIDs[lowerID] = true
		lowerLabel := strings.ToLower(view.Label)
		if seenLabels[lowerLabel] {
			return fmt.Errorf("workspace custom view %d label %q is repeated case-insensitively", i, view.Label)
		}
		seenLabels[lowerLabel] = true
		if len(view.Scopes) == 0 {
			return fmt.Errorf("workspace custom view %d id %q needs at least one scope", i, view.ID)
		}
		seenScopes := make(map[WorkspaceViewKind]bool, len(view.Scopes))
		for _, scope := range view.Scopes {
			switch scope {
			case WorkspaceViewDatabase, WorkspaceViewSchema, WorkspaceViewTable:
			default:
				return fmt.Errorf("workspace custom view %d id %q has invalid scope %q", i, view.ID, scope)
			}
			if seenScopes[scope] {
				return fmt.Errorf("workspace custom view %d id %q repeats scope %q", i, view.ID, scope)
			}
			seenScopes[scope] = true
		}
	}
	return nil
}

func validateWorkspaceViewID(index int, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("workspace custom view %d id must not be blank", index)
	}
	if runes := len([]rune(id)); runes > MaxWorkspaceViewIDRunes {
		return fmt.Errorf("workspace custom view %d id is %d runes, cap is %d", index, runes, MaxWorkspaceViewIDRunes)
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return fmt.Errorf("workspace custom view %d id contains a control character", index)
		}
	}
	return nil
}

func validateWorkspaceViewLabel(index int, label string) error {
	if strings.TrimSpace(label) == "" {
		return fmt.Errorf("workspace custom view %d label must not be blank", index)
	}
	if runes := len([]rune(label)); runes > MaxWorkspaceViewLabelRunes {
		return fmt.Errorf("workspace custom view %d label is %d runes, cap is %d", index, runes, MaxWorkspaceViewLabelRunes)
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return fmt.Errorf("workspace custom view %d label contains a control character", index)
		}
	}
	return nil
}

// WorkspaceViewTarget is the active structured target of one workspace
// view request: the scope kind plus the identifiers the kind needs. It
// is plain data, so it crosses the plugin DTO boundary unchanged.
type WorkspaceViewTarget struct {
	Kind     WorkspaceViewKind `json:"kind"`
	Database string            `json:"database,omitempty"`
	Schema   string            `json:"schema,omitempty"`
	Table    string            `json:"table,omitempty"`
}

// WorkspaceViewRequest is one plain-data request for a custom workspace
// view: the advertised view id and the active structured target. The
// result reuses the bounded table-result conventions of Result.
type WorkspaceViewRequest struct {
	ViewID string              `json:"view_id"`
	Target WorkspaceViewTarget `json:"target"`
}

// WorkspaceViewProvider is the optional interface a service implements
// when its driver advertises custom workspace views. Compiled-in drivers
// implement it directly; plugin sessions get it wrapped over the wire
// only when the plugin's capabilities advertise custom views, so
// non-advertisers stay source- and protocol-compatible without the
// method.
type WorkspaceViewProvider interface {
	// WorkspaceView executes one advertised custom view for the active
	// structured target. It is a session operation: the caller's context
	// cancels it like any other session call, and the result follows the
	// bounded table-result conventions (500 rows / 300 runes per cell in
	// the display path).
	WorkspaceView(context.Context, WorkspaceViewRequest) (Result, error)
}
