package chat

import (
	"testing"

	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

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
