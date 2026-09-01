package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

func TestTermRenderer_usesThemeCompatibleStyleAndContrast(t *testing.T) {
	previous := uikit.IsLightTheme
	t.Cleanup(func() { uikit.IsLightTheme = previous })

	rendered := make(map[bool]string, 2)
	for _, light := range []bool{false, true} {
		uikit.IsLightTheme = light
		if got, want := termRendererStyle(), map[bool]string{false: "dark", true: "light"}[light]; got != want {
			t.Fatalf("termRendererStyle(%t) = %q, want %q", light, got, want)
		}
		renderer, err := newTermRenderer(80)
		if err != nil {
			t.Fatalf("newTermRenderer(%t): %v", light, err)
		}
		rendered[light], err = renderer.Render("Readable assistant response.")
		if err != nil {
			t.Fatalf("Render(%t): %v", light, err)
		}
	}
	if rendered[false] == rendered[true] {
		t.Fatal("light and dark assistant responses rendered identically")
	}
}

func TestRenderContent_rebuildsRendererAndInvalidatesAllRunCachesOnThemeChange(t *testing.T) {
	previous := uikit.IsLightTheme
	t.Cleanup(func() { uikit.IsLightTheme = previous })

	uikit.IsLightTheme = false
	model := New()
	model.Resize(uikit.Layout{Width: 80, Height: 20})
	active := model.ActiveRun()
	other := &Run{
		BlockCache: []Block{{Block: "dark cache"}},
		stream:     streamCache{SourcePrefix: "dark stream"},
	}
	model.Runs["other"] = other
	active.BlockCache = []Block{{Block: "dark cache"}}
	active.stream = streamCache{SourcePrefix: "dark stream"}

	uikit.IsLightTheme = true
	rendered := model.RenderContent("Readable assistant response.")
	if len(active.BlockCache) != 0 || len(other.BlockCache) != 0 {
		t.Fatal("theme change retained rendered blocks from the previous style")
	}
	if active.stream.SourcePrefix != "" || other.stream.SourcePrefix != "" {
		t.Fatal("theme change retained streaming cache from the previous style")
	}
	if !strings.Contains(ansi.Strip(rendered), "Readable assistant response.") {
		t.Fatalf("light response = %q, want response text", rendered)
	}
}

func TestRefreshViewInvalidatesChangedMiddleAndRole(t *testing.T) {
	model := New()
	model.Resize(uikit.Layout{Width: 80, Height: 20})
	run := model.ActiveRun()
	run.Messages = []ai.Message{
		{Role: ai.RoleUser, Content: "question"},
		{Role: ai.RoleAssistant, Content: "first answer"},
		{Role: ai.RoleTool, Content: "tool result"},
	}
	model.RefreshView()
	first := run.BlockCache[0].Block

	run.Messages[1].Content = "changed answer"
	model.RefreshView()
	if len(run.BlockCache) != 3 || run.BlockCache[0].Block != first {
		t.Fatalf("middle replacement did not preserve equal front prefix")
	}
	if run.BlockCache[1].Source.Content != "changed answer" {
		t.Fatalf("middle replacement retained stale source: %#v", run.BlockCache[1].Source)
	}

	run.Messages[1].Role = ai.RoleUser
	model.RefreshView()
	if run.BlockCache[1].Source.Role != ai.RoleUser {
		t.Fatalf("role-only replacement retained stale role: %#v", run.BlockCache[1].Source)
	}
	if run.BlockCache[1].Block != model.MessageBlock(run.Messages[1]) {
		t.Fatalf("role-only replacement did not rerender block")
	}
}

func TestStreamUnsafeMarkdownFallsBackToWholeBuffer(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "unfinished table", content: "intro\n\n| Name |\n| --- |"},
		{name: "unfinished fence", content: "intro\n\n```sql\nSELECT 1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			model := New()
			model.Resize(uikit.Layout{Width: 80, Height: 20})
			run := model.ActiveRun()
			run.Loading = true
			run.StreamBuffer = testCase.content
			model.RefreshView()
			if got, want := run.stream.TailRendered, model.StreamBlock(testCase.content); got != want {
				t.Fatalf("unsafe stream render = %q, want whole-buffer render %q", got, want)
			}
			if run.stream.SourcePrefix != "" {
				t.Fatalf("unsafe stream retained a stable prefix: %q", run.stream.SourcePrefix)
			}
		})
	}
}
