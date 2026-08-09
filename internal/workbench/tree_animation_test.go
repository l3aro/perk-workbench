package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// completeTreeAnim drives the sidebar accordion animation to its end state
// by feeding synthetic frame ticks until no animation remains.
func completeTreeAnim(model Model) Model {
	for range 2 * treeAnimMaxTicks {
		if model.treeAnim == nil {
			return model
		}
		updated, _ := model.Update(treeAnimTickMsg{})
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
	model.schema.SetSize(30, 8)
	// Start collapsed, like a fresh session's non-preferred roots.
	model.expandedDatabases["analytics"] = false
	model.rebuildSchemaTree()

	// Expand the analytics root.
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.treeAnim == nil {
		t.Fatal("expanding toggle did not start an accordion animation")
	}
	if model.treeAnim.collapsing {
		t.Fatal("expanding toggle started a collapse animation")
	}
	if model.treeAnim.total != 3 {
		t.Fatalf("animation total = %d, want 3 child rows", model.treeAnim.total)
	}
	// First frame: nothing revealed yet.
	if view := ansi.Strip(model.schema.View()); strings.Contains(view, "  ▪ events") {
		t.Fatalf("frame 0 = %q, want no children yet", view)
	}

	// Intermediate frames reveal rows monotonically until the tree settles.
	seen := 0
	for range 200 {
		updated, _ := model.Update(treeAnimTickMsg{})
		model = updated.(Model)
		if model.treeAnim == nil {
			break
		}
		rows := strings.Count(ansi.Strip(model.schema.View()), "  ▪")
		if rows < seen {
			t.Fatalf("reveal regressed: %d then %d rows", seen, rows)
		}
		seen = rows
	}
	model = completeTreeAnim(model)
	view := ansi.Strip(model.schema.View())
	for _, label := range []string{"▣ analytics", "  ▪ events", "  ▪ funnels", "  ▪ sessions"} {
		if !strings.Contains(view, label) {
			t.Fatalf("settled tree = %q, want %q", view, label)
		}
	}
	if model.treeAnim != nil {
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
	model.schema.SetSize(30, 8)

	// Collapse the expanded analytics root.
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.treeAnim == nil || !model.treeAnim.collapsing {
		t.Fatal("collapsing toggle did not start a collapse animation")
	}
	if model.treeAnim.total != 3 {
		t.Fatalf("animation total = %d, want 3 child rows", model.treeAnim.total)
	}
	// First frame still shows every child; the settle hides them all.
	if view := ansi.Strip(model.schema.View()); !strings.Contains(view, "  ▪ events") {
		t.Fatalf("frame 0 = %q, want children still visible", view)
	}
	model = completeTreeAnim(model)
	if view := ansi.Strip(model.schema.View()); strings.Contains(view, "  ▪") {
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
	model.schema.SetSize(30, 8)

	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.treeAnim != nil {
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
	model.schema.SetSize(30, 8)

	// The load expands main and its first schema; collapse that schema.
	model.schema.Select(1)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.treeAnim == nil || !model.treeAnim.collapsing {
		t.Fatal("schema collapse did not start an animation")
	}
	if model.treeAnim.total != 2 {
		t.Fatalf("schema collapse total = %d, want 2 tables", model.treeAnim.total)
	}
	model = completeTreeAnim(model)
	if view := ansi.Strip(model.schema.View()); strings.Contains(view, "    ▪ accounts") {
		t.Fatalf("settled tree = %q, want public's tables hidden", view)
	}
}
