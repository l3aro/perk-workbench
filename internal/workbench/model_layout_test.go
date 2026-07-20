package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestResize_wide_and_compact_focus_layout(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.state = stateReady

	// When
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	// Then
	if model.compact {
		t.Fatal("wide terminal unexpectedly used compact layout")
	}
	if model.schemaWidth <= 0 || model.editorWidth <= 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
		t.Fatalf("wide layout has invalid dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = updated.(Model)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)

	// Then
	if !model.compact {
		t.Fatal("80-column terminal did not use compact layout")
	}
	if model.focus != focusResults {
		t.Fatalf("focus = %v, want results after tab", model.focus)
	}
	if model.schemaWidth <= 0 || model.editorWidth < 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
		t.Fatalf("compact layout has invalid dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
	}

	// When
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	model = updated.(Model)

	// Then
	if model.schemaWidth < 0 || model.editorWidth < 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
		t.Fatalf("edge layout has negative dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
	}
}

func TestResize_wide_layout_uses_plan_formula(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.state = stateReady

	// When
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(Model)

	// Then
	if got := model.schemaWidth; got != 30 {
		t.Errorf("schema width = %d, want 30", got)
	}
	if got := model.editorWidth; got != 68 {
		t.Errorf("right width = %d, want 68", got)
	}
	if got := model.editorHeight; got != 8 {
		t.Errorf("editor height = %d, want 8", got)
	}
	if got := model.resultsHeight; got != 12 {
		t.Errorf("results height = %d, want 12", got)
	}
}

func TestResize_small_nonzero_dimensions_render_without_negative_sizes(t *testing.T) {
	tests := []struct {
		name          string
		state         modelState
		focus         focus
		width, height int
	}{
		{name: "picking at 1x4", state: statePicking, focus: focusEditor, width: 1, height: 4},
		{name: "opening at 2x5", state: stateOpening, focus: focusEditor, width: 2, height: 5},
		{name: "failure at 1x4", state: stateFailure, focus: focusEditor, width: 1, height: 4},
		{name: "schema at 2x5", state: stateReady, focus: focusSchema, width: 2, height: 5},
		{name: "editor at 1x4", state: stateReady, focus: focusEditor, width: 1, height: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", Open(context.Background()))
			model.state, model.focus = test.state, test.focus

			// When
			updated, _ := model.Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			model = updated.(Model)
			view := model.View()

			// Then
			if model.schemaWidth < 0 || model.editorWidth < 0 || model.editorHeight < 0 || model.resultsHeight < 0 {
				t.Fatalf("negative layout dimensions: schema=%d editor=%d editorHeight=%d resultsHeight=%d", model.schemaWidth, model.editorWidth, model.editorHeight, model.resultsHeight)
			}
			if view.Content == "" {
				t.Fatal("view content is empty")
			}
		})
	}
}

func TestResize_short_wide_terminal_uses_compact_single_pane(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.state = stateReady

	// When
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 5})
	model = updated.(Model)
	view := model.View()

	// Then
	if !model.compact {
		t.Fatal("100x5 terminal used the wide layout")
	}
	if view.Content == "" {
		t.Fatal("compact view content is empty")
	}
	if strings.Contains(view.Content, "Results") {
		t.Fatal("compact editor pane rendered the results pane")
	}
	if got := lipgloss.Height(view.Content); got > 5 {
		t.Fatalf("compact view height = %d, want at most 5", got)
	}
	if !strings.Contains(view.Content, "BUBBLE WORKBENCH") || !strings.Contains(view.Content, "q quit") {
		t.Fatal("compact view does not retain header and footer")
	}
}
