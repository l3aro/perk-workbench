package app

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

const (
	aiWizardMenu = iota
	aiWizardProviders
	aiWizardProviderForm
	aiWizardAgents
	aiWizardAgentForm
)

const (
	aiWizardNoAction = iota
	aiWizardSave
	aiWizardClose
)

var canonicalAIAgents = []string{"assistant", "oracle", "spark"}

// aiWizard edits exactly one layer: the user AI file. Lists deliberately only
// expose the canonical routed agents; unrelated user entries remain in config.
type aiWizard struct {
	form        *huh.Form
	existing    ai.Config
	error       string
	width       int
	screen      int
	cursor      int
	providerIDs []string

	editingProvider     string
	editingAgent        string
	providerSnapshot    ai.Provider
	agentSnapshot       ai.Agent
	providerAdding      bool
	agentAdding         bool
	providerDraftActive bool

	// Provider form values. These names are retained for compatibility with
	// callers/tests that inspect the initial wizard defaults.
	providerID   string
	providerName string
	api          string
	baseURL      string
	apiKey       string
	models       string

	// Generic agent form values; assistant aliases retain compatibility with
	// the original wizard's tests and are synchronized when assistant opens.
	agentName      string
	agentProvider  string
	agentModel     string
	systemPrompt   string
	assistantName  string
	assistantAgent string
	assistantModel string
}

func (m *Model) SetAIConfig(path string, load func() (chat.Client, error)) {
	m.aiConfigPath = path
	m.aiConfigLoader = load
}

func (m Model) userAIPath() string {
	if path := strings.TrimSpace(m.aiConfigPath); path != "" {
		return path
	}
	user, _, err := ai.DefaultPaths()
	if err != nil {
		return ""
	}
	return user
}

func (m *Model) openAIWizard() tea.Cmd {
	path := m.userAIPath()
	config := ai.Config{Providers: map[string]ai.Provider{}, Agents: map[string]ai.Agent{}}
	var loadErr error
	if path != "" {
		// The wizard edits only the user layer. The effective loader used on
		// activation remains responsible for applying project overrides.
		config, loadErr = ai.LoadFile(path)
		if config.Providers == nil {
			config.Providers = map[string]ai.Provider{}
		}
		if config.Agents == nil {
			config.Agents = map[string]ai.Agent{}
		}
	}
	wizard := newAIWizard(config, m.layout.width)
	if loadErr != nil {
		wizard.error = "could not load existing AI configuration: " + loadErr.Error()
	}
	m.overlay.aiWizard = wizard
	return nil
}

func newAIWizard(existing ai.Config, width int) *aiWizard {
	if existing.Providers == nil {
		existing.Providers = map[string]ai.Provider{}
	}
	if existing.Agents == nil {
		existing.Agents = map[string]ai.Agent{}
	}
	wizard := &aiWizard{
		existing:            existing,
		width:               max(width-8, 1),
		screen:              aiWizardMenu,
		providerID:          "openai",
		providerName:        "OpenAI",
		api:                 string(ai.APIOpenAI),
		baseURL:             "https://api.openai.com/v1",
		apiKey:              "env:OPENAI_API_KEY",
		models:              "gpt-4o-mini",
		assistantName:       "Assistant",
		assistantAgent:      "openai",
		assistantModel:      "gpt-4o-mini",
		providerAdding:      true,
		agentAdding:         true,
		providerDraftActive: true,
	}
	wizard.refreshProviders()
	wizard.applyExisting()
	return wizard
}

func (w *aiWizard) refreshProviders() {
	w.providerIDs = w.providerIDs[:0]
	for id := range w.existing.Providers {
		w.providerIDs = append(w.providerIDs, id)
	}
	sort.Strings(w.providerIDs)
	if w.cursor >= len(w.providerIDs)+2 {
		w.cursor = max(len(w.providerIDs)+1, 0)
	}
}

