package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
)

func TestAIWizard_reopensExistingUserConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perk-workbench", "ai.json")
	contents := `{"providers":{"cloud":{"name":"Cloud","api":"openai-compatible","base_url":"https://example.test/v1","api_key":"env:CLOUD_KEY","models":["small"]}},"agents":{"assistant":{"name":"Assistant","provider":"cloud","model":"small","system_prompt":"Be precise."}}}`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	model := New("", context.Background(), nil, false)
	model.aiConfigPath = path
	model.openAIWizard()

	wizard := model.overlay.aiWizard
	if wizard == nil {
		t.Fatal("AI wizard is not open")
	}
	if wizard.providerID != "cloud" || wizard.providerName != "Cloud" || wizard.apiKey != "env:CLOUD_KEY" {
		t.Fatalf("provider fields = (%q, %q, %q), want existing values", wizard.providerID, wizard.providerName, wizard.apiKey)
	}
	if wizard.assistantModel != "small" || wizard.systemPrompt != "Be precise." {
		t.Fatalf("assistant fields = (%q, %q), want existing values", wizard.assistantModel, wizard.systemPrompt)
	}
	if wizard.error != "" {
		t.Fatalf("reopening existing config failed: %s", wizard.error)
	}
}

func TestAIWizard_prefillsProviderOnlyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	contents := `{"providers":{"cloud":{"name":"Cloud","api":"openai","base_url":"https://example.test/v1","api_key":"env:CLOUD_KEY","models":["small"]}}}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	model := New("", context.Background(), nil, false)
	model.aiConfigPath = path
	model.openAIWizard()

	wizard := model.overlay.aiWizard
	if wizard == nil {
		t.Fatal("AI wizard is not open")
	}
	if wizard.providerID != "cloud" || wizard.assistantAgent != "cloud" || wizard.assistantModel != "small" {
		t.Fatalf("wizard fields = (%q, %q, %q), want provider-only defaults", wizard.providerID, wizard.assistantAgent, wizard.assistantModel)
	}
}

func TestAIWizard_newAgentUsesMarkdownTableSystemPrompt(t *testing.T) {
	wizard := newAIWizard(ai.Config{
		Providers: map[string]ai.Provider{
			"cloud": {Models: []string{"small"}},
		},
	}, 120)

	wizard.startAgentForm("spark")

	want := `You are a careful database assistant. Explain your reasoning and write safe SQL.

Always render tables as GitHub-Flavored Markdown. Include a header row, a hyphen separator row, consistent column counts, and blank lines before and after the table. Never place Markdown tables inside code fences. Escape literal pipe characters in cell contents.

