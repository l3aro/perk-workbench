package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench/internal/core"
)

func TestLoadConfig_missing_file_writes_defaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil error", err)
	}
	if len(config.Plugins) != 4 {
		t.Fatalf("LoadConfig plugins = %#v, want four built-ins", config.Plugins)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("default config file not written: %v", err)
	}
	for _, want := range []string{`"browse_page_size": 25`, `"query_log_page_size": 25`, `"query_log_retention_days": 30`, `"notification_retention_days": 30`, `"notification_timeout_seconds": 10`, `"appearance": "dark"`, `"auto_theme": true`, `"dark_theme": "ocean"`, `"light_theme": "light-ocean"`, `"vim_mode": true`, `"nerd_font": true`, `"log_level": "info"`, `"table_open_target": "structure"`, `"builtin": "sqlite"`, `"builtin": "mongodb"`} {
		if !strings.Contains(string(contents), want) {
			t.Fatalf("default config = %q, want it to contain %q", contents, want)
		}
	}
}

func TestLoadConfig_reads_values(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	contents := `{"browse_page_size": 100, "query_log_page_size": 7, "query_log_retention_days": 90, "notification_retention_days": 45, "notification_timeout_seconds": 20, "read_only": true, "theme": "nord", "log_level": "warn"}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil error", err)
	}
	want := Config{BrowsePageSize: 100, QueryLogPageSize: 7, QueryLogRetentionDays: 90, NotificationRetentionDays: 45, NotificationTimeoutSeconds: 20, ReadOnly: true, DarkTheme: "nord", LightTheme: "light-ocean", AutoTheme: boolPtr(false), LogLevel: "warn"}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("LoadConfig = %#v, want %#v", config, want)
	}
}

func TestLoadConfig_rejects_invalid(t *testing.T) {
	for _, contents := range []string{
		`{"browse_page_size": -1}`,
		`{"browse_page_size": 501}`,
		`{"query_log_page_size": 101}`,
		`{"query_log_retention_days": -1}`,
		`{"notification_retention_days": -1}`,
		`{"notification_timeout_seconds": -1}`,
		`{"notification_timeout_seconds": 86401}`,
		`{"appearance": "lightish"}`,
		`{"dark_theme": "light-ocean"}`,
		`{"light_theme": "ocean"}`,
		`{"log_level": "verbose"}`,
		`{"table_open_target": "columns"}`,
		`{"plugins": ["  "]}`,
		`{"plugins": ["ok", ""]}`,
		`{"plugins": ["bad\u0000item"]}`,
		`not json`,
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Fatalf("LoadConfig(%q) = nil error, want error", contents)
		}
	}
}

func TestSetAppConfig_applies_defaults_to_new_models(t *testing.T) {
	previous := appConfig
	originalTheme := activeTheme
	t.Cleanup(func() {
		appConfig = previous
		setTheme(originalTheme)
	})

	SetAppConfig(Config{BrowsePageSize: 50, QueryLogPageSize: 7, QueryLogRetentionDays: 90, ReadOnly: true, DarkTheme: "nord"})
	model := New("", context.Background(), testOpen, false)

	if model.browse.component.PageSize != 50 {
		t.Fatalf("browsePageSize = %d, want 50", model.browse.component.PageSize)
	}
	if model.queryLog.component.PageSize != 7 {
		t.Fatalf("queryLogPageSize = %d, want 7", model.queryLog.component.PageSize)
	}
	if !model.ReadOnly {
		t.Fatal("ReadOnly = false, want true from config")
	}
	if !model.connection.component.Form.Values.ReadOnly {
		t.Fatal("connection form readOnly = false, want pre-checked from config")
	}
	if activeTheme != themeNord {
		t.Fatalf("activeTheme = %q, want %q", activeTheme, themeNord)
	}
}

func TestSetAppConfig_zero_keeps_builtin_defaults(t *testing.T) {
	previous := appConfig
	originalTheme := activeTheme
	t.Cleanup(func() {
		appConfig = previous
		setTheme(originalTheme)
	})

	SetAppConfig(Config{})
	model := New("", context.Background(), testOpen, false)

	if model.browse.component.PageSize != core.BrowsePageSize {
		t.Fatalf("browsePageSize = %d, want built-in %d", model.browse.component.PageSize, core.BrowsePageSize)
	}
	if model.queryLog.component.PageSize != defaultQueryLogPageSize {
		t.Fatalf("queryLogPageSize = %d, want built-in %d", model.queryLog.component.PageSize, defaultQueryLogPageSize)
	}
	if model.ReadOnly {
		t.Fatal("ReadOnly = true, want false with zero config")
	}
	if activeTheme != themeOcean {
		t.Fatalf("activeTheme = %q, want built-in %q", activeTheme, themeOcean)
	}
}

func TestLoadConfig_vimModeOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	contents := `{"vim_mode": false}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil error", err)
	}
	if config.VimMode == nil || *config.VimMode {
		t.Fatalf("VimMode = %v, want explicit false", config.VimMode)
	}
}

func TestSaveVimMode_preservesUnknownKeys(t *testing.T) {
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })

	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"browse_page_size": 50, "future_key": {"nested": [1, 2]}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SaveVimMode(path, false); err != nil {
		t.Fatalf("SaveVimMode = %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(contents, &raw); err != nil {
		t.Fatalf("saved config = %q, not valid JSON: %v", contents, err)
	}
	if got := string(raw["vim_mode"]); got != "false" {
		t.Fatalf("vim_mode = %s, want false", got)
	}
	if got := string(raw["browse_page_size"]); got != "50" {
		t.Fatalf("browse_page_size = %s, want 50 (dropped on rewrite)", got)
	}
	var future struct {
		Nested []int
	}
	if err := json.Unmarshal(raw["future_key"], &future); err != nil || len(future.Nested) != 2 || future.Nested[0] != 1 || future.Nested[1] != 2 {
		t.Fatalf("future_key = %s, want preserved nested value (err %v)", raw["future_key"], err)
	}
	if vimModeEnabled() {
		t.Fatal("vimModeEnabled = true, want false after SaveVimMode(false)")
	}
}