// applyExisting seeds the compatibility fields and defaults from the user's
// existing assistant/provider without importing project-layer values.
func (w *aiWizard) applyExisting() {
	providerID := w.assistantAgent
	if assistant, ok := w.existing.Agents["assistant"]; ok {
		w.assistantName = assistant.Name
		w.assistantAgent = assistant.Provider
		w.assistantModel = assistant.Model
		w.systemPrompt = assistant.SystemPrompt
		providerID = assistant.Provider
	}
	provider, ok := w.existing.Providers[providerID]
	if !ok {
		for id, candidate := range w.existing.Providers {
			providerID, provider, ok = id, candidate, true
			break
		}
	}
	if !ok {
		return
	}
	w.providerID = providerID
	w.providerName = provider.Name
	w.api = string(provider.API)
	w.baseURL = provider.BaseURL
	w.apiKey = provider.APIKey
	w.models = strings.Join(provider.Models, ", ")
	if _, assistantExists := w.existing.Agents["assistant"]; !assistantExists || w.assistantAgent == "" {
		w.assistantAgent = providerID
	}
	if (w.assistantModel == "" || w.assistantModel == "gpt-4o-mini") && len(provider.Models) > 0 {
		w.assistantModel = provider.Models[0]
	}
}

func (w *aiWizard) startProviderForm(id string, adding bool) {
	w.screen = aiWizardProviderForm
	w.editingProvider, w.providerAdding = id, adding
	w.providerDraftActive = true
	if adding {
		w.providerID = w.nextProviderID()
		w.providerName = "New provider"
		w.api = string(ai.APIOpenAI)
		w.baseURL = "https://api.openai.com/v1"
		w.apiKey = "env:OPENAI_API_KEY"
		w.models = "gpt-4o-mini"
		w.providerSnapshot = ai.Provider{}
	} else {
		provider := w.existing.Providers[id]
		w.providerSnapshot = provider
		w.providerID, w.providerName = id, provider.Name
		w.api, w.baseURL, w.apiKey = string(provider.API), provider.BaseURL, provider.APIKey
		w.models = strings.Join(provider.Models, ", ")
	}
	w.form = w.providerForm()
}

func (w *aiWizard) nextProviderID() string {
	for i := 1; ; i++ {
		id := fmt.Sprintf("provider-%d", i)
		if _, exists := w.existing.Providers[id]; !exists {
			return id
		}
	}
}

func (w *aiWizard) providerForm() *huh.Form {
	apiOptions := []huh.Option[string]{
		huh.NewOption("OpenAI", string(ai.APIOpenAI)),
		huh.NewOption("Anthropic", string(ai.APIAnthropic)),
		huh.NewOption("Gemini", string(ai.APIGemini)),
		huh.NewOption("OpenAI-compatible", string(ai.APIOpenAICompatible)),
	}
	form := uikit.NewForm(huh.NewGroup(
		uikit.NewEditableInput(huh.NewInput().Key("provider_id").Title("Provider ID").Value(&w.providerID).Validate(w.validateProviderID), &w.providerID),
		uikit.NewEditableInput(huh.NewInput().Key("provider_name").Title("Display name").Value(&w.providerName).Validate(requiredValue("provider display name")), &w.providerName),
		huh.NewSelect[string]().Key("api").Title("API").Options(apiOptions...).Value(&w.api),
		uikit.NewEditableInput(huh.NewInput().Key("base_url").Title("Base URL").Placeholder("https://api.openai.com/v1").Value(&w.baseURL).Validate(requiredValue("base URL")), &w.baseURL),
		uikit.NewEditableInput(huh.NewInput().Key("api_key").Title("API key or env reference").Description("Prefer env:NAME so the secret stays out of this file.").Placeholder("env:OPENAI_API_KEY").EchoMode(huh.EchoModePassword).Value(&w.apiKey).Validate(requiredValue("API key")), &w.apiKey),
		uikit.NewEditableInput(huh.NewInput().Key("models").Title("Models").Placeholder("gpt-4o-mini, ...").Value(&w.models).Validate(validateModels), &w.models),
	).Title("Provider")).WithShowHelp(w.width >= 40).WithWidth(max(w.width, 1)).WithHeight(max(w.width/2, 1))
	_ = form.Init()
	return form
}

