package ai

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadFiles_projectProfileReplacesUserProfile(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "ai.json")
	projectDir := t.TempDir()
	projectPath := filepath.Join(projectDir, ".perk-workbench", "ai.json")
	if err := os.Mkdir(filepath.Dir(projectPath), 0o700); err != nil {
		t.Fatal(err)
	}
	user := `{"providers":{"cloud":{"name":"User","api":"openai","base_url":"https://user.example/v1","api_key":"user","models":["small"]}},"agents":{"assistant":{"name":"Assistant","provider":"cloud","model":"small","system_prompt":"help"}}}`
	project := `{"providers":{"cloud":{"name":"Project","api":"openai-compatible","base_url":"https://project.example/v1","api_key":"env:PROJECT_KEY","models":["large"]}},"agents":{"assistant":{"name":"Assistant","provider":"cloud","model":"large","system_prompt":"project"}}}`
	if err := os.WriteFile(userPath, []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	projectBefore, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	config, err := LoadFiles(userPath, projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Providers["cloud"].Name; got != "Project" {
		t.Fatalf("provider name = %q, want project override", got)
	}
	if got := config.Agents["assistant"].Model; got != "large" {
		t.Fatalf("assistant model = %q, want project override", got)
	}
	projectAfter, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projectAfter, projectBefore) {
		t.Fatalf("project config changed from %q to %q", projectBefore, projectAfter)
	}
}

func TestLoadFile_missingReturnsEmptyConfig(t *testing.T) {
	config, err := LoadFile(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, Config{}) {
		t.Fatalf("loaded config = %#v, want empty config", config)
	}
}

func TestResolveValue_readsExactEnvironmentReference(t *testing.T) {
	t.Setenv("PERK_WORKBENCH_TEST_KEY", "secret")

	value, err := ResolveValue("env:PERK_WORKBENCH_TEST_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if value != "secret" {
		t.Fatalf("resolved value = %q, want secret", value)
	}
}

func TestLoadFiles_providerOnlyConfigurationLeavesAIUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.json")
	contents := `{"providers":{"cloud":{"name":"Cloud","api":"openai","base_url":"https://api.example/v1","api_key":"env:CLOUD_KEY","models":["small"]}}}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadFiles(path, filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Agents) != 0 {
		t.Fatalf("agents = %#v, want none", config.Agents)
	}
}

func TestSave_roundTripsAndCreatesSecureFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "ai.json")
	config := Config{
		Providers: map[string]Provider{
			"cloud": {
				Name:    "Cloud",
				API:     APIOpenAI,
				BaseURL: "https://api.example/v1",
				APIKey:  "env:CLOUD_KEY",
				Models:  []string{"small"},
			},
		},
		Agents: map[string]Agent{
			"assistant": {
				Name:         "Assistant",
				Provider:     "cloud",
				Model:        "small",
				SystemPrompt: "help",
			},
		},
	}

	if err := Save(path, config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("saved mode = %o, want 600", got)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "\n  \"providers\"") {
		t.Fatalf("saved config is not indented JSON: %s", contents)
	}
	got, err := LoadFiles(path, filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, config) {
		t.Fatalf("loaded config = %#v, want %#v", got, config)
	}
}

func TestSave_userLayerPersistsAPIKeyWithoutTouchingProject(t *testing.T) {
	root := t.TempDir()
	userPath := filepath.Join(root, "user", "ai.json")
	projectPath := filepath.Join(root, "project", ".perk-workbench", "ai.json")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o700); err != nil {
		t.Fatal(err)
	}
	projectContents := []byte(`{"providers":{"project":{"name":"Project","api":"openai","base_url":"https://project.example/v1","api_key":"env:PROJECT_KEY","models":["large"]}}}`)
	if err := os.WriteFile(projectPath, projectContents, 0o600); err != nil {
		t.Fatal(err)
	}

	config := Config{
		Providers: map[string]Provider{
			"cloud": {
				Name:    "Cloud",
				API:     APIOpenAI,
				BaseURL: "https://api.example/v1",
				APIKey:  "sk-entered-secret",
				Models:  []string{"small"},
			},
		},
		Agents: map[string]Agent{
			"assistant": {
				Name:     "Assistant",
				Provider: "cloud",
				Model:    "small",
			},
		},
	}
	if err := Save(userPath, config); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("saved user mode = %o, want 600", got)
	}
	got, err := LoadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, config) {
		t.Fatalf("loaded user config = %#v, want %#v", got, config)
	}
	entries, err := os.ReadDir(filepath.Dir(userPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".ai-") && strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file %q remains after save", entry.Name())
		}
	}

	projectAfter, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projectAfter, projectContents) {
		t.Fatalf("project config changed from %q to %q", projectContents, projectAfter)
	}
}

func TestSave_rejectsInvalidConfigBeforeTouchingTarget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "ai.json")
	invalid := Config{
		Providers: map[string]Provider{
			"cloud": {Name: "Cloud"},
		},
	}
	if err := Save(path, invalid); err == nil {
		t.Fatal("Save(invalid) succeeded, want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("target stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("parent stat error = %v, want not exist", err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"providers":{},"agents":{}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, invalid); err == nil {
		t.Fatal("Save(invalid) succeeded, want error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("target changed to %q, want %q", got, original)
	}
}
