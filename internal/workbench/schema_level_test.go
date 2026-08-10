package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// levelTreeModel builds a ready non-server tree: analytics (expanded with
// three tables) and app (expanded, no children).
func levelTreeModel(t *testing.T) Model {
	t.Helper()
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
	return model
}

func TestSchemaLevel_rightExpandsCollapsedRoot(t *testing.T) {
	model := levelTreeModel(t)
	// Collapse analytics first (the load expands it).
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = completeTreeAnim(model)

	// When — l on the collapsed root.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l'})
	model = updated.(Model)

	// Then — the accordion expands it.
	if model.treeAnim == nil || model.treeAnim.collapsing {
		t.Fatal("l on a collapsed root did not start an expand animation")
	}
	model = completeTreeAnim(model)
	view := ansi.Strip(model.schema.View())
	for _, label := range []string{"▣ analytics", "  ▪ events", "  ▪ funnels", "  ▪ sessions"} {
		if !strings.Contains(view, label) {
			t.Fatalf("tree = %q, want %q", view, label)
		}
	}
}

func TestSchemaLevel_rightOnExpandedRootMovesToFirstChild(t *testing.T) {
	model := levelTreeModel(t)

	// When — right arrow on the expanded analytics root.
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)

	// Then — the cursor drops onto the first child, no toggle.
	if model.treeAnim != nil {
		t.Fatal("right on an expanded root started an animation")
	}
	if got := model.schema.Index(); got != 1 {
		t.Fatalf("cursor = %d, want the first child row at 1", got)
	}
}

func TestSchemaLevel_leftCollapsesExpandedRoot(t *testing.T) {
	model := levelTreeModel(t)

	// When — h on the expanded analytics root.
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)

	// Then — the accordion collapses it.
	if model.treeAnim == nil || !model.treeAnim.collapsing {
		t.Fatal("h on an expanded root did not start a collapse animation")
	}
	model = completeTreeAnim(model)
	if view := ansi.Strip(model.schema.View()); strings.Contains(view, "  ▪ events") {
		t.Fatalf("tree = %q, want analytics without children", view)
	}
}

func TestSchemaLevel_leftOnChildJumpsToParent(t *testing.T) {
	model := levelTreeModel(t)

	// h on a table moves the cursor to the database root.
	model.schema.Select(2) // funnels
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model = updated.(Model)
	if model.treeAnim != nil {
		t.Fatal("left on a child started an animation")
	}
	if got := model.schema.Index(); got != 0 {
		t.Fatalf("cursor = %d, want the analytics root at 0", got)
	}
}

func TestSchemaLevel_leftOnCollapsedRootNoop(t *testing.T) {
	model := levelTreeModel(t)
	// Collapse analytics.
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = completeTreeAnim(model)

	// h on the collapsed root changes nothing.
	model.schema.Select(0)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	if model.treeAnim != nil {
		t.Fatal("h on a collapsed root started an animation")
	}
	if got := model.schema.Index(); got != 0 {
		t.Fatalf("cursor moved to %d on a collapsed root", got)
	}
}

func TestSchemaLevel_rightOnLeafNoop(t *testing.T) {
	model := levelTreeModel(t)

	model.schema.Select(1) // events
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	if model.treeAnim != nil {
		t.Fatal("right on a leaf started an animation")
	}
	if got := model.schema.Index(); got != 1 {
		t.Fatalf("cursor moved to %d on a leaf", got)
	}
}

func TestSchemaLevel_paletteExpandCollapse(t *testing.T) {
	model := levelTreeModel(t)
	// Collapse analytics (the load expands it).
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model = completeTreeAnim(model)

	// When — schema.expand from the palette on the collapsed root.
	model.schema.Select(0)
	updated, _ = model.handlePaletteCommand("schema.expand")
	model = updated.(Model)

	// Then — the palette closes and the accordion expands the root.
	if model.commandPalette.visible {
		t.Fatal("palette stayed open after schema.expand")
	}
	if model.treeAnim == nil || model.treeAnim.collapsing {
		t.Fatal("schema.expand did not start an expand animation")
	}
	model = completeTreeAnim(model)
	if view := ansi.Strip(model.schema.View()); !strings.Contains(view, "  ▪ events") {
		t.Fatalf("tree = %q, want analytics expanded with events", view)
	}

	// When — schema.collapse from the palette on the expanded root.
	model.schema.Select(0)
	updated, _ = model.handlePaletteCommand("schema.collapse")
	model = updated.(Model)

	// Then — the palette closes and the accordion collapses the root.
	if model.commandPalette.visible {
		t.Fatal("palette stayed open after schema.collapse")
	}
	if model.treeAnim == nil || !model.treeAnim.collapsing {
		t.Fatal("schema.collapse did not start a collapse animation")
	}
	model = completeTreeAnim(model)
	if view := ansi.Strip(model.schema.View()); strings.Contains(view, "  ▪ events") {
		t.Fatalf("tree = %q, want analytics without children", view)
	}
}

func TestSchemaLevel_postgresNavigation(t *testing.T) {
	model := serverProductModel(t, "PostgreSQL", &createDatabaseStub{})
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "main", Type: "database", Name: "main"},
		{Database: "main", Type: "schema", Name: "public"},
		{Database: "main", Type: "table", Name: "public.accounts"},
		{Database: "main", Type: "table", Name: "public.orders"},
		{Database: "main", Type: "schema", Name: "audit"},
		{Database: "main", Type: "table", Name: "audit.events"},
	})
	model.schema.SetSize(30, 8)

	// l on the expanded main root drops to its first schema.
	model.schema.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'l'})
	model = updated.(Model)
	if model.treeAnim != nil || model.schema.Index() != 1 {
		t.Fatalf("l on main = anim %v, cursor %d, want the public schema at 1", model.treeAnim != nil, model.schema.Index())
	}

	// l on the expanded public schema drops to its first table.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	if model.treeAnim != nil || model.schema.Index() != 2 {
		t.Fatalf("l on public = anim %v, cursor %d, want accounts at 2", model.treeAnim != nil, model.schema.Index())
	}

	// h on a table jumps to its schema row, not the database root.
	model.schema.Select(3) // orders
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'h'})
	model = updated.(Model)
	if model.schema.Index() != 1 {
		t.Fatalf("h on orders = cursor %d, want the public schema at 1", model.schema.Index())
	}

	// h on a collapsed schema jumps to the database root.
	model.schema.Select(4) // audit
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	model = updated.(Model)
	if model.schema.Index() != 0 {
		t.Fatalf("h on audit = cursor %d, want the main root at 0", model.schema.Index())
	}

	// l on the collapsed audit schema expands it with the accordion.
	model.schema.Select(4)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l'})
	model = updated.(Model)
	if model.treeAnim == nil || model.treeAnim.collapsing {
		t.Fatal("l on a collapsed schema did not start an expand animation")
	}
	model = completeTreeAnim(model)
	if view := ansi.Strip(model.schema.View()); !strings.Contains(view, "    ▪ events") {
		t.Fatalf("tree = %q, want audit expanded with events", view)
	}
}