func (w *aiWizard) startAgentForm(id string) {
	w.editingAgent = id
	w.agentName, w.agentProvider, w.agentModel, w.systemPrompt = agentDefaults(id, w.existing)
	if existing, ok := w.existing.Agents[id]; ok {
		w.agentSnapshot = existing
		w.agentName, w.agentProvider, w.agentModel, w.systemPrompt = existing.Name, existing.Provider, existing.Model, existing.SystemPrompt
	} else {
		w.agentSnapshot = ai.Agent{}
		w.agentAdding = true
	}
	if id == "assistant" {
		w.assistantName, w.assistantAgent, w.assistantModel = w.agentName, w.agentProvider, w.agentModel
	}
	w.form = uikit.NewForm(huh.NewGroup(
		uikit.NewEditableInput(huh.NewInput().Key("agent_name").Title("Display name").Value(&w.agentName).Validate(requiredValue("agent display name")), &w.agentName),
		uikit.NewEditableInput(huh.NewInput().Key("agent_provider").Title("Provider ID").Value(&w.agentProvider).Validate(requiredValue("agent provider")), &w.agentProvider),
		uikit.NewEditableInput(huh.NewInput().Key("agent_model").Title("Model").Value(&w.agentModel).Validate(requiredValue("agent model")), &w.agentModel),
		uikit.NewEditableInput(huh.NewInput().Key("system_prompt").Title("System prompt").Value(&w.systemPrompt).Validate(requiredValue("system prompt")), &w.systemPrompt),
	).Title("Agent — " + id)).WithShowHelp(w.width >= 40).WithWidth(max(w.width, 1)).WithHeight(max(w.width/2, 1))
	_ = w.form.Init()
}

const defaultAgentSystemPrompt = "You are a careful database assistant. Explain your reasoning and write safe SQL.\n\n" +
	"Always render tables as GitHub-Flavored Markdown. Include a header row, a hyphen separator row, consistent column counts, and blank lines before and after the table. Never place Markdown tables inside code fences. Escape literal pipe characters in cell contents.\n\n" +
	"When displaying SQL query results, convert the result into a Markdown table instead of copying the tool’s raw pipe-separated output."

func agentDefaults(id string, config ai.Config) (string, string, string, string) {
	name := strings.ToUpper(id[:1]) + id[1:]
	provider, model := "", ""
	providerIDs := make([]string, 0, len(config.Providers))
	for providerID := range config.Providers {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)
	if len(providerIDs) > 0 {
		provider = providerIDs[0]
		model = firstModel(config.Providers[provider].Models)
	}
	return name, provider, model, defaultAgentSystemPrompt
}

