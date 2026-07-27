package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type API string

const (
	APIOpenAI           API = "openai"
	APIAnthropic        API = "anthropic"
	APIGemini           API = "gemini"
	APIOpenAICompatible API = "openai-compatible"
)

type Provider struct {
	Name    string   `json:"name"`
	API     API      `json:"api"`
	BaseURL string   `json:"base_url"`
	APIKey  string   `json:"api_key"`
	Models  []string `json:"models"`
}

type Agent struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
}

type Config struct {
	Providers map[string]Provider `json:"providers"`
	Agents    map[string]Agent    `json:"agents"`
}

func DefaultPaths() (user, project string, err error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", "", fmt.Errorf("locating user config directory: %w", err)
	}
	return filepath.Join(dir, "perk-workbench", "ai.json"), filepath.Join(".perk-workbench", "ai.json"), nil
}

func Load() (Config, error) {
	user, project, err := DefaultPaths()
	if err != nil {
		return Config{}, err
	}
	return LoadFiles(user, project)
}

func LoadFiles(userPath, projectPath string) (Config, error) {
	user, err := readConfig(userPath)
	if err != nil {
		return Config{}, err
	}
	project, err := readConfig(projectPath)
	if err != nil {
		return Config{}, err
	}
	config := Config{Providers: maps.Clone(user.Providers), Agents: maps.Clone(user.Agents)}
	if config.Providers == nil {
		config.Providers = map[string]Provider{}
	}
	if config.Agents == nil {
		config.Agents = map[string]Agent{}
	}
	maps.Copy(config.Providers, project.Providers)
	maps.Copy(config.Agents, project.Agents)
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func readConfig(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading AI config %q: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("parsing AI config %q: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Config{}, fmt.Errorf("parsing AI config %q: multiple JSON values", path)
	}
	return config, nil
}

func (c Config) Validate() error {
	if len(c.Providers) == 0 && len(c.Agents) == 0 {
		return nil
	}
	for id, provider := range c.Providers {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(provider.Name) == "" || strings.TrimSpace(provider.BaseURL) == "" || strings.TrimSpace(provider.APIKey) == "" || len(provider.Models) == 0 {
			return fmt.Errorf("provider %q is incomplete", id)
		}
		switch provider.API {
		case APIOpenAI, APIAnthropic, APIGemini, APIOpenAICompatible:
		default:
			if strings.HasPrefix(string(provider.API), "env:") {
				continue
			}
			return fmt.Errorf("provider %q has unsupported API %q", id, provider.API)
		}
	}
	for id, agent := range c.Agents {
		provider, ok := c.Providers[agent.Provider]
		if strings.TrimSpace(id) == "" || strings.TrimSpace(agent.Name) == "" || !ok || !configuredModel(provider.Models, agent.Model) {
			return fmt.Errorf("agent %q has an unknown provider or model", id)
		}
	}
	return nil
}

func configuredModel(models []string, model string) bool {
	if slices.Contains(models, model) {
		return true
	}
	if strings.HasPrefix(model, "env:") {
		return true
	}
	for _, candidate := range models {
		if strings.HasPrefix(candidate, "env:") {
			return true
		}
	}
	return false
}

func ResolveValue(value string) (string, error) {
	if !strings.HasPrefix(value, "env:") {
		return value, nil
	}
	name := strings.TrimPrefix(value, "env:")
	if name == "" {
		return "", fmt.Errorf("empty environment variable reference")
	}
	resolved, ok := os.LookupEnv(name)
	if !ok || resolved == "" {
		return "", fmt.Errorf("environment variable %q is not set", name)
	}
	return resolved, nil
}
