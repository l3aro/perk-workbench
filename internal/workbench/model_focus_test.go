package workbench

import (
	"context"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func TestView_sql_cursor_accounts_for_rendered_layout(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		wantX, wantY  int
	}{
		{name: "wide", width: 100, height: 24, wantX: 36, wantY: 2},
		{name: "compact", width: 80, height: 24, wantX: 8, wantY: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", Open(context.Background()))
			model.State = stateReady

			// When
			updated, _ := model.Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			view := updated.(Model).View()

			// Then
			if view.Cursor == nil {
				t.Fatal("view cursor = nil, want SQL cursor")
			}
			if got := view.Cursor.Position.X; got != test.wantX {
				t.Errorf("cursor X = %d, want %d", got, test.wantX)
			}
			if got := view.Cursor.Position.Y; got != test.wantY {
				t.Errorf("cursor Y = %d, want %d", got, test.wantY)
			}
		})
	}
}

func TestWorkspace_tabs_route_input_to_the_active_view(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.State = stateReady
	model.editor.textarea.SetValue("select ")

	// When
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusWorkspace)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabStructure)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.editor.textarea.Value(); got != "select " {
		t.Fatalf("non-SQL tab changed editor value = %q, want %q", got, "select ")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabBrowse)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)

	// Then
	if got := model.editor.textarea.Value(); got != "select " {
		t.Fatalf("non-SQL tab changed editor value = %q, want %q", got, "select ")
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabSQL)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabBrowse)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(Model)

	// Then
	assertTab(t, model, tabStructure)
}

func TestFocus_sql_keeps_q_as_text_after_input_starts(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.State = stateReady
	model.editor.textarea.SetValue("select ")

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("editor q returned a root quit command")
	}
	if got := model.editor.textarea.Value(); got != "select q" {
		t.Fatalf("editor value = %q, want %q", got, "select q")
	}
}

func TestFocus_schema_filters_with_slash_and_esc(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.State, model.Focus = stateReady, focusSchema
	model.schema.SetItems([]list.Item{
		schemaItem{title: "accounts", description: "table"},
		schemaItem{title: "queue_1", description: "table"},
	})

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	updated, command := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)
	model = updateFromCommand(model, command)
	updated, command = model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)
	model = updateFromCommand(model, command)

	// Then
	if !model.schema.SettingFilter() {
		t.Fatal("schema filter is not active")
	}
	if got := model.schema.FilterValue(); got != "q1" {
		t.Fatalf("filter value = %q, want %q", got, "q1")
	}
	if got := model.schema.VisibleItems(); len(got) != 1 || got[0].FilterValue() != "queue_1" {
		t.Fatalf("visible items = %#v, want queue_1", got)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)

	// Then
	if model.schema.SettingFilter() {
		t.Fatal("schema filter remains active after escape")
	}
	if got := model.schema.FilterValue(); got != "" {
		t.Fatalf("filter value = %q, want empty", got)
	}
	if got := model.schema.VisibleItems(); len(got) != 2 {
		t.Fatalf("visible items = %#v, want both tables", got)
	}
}

func TestFocus_numeric_keys_switch_between_tables_and_tabs(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.State = stateReady

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusSchema)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	model = updated.(Model)

	// Then
	assertFocus(t, model, focusWorkspace)
}

func updateFromCommand(model Model, command tea.Cmd) Model {
	if command == nil {
		return model
	}

	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, command := range batch {
			model = updateFromCommand(model, command)
		}
		return model
	}

	updated, _ := model.Update(message)
	return updated.(Model)
}

func assertFocus(t *testing.T, model Model, want focus) {
	t.Helper()
	if model.Focus != want {
		t.Fatalf("focus = %v, want %v", model.Focus, want)
	}
	if got := model.editor.textarea.Focused(); got != (want == focusWorkspace && model.Tab == tabSQL) {
		t.Fatalf("editor focused = %t, want %t", got, want == focusWorkspace && model.Tab == tabSQL)
	}
}

func assertTab(t *testing.T, model Model, want workspaceTab) {
	t.Helper()
	assertFocus(t, model, focusWorkspace)
	if model.Tab != want {
		t.Fatalf("tab = %v, want %v", model.Tab, want)
	}
}