When displaying SQL query results, convert the result into a Markdown table instead of copying the tool’s raw pipe-separated output.`
	if wizard.systemPrompt != want {
		t.Fatalf("new agent system prompt = %q, want %q", wizard.systemPrompt, want)
	}
}

func TestAIWizard_savesAndActivatesClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	model := New("", context.Background(), nil, false)
	model.State = stateReady
	model.applyLayout(140, 32)
	model.SetAIConfig(path, func() (chat.Client, error) {
		return fakeChatClient{}, nil
	})
	model.overlay.aiWizard = newAIWizard(ai.Config{}, 120)

	if cmd := model.saveAIWizard(); cmd != nil {
		t.Fatal("saveAIWizard returned an unexpected command")
	}
	if model.overlay.aiWizard != nil {
		t.Fatalf("AI wizard remains open after save: %s", model.overlay.aiWizard.error)
	}
	if !model.chat.component.Enabled || model.chat.component.Client == nil {
		t.Fatal("AI client was not activated after successful save")
	}
	if !model.chat.component.Visible {
		t.Fatal("assistant panel is not visible after successful configuration")
	}
	if model.layout.chatWidth != chatPaneWidth {
		t.Fatalf("chat pane width after configuration = %d, want %d", model.layout.chatWidth, chatPaneWidth)
	}
	if model.layout.editorWidth >= 140 {
		t.Fatalf("editor width after configuration = %d, want space reserved for chat", model.layout.editorWidth)
	}
	if _, err := ai.LoadFiles(path, filepath.Join(t.TempDir(), "missing.json")); err != nil {
		t.Fatalf("saved AI config is invalid: %v", err)
	}

	// Configure AI is completed after New has cached the command palette.
	// The TUI must refresh that cache so both assistant commands are
	// immediately discoverable without restarting the process.
	commands := make(map[CommandID]bool)
	for _, item := range model.overlay.commandPalette.items {
		commands[item.id] = true
	}
	if !commands["ai.toggle"] || !commands["focus.chat"] {
		t.Fatalf("post-save command palette = %#v, want ai.toggle and focus.chat", commands)
	}

	// Exercise the actual keyboard paths users use after leaving the wizard:
	// Ctrl+G hides and reopens the pane, then 4 focuses the assistant.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl, Text: "g"})
	model = updated.(Model)
	if model.chat.component.Visible {
		t.Fatal("assistant panel remains visible after Ctrl+G toggle")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl, Text: "g"})
	model = updated.(Model)
	if !model.chat.component.Visible {
		t.Fatal("assistant panel could not be reopened after Ctrl+G toggle")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	model = updated.(Model)
	if model.Focus != focusChat {
		t.Fatalf("focus after Configure AI = %v, want chat", model.Focus)
	}
}

func TestAIWizard_managesMultipleProvidersAndCanonicalAgents(t *testing.T) {
	config := ai.Config{
		Providers: map[string]ai.Provider{
			"cloud": {Name: "Cloud", API: ai.APIOpenAI, BaseURL: "https://cloud.example/v1", APIKey: "env:CLOUD_KEY", Models: []string{"large"}},
			"local": {Name: "Local", API: ai.APIOpenAICompatible, BaseURL: "http://localhost:11434/v1", APIKey: "local", Models: []string{"small"}},
		},
		Agents: map[string]ai.Agent{
			"assistant": {Name: "Assistant", Provider: "cloud", Model: "large"},
			"oracle":    {Name: "Oracle", Provider: "local", Model: "small"},
			"custom":    {Name: "Custom", Provider: "cloud", Model: "large"},
		},
	}
	wizard := newAIWizard(config, 120)

	if got, want := wizard.providerIDs, []string{"cloud", "local"}; !slices.Equal(got, want) {
		t.Fatalf("provider IDs = %#v, want %#v", got, want)
	}
	if got, want := canonicalAIAgents, []string{"assistant", "oracle", "spark"}; !slices.Equal(got, want) {
		t.Fatalf("canonical agents = %#v, want %#v", got, want)
	}

	wizard.startProviderForm("local", false)
	if wizard.providerName != "Local" || wizard.models != "small" {
		t.Fatalf("local provider draft = (%q, %q), want existing values", wizard.providerName, wizard.models)
	}
	if wizard.form == nil {
		t.Fatal("provider form was not initialized")
	}

	wizard.startAgentForm("oracle")
	if wizard.agentName != "Oracle" || wizard.agentProvider != "local" || wizard.agentModel != "small" {
		t.Fatalf("oracle draft = (%q, %q, %q), want existing values", wizard.agentName, wizard.agentProvider, wizard.agentModel)
	}
	if _, ok := wizard.existing.Agents["custom"]; !ok {
		t.Fatal("unmanaged user agent was removed")
	}

	wizard.removeProvider("cloud")
	if _, ok := wizard.existing.Providers["cloud"]; !ok {
		t.Fatal("provider used by agents was removed")
	}
	if wizard.error == "" {
		t.Fatal("removing a referenced provider did not report an error")
	}
}

func TestAIWizard_editsAssistantThroughFormAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	contents := `{"providers":{"cloud":{"name":"Cloud","api":"openai","base_url":"https://example.test/v1","api_key":"env:CLOUD_KEY","models":["small","large"]}},"agents":{"assistant":{"name":"Old assistant","provider":"cloud","model":"small","system_prompt":"Old prompt"}}}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	model := New("", context.Background(), nil, false)
	model.aiConfigPath = path
	model.openAIWizard()

	send := func(message tea.Msg) {
		t.Helper()
		updated, command := model.Update(message)
		model = updated.(Model)
		model = driveCommand(model, command)
	}

	// Open the canonical assistant form through the wizard's menu and agents list.
	send(tea.KeyPressMsg{Code: tea.KeyDown})
	send(tea.KeyPressMsg{Code: tea.KeyEnter})
	send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.overlay.aiWizard == nil || model.overlay.aiWizard.form == nil {
		t.Fatal("assistant form did not open")
	}

	// Replace each field through the focused form input, then complete the form.
	for _, value := range []string{"Edited assistant", "cloud", "large", "Edited prompt"} {
		send(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		send(tea.PasteMsg{Content: value})
		send(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	if model.overlay.aiWizard == nil || model.overlay.aiWizard.form != nil {
		t.Fatal("assistant form did not complete")
	}

	if command := model.saveAIWizard(); command != nil {
		t.Fatal("saveAIWizard returned an unexpected command")
	}
	if model.overlay.aiWizard != nil {
		t.Fatalf("saveAIWizard failed: %s", model.overlay.aiWizard.error)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Agents map[string]ai.Agent `json:"agents"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("saved AI config is not valid JSON: %v", err)
	}
	got, ok := persisted.Agents["assistant"]
	if !ok {
		t.Fatalf("saved agents do not contain assistant: %#v", persisted.Agents)
	}
	want := ai.Agent{Name: "Edited assistant", Provider: "cloud", Model: "large", SystemPrompt: "Edited prompt"}
	if got != want {
		t.Fatalf("saved assistant = %#v, want %#v", got, want)
	}
	if _, ok := persisted.Agents[""]; ok {
		t.Fatalf("saved agents contain an empty agent key: %#v", persisted.Agents)
	}
}
