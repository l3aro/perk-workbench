package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
)

// completeTreeAnim drives the sidebar accordion animation to its end state
// by feeding synthetic frame ticks until no animation remains.
func completeTreeAnim(model Model) Model {
	for range 2 * schema.TreeAnimMaxTicks {
		if model.schema.component.Anim == nil {
			return model
		}
		updated, _ := model.Update(schema.TreeAnimTickMsg{})
		model = updated.(Model)
	}
	return model
}

func TestSchemaTree_expandAnimatesChildReveal(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.State, model.Focus = stateReady, focusSchema
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
		{Database: "analytics", Type: "table", Name: "funnels"},
		{Database: "analytics", Type: "table", Name: "sessions"},
		{Database: "app", Type: "database", Name: "app"},
	})
	model.schema.component.List.SetSize(30, 8)
	// Start collapsed, like a fresh session's non-preferred roots.
	model.schema.component.ExpandedDatabases["analytics"] = false
	model.rebuildSchemaTree()

	// Expand the analytics root.
	model.schema.component.List.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.schema.component.Anim == nil {
		t.Fatal("expanding toggle did not start an accordion animation")
	}
	if model.schema.component.Anim.Collapsing {
		t.Fatal("expanding toggle started a collapse animation")
	}
	if model.schema.component.Anim.Total != 3 {
		t.Fatalf("animation total = %d, want 3 child rows", model.schema.component.Anim.Total)
	}
	// First frame: nothing revealed yet.
	if view := ansi.Strip(model.schema.component.List.View()); strings.Contains(view, "  ▪ events") {
		t.Fatalf("frame 0 = %q, want no children yet", view)
	}

	// Intermediate frames reveal rows monotonically until the tree settles.
	seen := 0
	for range 200 {
		updated, _ := model.Update(schema.TreeAnimTickMsg{})
		model = updated.(Model)
		if model.schema.component.Anim == nil {
			break
		}
		rows := strings.Count(ansi.Strip(model.schema.component.List.View()), "  ▪")
		if rows < seen {
			t.Fatalf("reveal regressed: %d then %d rows", seen, rows)
		}
		seen = rows
	}
	model = completeTreeAnim(model)
	view := ansi.Strip(model.schema.component.List.View())
	for _, label := range []string{"▣ analytics", "  ▪ events", "  ▪ funnels", "  ▪ sessions"} {
		if !strings.Contains(view, label) {
			t.Fatalf("settled tree = %q, want %q", view, label)
		}
	}
	if model.schema.component.Anim != nil {
		t.Fatal("animation still running after settling")
	}
}

func TestSchemaTree_collapseAnimatesChildHide(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.State, model.Focus = stateReady, focusSchema
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "analytics", Type: "table", Name: "events"},
		{Database: "analytics", Type: "table", Name: "funnels"},
		{Database: "analytics", Type: "table", Name: "sessions"},
		{Database: "app", Type: "database", Name: "app"},
	})
	model.schema.component.List.SetSize(30, 8)

	// Collapse the expanded analytics root.
	model.schema.component.List.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.schema.component.Anim == nil || !model.schema.component.Anim.Collapsing {
		t.Fatal("collapsing toggle did not start a collapse animation")
	}
	if model.schema.component.Anim.Total != 3 {
		t.Fatalf("animation total = %d, want 3 child rows", model.schema.component.Anim.Total)
	}
	// First frame still shows every child; the settle hides them all.
	if view := ansi.Strip(model.schema.component.List.View()); !strings.Contains(view, "  ▪ events") {
		t.Fatalf("frame 0 = %q, want children still visible", view)
	}
	model = completeTreeAnim(model)
	if view := ansi.Strip(model.schema.component.List.View()); strings.Contains(view, "  ▪") {
		t.Fatalf("settled tree = %q, want no children", view)
	}
}

func TestSchemaTree_emptyRootToggleArmsNoAnimation(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.State, model.Focus = stateReady, focusSchema
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "analytics", Type: "database", Name: "analytics"},
		{Database: "app", Type: "database", Name: "app"},
	})
	model.schema.component.List.SetSize(30, 8)

	model.schema.component.List.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.schema.component.Anim != nil {
		t.Fatal("toggle on an empty root started an animation")
	}
}

// TestSchemaTree_postgresSchemaCollapseAnimatesTables guards the collapse
// row count: it must come from the pre-toggle state, or a closing schema
// would have nothing to animate.
func TestSchemaTree_postgresSchemaCollapseAnimatesTables(t *testing.T) {
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts"},
		{Database: "main", Type: "table", Name: "public.orders"},
	})
	model.schema.component.List.SetSize(30, 8)

	// The load expands main and its first schema; collapse that schema.
	model.schema.component.List.Select(1)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.schema.component.Anim == nil || !model.schema.component.Anim.Collapsing {
		t.Fatal("schema collapse did not start an animation")
	}
	if model.schema.component.Anim.Total != 2 {
		t.Fatalf("schema collapse total = %d, want 2 tables", model.schema.component.Anim.Total)
	}
	model = completeTreeAnim(model)
	if view := ansi.Strip(model.schema.component.List.View()); strings.Contains(view, "    ▪ accounts") {
		t.Fatalf("settled tree = %q, want public's tables hidden", view)
	}
}