func firstModel(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func (w *aiWizard) commitProvider() {
	id := strings.TrimSpace(w.providerID)
	provider := ai.Provider{Name: strings.TrimSpace(w.providerName), API: ai.API(strings.TrimSpace(w.api)), BaseURL: strings.TrimSpace(w.baseURL), APIKey: strings.TrimSpace(w.apiKey), Models: parseModels(w.models)}
	if w.editingProvider != "" && w.editingProvider != id {
		delete(w.existing.Providers, w.editingProvider)
		for agentID, agent := range w.existing.Agents {
			if agent.Provider == w.editingProvider {
				agent.Provider = id
				w.existing.Agents[agentID] = agent
			}
		}
	}
	w.existing.Providers[id] = provider
	w.editingProvider = id
	w.providerAdding = false
	w.refreshProviders()
	w.providerID, w.providerName = id, provider.Name
	w.api, w.baseURL, w.apiKey, w.models = string(provider.API), provider.BaseURL, provider.APIKey, strings.Join(provider.Models, ", ")
}

func (w *aiWizard) commitAgent() {
	id := w.editingAgent
	w.existing.Agents[id] = ai.Agent{Name: strings.TrimSpace(w.agentName), Provider: strings.TrimSpace(w.agentProvider), Model: strings.TrimSpace(w.agentModel), SystemPrompt: w.systemPrompt}
	if id == "assistant" {
		w.assistantName, w.assistantAgent, w.assistantModel = w.agentName, w.agentProvider, w.agentModel
	}
	w.agentAdding = false
}

func (w *aiWizard) cancelForm() {
	if w.screen == aiWizardProviderForm {
		if w.providerAdding {
			w.providerDraftActive = len(w.existing.Providers) == 0
			if w.providerDraftActive {
				w.providerID, w.providerName = "openai", "OpenAI"
				w.api, w.baseURL, w.apiKey, w.models = string(ai.APIOpenAI), "https://api.openai.com/v1", "env:OPENAI_API_KEY", "gpt-4o-mini"
			}
		} else {
			w.providerID, w.providerName = w.editingProvider, w.providerSnapshot.Name
			w.api, w.baseURL, w.apiKey = string(w.providerSnapshot.API), w.providerSnapshot.BaseURL, w.providerSnapshot.APIKey
			w.models = strings.Join(w.providerSnapshot.Models, ", ")
		}
		w.screen, w.form = aiWizardProviders, nil
	} else {
		if w.agentAdding {
			w.agentName, w.agentProvider, w.agentModel, w.systemPrompt = "", "", "", ""
		} else {
			w.agentName, w.agentProvider, w.agentModel, w.systemPrompt = w.agentSnapshot.Name, w.agentSnapshot.Provider, w.agentSnapshot.Model, w.agentSnapshot.SystemPrompt
		}
		w.screen, w.form = aiWizardAgents, nil
	}
	w.error = ""
	w.cursor = 0
}

func (w *aiWizard) update(message tea.Msg) (int, tea.Cmd) {
	if w.form != nil {
		if key, ok := message.(tea.KeyPressMsg); ok && key.Key().Code == tea.KeyEscape {
			w.cancelForm()
			return aiWizardNoAction, nil
		}
		form, command := w.form.Update(message)
		w.form = form.(*huh.Form)
		if w.form.State == huh.StateCompleted {
			if w.screen == aiWizardProviderForm {
				w.commitProvider()
				w.screen = aiWizardProviders
			} else {
				w.commitAgent()
				w.screen = aiWizardAgents
			}
			w.form = nil
			w.error = ""
			w.cursor = 0
		}
		return aiWizardNoAction, command
	}
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return aiWizardNoAction, nil
	}
	move := func(delta, maxValue int) { w.cursor = max(0, min(w.cursor+delta, maxValue)) }
	switch w.screen {
	case aiWizardMenu:
		switch key.Key().Code {
		case tea.KeyEscape:
			return aiWizardClose, nil
		case tea.KeyEnter:
			switch w.cursor {
			case 0:
				w.screen, w.cursor = aiWizardProviders, 0
			case 1:
				w.screen, w.cursor = aiWizardAgents, 0
			case 2:
				return aiWizardSave, nil
			default:
				return aiWizardClose, nil
			}
		case tea.KeyUp:
			move(-1, 3)
		case tea.KeyDown:
			move(1, 3)
		default:
			switch key.Key().Keystroke() {
			case "j":
				move(1, 3)
			case "k":
				move(-1, 3)
			}
		}
	case aiWizardProviders:
		maxValue := len(w.providerIDs) + 1
		switch key.Key().Code {
		case tea.KeyEscape:
			w.screen, w.cursor = aiWizardMenu, 0
		case tea.KeyEnter:
			switch {
			case w.cursor < len(w.providerIDs):
				w.startProviderForm(w.providerIDs[w.cursor], false)
			case w.cursor == len(w.providerIDs):
				w.startProviderForm("", true)
			default:
				w.screen, w.cursor = aiWizardMenu, 0
			}
		case tea.KeyDelete, tea.KeyBackspace:
			if w.cursor < len(w.providerIDs) {
				w.removeProvider(w.providerIDs[w.cursor])
				w.cursor = min(w.cursor, max(len(w.providerIDs)-1, 0))
			}
		case tea.KeyUp:
			move(-1, maxValue)
		case tea.KeyDown:
			move(1, maxValue)
		default:
			switch key.Key().Keystroke() {
			case "j":
				move(1, maxValue)
			case "k":
				move(-1, maxValue)
			case "a":
				w.startProviderForm("", true)
			case "d":
				if w.cursor < len(w.providerIDs) {
					w.removeProvider(w.providerIDs[w.cursor])
				}
			}
		}
	case aiWizardAgents:
		maxValue := len(canonicalAIAgents)
		switch key.Key().Code {
		case tea.KeyEscape:
			w.screen, w.cursor = aiWizardMenu, 0
		case tea.KeyEnter:
			if w.cursor < len(canonicalAIAgents) {
				w.startAgentForm(canonicalAIAgents[w.cursor])
			} else {
				w.screen, w.cursor = aiWizardMenu, 0
			}
		case tea.KeyDelete, tea.KeyBackspace:
			w.removeAgent()
		case tea.KeyUp:
			move(-1, maxValue)
		case tea.KeyDown:
			move(1, maxValue)
		default:
			switch key.Key().Keystroke() {
			case "j":
				move(1, maxValue)
			case "k":
				move(-1, maxValue)
			case "d":
				w.removeAgent()
			}
		}
	}
	return aiWizardNoAction, nil
}
func (w *aiWizard) removeProvider(id string) {
	for agentID, agent := range w.existing.Agents {
		if agent.Provider == id {
			w.error = fmt.Sprintf("provider %q is used by agent %q", id, agentID)
			return
		}
	}
	delete(w.existing.Providers, id)
	if w.providerID == id {
		w.providerID, w.providerName, w.api, w.baseURL, w.apiKey, w.models = "", "", "", "", "", ""
		w.assistantAgent = ""
		w.providerDraftActive = false
	}
	w.refreshProviders()
	w.error = ""
}

