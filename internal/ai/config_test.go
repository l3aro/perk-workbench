package ai

import (
	"os"
	"path/filepath"
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