func TestSaveTheme_preservesUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"browse_page_size": 50, "future_key": {"nested": [1, 2]}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SaveTheme(path, themeNord); err != nil {
		t.Fatalf("SaveTheme = %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(contents, &raw); err != nil {
		t.Fatalf("saved config = %q, not valid JSON: %v", contents, err)
	}
	if got := string(raw["dark_theme"]); got != `"nord"` {
		t.Fatalf("dark_theme = %s, want %q", got, "nord")
	}
	if _, ok := raw["theme"]; ok {
		t.Fatalf("saved config still holds the legacy theme key: %q", contents)
	}
	if got := string(raw["browse_page_size"]); got != "50" {
		t.Fatalf("browse_page_size = %s, want 50 (dropped on rewrite)", got)
	}
	var future struct {
		Nested []int
	}
	if err := json.Unmarshal(raw["future_key"], &future); err != nil || len(future.Nested) != 2 || future.Nested[0] != 1 || future.Nested[1] != 2 {
		t.Fatalf("future_key = %s, want preserved nested value (err %v)", raw["future_key"], err)
	}
}

func TestLoadConfig_tableOpenTarget(t *testing.T) {
	for _, target := range []string{"structure", "browse", "sql", "indexes", "foreign_keys"} {
		path := filepath.Join(t.TempDir(), "config.json")
		contents := `{"table_open_target": "` + target + `"}`
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("LoadConfig(%q) = %v, want nil error", target, err)
		}
		if config.TableOpenTarget != target {
			t.Fatalf("TableOpenTarget = %q, want %q", config.TableOpenTarget, target)
		}
	}
}

func TestTableOpenTarget_defaults_and_config_selects_tab(t *testing.T) {
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })

	SetAppConfig(Config{})
	if got := tableOpenTargetTab(); got != tabStructure {
		t.Fatalf("tableOpenTargetTab = %v, want built-in Structure", got)
	}
	SetAppConfig(Config{TableOpenTarget: "browse"})
	if got := tableOpenTargetTab(); got != tabBrowse {
		t.Fatalf("tableOpenTargetTab = %v, want Browse from config", got)
	}
	SetAppConfig(Config{TableOpenTarget: "foreign_keys"})
	if got := tableOpenTargetTab(); got != tabForeignKeys {
		t.Fatalf("tableOpenTargetTab = %v, want Foreign Keys from config", got)
	}
}

func TestSaveTableOpenTarget_preservesUnknownKeys(t *testing.T) {
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })

	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"browse_page_size": 50, "future_key": {"nested": [1, 2]}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SaveTableOpenTarget(path, "browse"); err != nil {
		t.Fatalf("SaveTableOpenTarget = %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(contents, &raw); err != nil {
		t.Fatalf("saved config = %q, not valid JSON: %v", contents, err)
	}
	if got := string(raw["table_open_target"]); got != `"browse"` {
		t.Fatalf("table_open_target = %s, want %q", got, "browse")
	}
	if got := string(raw["browse_page_size"]); got != "50" {
		t.Fatalf("browse_page_size = %s, want 50 (dropped on rewrite)", got)
	}
	var future struct {
		Nested []int
	}
	if err := json.Unmarshal(raw["future_key"], &future); err != nil || len(future.Nested) != 2 || future.Nested[0] != 1 || future.Nested[1] != 2 {
		t.Fatalf("future_key = %s, want preserved nested value (err %v)", raw["future_key"], err)
	}
	if got := tableOpenTargetTab(); got != tabBrowse {
		t.Fatalf("tableOpenTargetTab = %v, want Browse after SaveTableOpenTarget", got)
	}
}

func TestVimMode_modelDefaultsFromConfig(t *testing.T) {
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })

	SetAppConfig(Config{})
	if model := New("", context.Background(), testOpen, false); !model.vimMode {
		t.Fatal("vimMode = false, want built-in default true")
	}
	SetAppConfig(Config{VimMode: boolPtr(false)})
	if model := New("", context.Background(), testOpen, false); model.vimMode {
		t.Fatal("vimMode = true, want false from config")
	}
}

func TestQueryLogConfig_uses_config_then_env_wins(t *testing.T) {
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
	SetAppConfig(Config{QueryLogPageSize: 7, QueryLogRetentionDays: 90})

	t.Setenv("PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE", "")
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS", "")
	if got, want := queryLogPageSize(), 7; got != want {
		t.Fatalf("queryLogPageSize = %d, want config %d", got, want)
	}
	if got, want := queryLogRetentionDays(), 90; got != want {
		t.Fatalf("queryLogRetentionDays = %d, want config %d", got, want)
	}

	// Env vars still win over config values.
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_PAGE_SIZE", "3")
	t.Setenv("PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS", "0")
	if got, want := queryLogPageSize(), 3; got != want {
		t.Fatalf("queryLogPageSize = %d, want env %d", got, want)
	}
	if got, want := queryLogRetentionDays(), 0; got != want {
		t.Fatalf("queryLogRetentionDays = %d, want env %d", got, want)
	}
}

func TestLoadConfig_plugins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"plugins":[{"builtin":"sqlite"},{"path":"redis-db","sha256":"`+strings.Repeat("ab", 32)+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Plugins) != 2 || config.Plugins[0].Builtin != "sqlite" || config.Plugins[1].Path != "redis-db" {
		t.Fatalf("plugins = %#v, want descriptor values", config.Plugins)
	}
}