func (w *aiWizard) removeAgent() {
	if w.cursor >= len(canonicalAIAgents) {
		return
	}
	id := canonicalAIAgents[w.cursor]
	if id == "assistant" {
		w.error = "assistant is required and cannot be removed"
		return
	}
	delete(w.existing.Agents, id)
	w.error = ""
}

func (w *aiWizard) validateProviderID(value string) error {
	id := strings.TrimSpace(value)
	if id == "" {
		return fmt.Errorf("provider ID is required")
	}
	if id != w.editingProvider {
		if _, exists := w.existing.Providers[id]; exists {
			return fmt.Errorf("provider %q already exists", id)
		}
	}
	return nil
}

func requiredValue(name string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}

func validateModels(value string) error {
	if len(parseModels(value)) == 0 {
		return fmt.Errorf("at least one model is required")
	}
	return nil
}

func parseModels(value string) []string {
	parts := strings.Split(value, ",")
	models := make([]string, 0, len(parts))
	for _, part := range parts {
		if model := strings.TrimSpace(part); model != "" {
			models = append(models, model)
		}
	}
	return models
}

func (w *aiWizard) setWidth(width int) {
	w.width = max(width-8, 1)
	if w.form != nil {
		w.form.WithShowHelp(w.width >= 40).WithWidth(w.width).WithHeight(max(w.width/2, 1))
	}
}

