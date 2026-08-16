package sql

import (
	"strings"
	"testing"
)

// TestValidateWorkspaceCapability drives the workspace advertisement
// invariant set: nil and all-empty advertisements pass (no
// advertisement), standard tabs come from the fixed set without
// duplicates, and custom views are capped with nonblank bounded
// control-free case-insensitively-unique ids and labels plus nonempty
// duplicate-free valid scopes.
func TestValidateWorkspaceCapability(t *testing.T) {
	if err := ValidateWorkspaceCapability(nil); err != nil {
		t.Fatalf("nil advertisement rejected: %v", err)
	}
	if err := ValidateWorkspaceCapability(&WorkspaceCapability{}); err != nil {
		t.Fatalf("empty advertisement rejected: %v", err)
	}

	valid := WorkspaceCapability{
		StandardTabs: []StandardWorkspaceTab{StandardWorkspaceTabColumns, StandardWorkspaceTabIndexes},
		CustomViews: []CustomWorkspaceView{
			{ID: "key-summary", Label: "Key Summary", Scopes: []WorkspaceViewKind{WorkspaceViewDatabase, WorkspaceViewSchema}},
			{ID: "eviction", Label: "Evictions", Scopes: []WorkspaceViewKind{WorkspaceViewTable}},
		},
	}
	if err := ValidateWorkspaceCapability(&valid); err != nil {
		t.Fatalf("valid advertisement rejected: %v", err)
	}
	allStandard := WorkspaceCapability{
		StandardTabs: []StandardWorkspaceTab{
			StandardWorkspaceTabColumns, StandardWorkspaceTabIndexes,
			StandardWorkspaceTabForeignKeys, StandardWorkspaceTabDiagram,
		},
	}
	if err := ValidateWorkspaceCapability(&allStandard); err != nil {
		t.Fatalf("full standard-tab advertisement rejected: %v", err)
	}

	for _, test := range []struct {
		name string
		ws   WorkspaceCapability
	}{
		{name: "unknown standard tab", ws: WorkspaceCapability{StandardTabs: []StandardWorkspaceTab{"relations"}}},
		{name: "duplicate standard tab", ws: WorkspaceCapability{StandardTabs: []StandardWorkspaceTab{StandardWorkspaceTabColumns, StandardWorkspaceTabColumns}}},
		{name: "blank id", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: " ", Label: "View", Scopes: []WorkspaceViewKind{WorkspaceViewTable}}}}},
		{name: "blank label", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: "view", Label: "\t", Scopes: []WorkspaceViewKind{WorkspaceViewTable}}}}},
		{name: "control in id", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: "a\nb", Label: "View", Scopes: []WorkspaceViewKind{WorkspaceViewTable}}}}},
		{name: "control in label", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: "view", Label: "a\x00b", Scopes: []WorkspaceViewKind{WorkspaceViewTable}}}}},
		{name: "id overlong", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: strings.Repeat("i", MaxWorkspaceViewIDRunes+1), Label: "View", Scopes: []WorkspaceViewKind{WorkspaceViewTable}}}}},
		{name: "label overlong", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: "view", Label: strings.Repeat("l", MaxWorkspaceViewLabelRunes+1), Scopes: []WorkspaceViewKind{WorkspaceViewTable}}}}},
		{name: "duplicate id case-insensitive", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{
			{ID: "Keys", Label: "One", Scopes: []WorkspaceViewKind{WorkspaceViewTable}},
			{ID: "keys", Label: "Two", Scopes: []WorkspaceViewKind{WorkspaceViewTable}},
		}}},
		{name: "duplicate label case-insensitive", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{
			{ID: "one", Label: "Keys", Scopes: []WorkspaceViewKind{WorkspaceViewTable}},
			{ID: "two", Label: "keys", Scopes: []WorkspaceViewKind{WorkspaceViewTable}},
		}}},
		{name: "no scopes", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: "view", Label: "View", Scopes: nil}}}},
		{name: "invalid scope", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: "view", Label: "View", Scopes: []WorkspaceViewKind{"collection"}}}}},
		{name: "duplicate scope", ws: WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: "view", Label: "View", Scopes: []WorkspaceViewKind{WorkspaceViewTable, WorkspaceViewTable}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateWorkspaceCapability(&test.ws); err == nil {
				t.Fatal("invalid advertisement accepted")
			}
		})
	}

	over := WorkspaceCapability{CustomViews: make([]CustomWorkspaceView, MaxCustomWorkspaceViews+1)}
	for i := range over.CustomViews {
		over.CustomViews[i] = CustomWorkspaceView{
			ID: "view" + strings.Repeat("x", i), Label: "View",
			Scopes: []WorkspaceViewKind{WorkspaceViewTable},
		}
	}
	if err := ValidateWorkspaceCapability(&over); err == nil {
		t.Fatal("over-cap custom view list accepted")
	}
}

// TestIsZeroWorkspaceCapability: an advertisement with no standard tabs
// and no custom views carries no metadata.
func TestIsZeroWorkspaceCapability(t *testing.T) {
	if !IsZeroWorkspaceCapability(WorkspaceCapability{}) {
		t.Fatal("empty capability must be zero")
	}
	if IsZeroWorkspaceCapability(WorkspaceCapability{StandardTabs: []StandardWorkspaceTab{StandardWorkspaceTabColumns}}) {
		t.Fatal("standard-tab advertisement must not be zero")
	}
	if IsZeroWorkspaceCapability(WorkspaceCapability{CustomViews: []CustomWorkspaceView{{ID: "a", Label: "A"}}}) {
		t.Fatal("custom-view advertisement must not be zero")
	}
}