func (w *aiWizard) viewContent() string {
	if w.form != nil {
		return w.form.View() + w.errorSuffix()
	}
	var content strings.Builder
	switch w.screen {
	case aiWizardProviders:
		content.WriteString(headerStyle.Render(" AI providers (user layer) "))
		content.WriteString("\n\n")
		for i, id := range w.providerIDs {
			label := id + " — " + w.existing.Providers[id].Name
			if i == w.cursor {
				label = selectedItemStyle.Render(label)
				content.WriteString("> ")
			} else {
				content.WriteString("  ")
			}
			content.WriteString(label + "\n")
		}
		content.WriteString("\n  Add provider\n  Back\n\n")
		content.WriteString(mutedStyle.Render(" enter edit/select • a add • d delete • esc back"))
	case aiWizardAgents:
		content.WriteString(headerStyle.Render(" AI agents (user layer) "))
		content.WriteString("\n\n")
		for i, id := range canonicalAIAgents {
			label := id
			if agent, ok := w.existing.Agents[id]; ok {
				label += " — " + agent.Name
			} else {
				label += " (not configured)"
			}
			if i == w.cursor {
				label = selectedItemStyle.Render(label)
				content.WriteString("> ")
			} else {
				content.WriteString("  ")
			}
			content.WriteString(label + "\n")
		}
		content.WriteString("\n  Back\n\n")
		content.WriteString(mutedStyle.Render(" enter add/edit • d delete oracle/spark • esc back"))
	default:
		content.WriteString(headerStyle.Render(" AI configuration "))
		content.WriteString("\n\n")
		for i, option := range []string{"Providers", "Agents", "Save and activate", "Cancel"} {
			label := option
			if i == w.cursor {
				label = selectedItemStyle.Render(label)
				content.WriteString("> ")
			} else {
				content.WriteString("  ")
			}
			content.WriteString(label + "\n")
		}
		content.WriteString("\n")
		content.WriteString(mutedStyle.Render(" arrows or j/k navigate • enter select • esc close"))
	}
	return content.String() + w.errorSuffix()
}

func (w *aiWizard) errorSuffix() string {
	if w.error == "" {
		return ""
	}
	return "\n\n" + statusFailedStyle.Render("Error: "+w.error)
}
func (w *aiWizard) view() string { return w.viewContent() }

func (w *aiWizard) config() (ai.Config, error) {
	config := ai.Config{Providers: maps.Clone(w.existing.Providers), Agents: maps.Clone(w.existing.Agents)}
	if config.Providers == nil {
		config.Providers = map[string]ai.Provider{}
	}
	if config.Agents == nil {
		config.Agents = map[string]ai.Agent{}
	}
	// A fresh wizard still offers a useful first provider/assistant pair for
	// the existing direct-save API. Normal UI edits commit values before
	// reaching this method; an active draft also supports direct callers.
	if w.providerDraftActive && strings.TrimSpace(w.providerID) != "" {
		config.Providers[strings.TrimSpace(w.providerID)] = ai.Provider{Name: strings.TrimSpace(w.providerName), API: ai.API(strings.TrimSpace(w.api)), BaseURL: strings.TrimSpace(w.baseURL), APIKey: strings.TrimSpace(w.apiKey), Models: parseModels(w.models)}
	}
	if _, ok := config.Agents["assistant"]; !ok && strings.TrimSpace(w.assistantAgent) != "" {
		config.Agents["assistant"] = ai.Agent{Name: strings.TrimSpace(w.assistantName), Provider: strings.TrimSpace(w.assistantAgent), Model: strings.TrimSpace(w.assistantModel), SystemPrompt: w.systemPrompt}
	}
	if err := config.Validate(); err != nil {
		return ai.Config{}, err
	}
	return config, nil
}

func (m *Model) saveAIWizard() tea.Cmd {
	wizard := m.overlay.aiWizard
	if wizard == nil {
		return nil
	}
	config, err := wizard.config()
	if err == nil {
		path := m.userAIPath()
		if path == "" {
			err = fmt.Errorf("unable to determine user AI configuration path")
		} else {
			err = ai.Save(path, config)
		}
	}
	if err != nil {
		wizard.error = err.Error()
		return nil
	}
	wizard.existing = config
	wizard.refreshProviders()
	wizard.error = ""
	if m.aiConfigLoader != nil {
		client, loadErr := m.aiConfigLoader()
		if loadErr != nil {
			wizard.error = "saved configuration but could not activate it: " + loadErr.Error()
			return nil
		}
		m.SetAI(client, m.chat.component.History)
	}
	m.overlay.aiWizard = nil
	m.setStatus("AI configured")
	return nil
}
