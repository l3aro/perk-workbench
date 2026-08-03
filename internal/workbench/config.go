package workbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// Config holds user-configurable default behavior. Zero values mean the
// built-in default, so fields can be omitted from config.json.
type Config struct {
	// BrowsePageSize is the default row limit for table browsing.
	// Must be within [1, sharedsql.MaxRows] when set.
	BrowsePageSize int `json:"browse_page_size"`
	// QueryLogPageSize is the default page size of the query-log pane.
	// Must be within [1, queryLogLimit] when set.
	QueryLogPageSize int `json:"query_log_page_size"`
	// QueryLogRetentionDays keeps query-log entries for this many days.
	// Omitted or 0 keeps the built-in default (30); to keep no history at
	// all, set PERK_WORKBENCH_QUERY_LOG_RETENTION_DAYS=0 (config cannot
	// express it, since 0 means unset).
	QueryLogRetentionDays int `json:"query_log_retention_days"`
	// ReadOnly opens every connection read-only by default. The
	// per-connection form toggle still opts a connection back to read-write.
	ReadOnly bool `json:"read_only"`
	// Theme is the startup theme: one of ocean, nord, monokai, dracula,
	// catppuccin, solarized.
	Theme string `json:"theme"`
}

// appConfig is the resolved user configuration applied by SetAppConfig.
// Package-level consumers (query-log sizing/pruning, theme) read it; zero
// values keep the built-in defaults.
var appConfig Config

// SetAppConfig applies user configuration. Call it before New so startup
// defaults (browse page size, query-log page size, read-only, theme) pick
// it up. Env-var overrides (PERK_WORKBENCH_QUERY_LOG_*) still win over
// config values at their read sites.
func SetAppConfig(config Config) {
	appConfig = config
	for _, choice := range themeChoices {
		if appTheme(config.Theme) == choice {
			setTheme(choice)
			return
		}
	}
}

// LoadConfig reads config.json and returns the Config. If the file does not
// exist, it writes the default config file and returns the defaults.
func LoadConfig(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := writeDefaultConfigFile(path); err != nil {
				return Config{}, fmt.Errorf("writing default config %q: %w", path, err)
			}
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading config %q: %w", path, err)
	}
	if len(contents) == 0 {
		return Config{}, nil
	}

	var config Config
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, fmt.Errorf("parsing config %q: %w", path, err)
	}
	switch {
	case config.BrowsePageSize != 0 && (config.BrowsePageSize < 1 || config.BrowsePageSize > sharedsql.MaxRows):
		return Config{}, fmt.Errorf("config %q: browse_page_size must be between 1 and %d, got %d", path, sharedsql.MaxRows, config.BrowsePageSize)
	case config.QueryLogPageSize != 0 && (config.QueryLogPageSize < 1 || config.QueryLogPageSize > queryLogLimit):
		return Config{}, fmt.Errorf("config %q: query_log_page_size must be between 1 and %d, got %d", path, queryLogLimit, config.QueryLogPageSize)
	case config.QueryLogRetentionDays < 0:
		return Config{}, fmt.Errorf("config %q: query_log_retention_days must be non-negative, got %d", path, config.QueryLogRetentionDays)
	case config.Theme != "" && !validTheme(config.Theme):
		return Config{}, fmt.Errorf("config %q: theme %q is not one of %v", path, config.Theme, themeNames())
	}
	return config, nil
}

func validTheme(name string) bool {
	for _, choice := range themeChoices {
		if appTheme(name) == choice {
			return true
		}
	}
	return false
}

func themeNames() []string {
	names := make([]string, len(themeChoices))
	for i, choice := range themeChoices {
		names[i] = string(choice)
	}
	return names
}

// SaveTheme persists a theme choice into config.json, replacing only the
// theme key and preserving every other key byte-for-byte — including
// unknown or future fields the Config struct does not model. A missing or
// empty file starts from the built-in defaults.
func SaveTheme(path, theme string) error {
	contents, err := os.ReadFile(path)
	var raw map[string]json.RawMessage
	switch {
	case err == nil && len(contents) > 0:
		if err := json.Unmarshal(contents, &raw); err != nil {
			return fmt.Errorf("parsing config %q: %w", path, err)
		}
	case err == nil:
		// empty file: start from defaults
	default:
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading config %q: %w", path, err)
		}
	}
	if raw == nil {
		raw = defaultConfigValues()
	}
	themeValue, err := json.Marshal(theme)
	if err != nil {
		return err
	}
	raw["theme"] = themeValue
	appConfig.Theme = theme // keep the resolved config in sync
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// defaultConfigValues returns the built-in defaults as a JSON map, used when
// materializing a missing config file.
func defaultConfigValues() map[string]json.RawMessage {
	data, err := json.Marshal(Config{
		BrowsePageSize:        core.BrowsePageSize,
		QueryLogPageSize:      defaultQueryLogPageSize,
		QueryLogRetentionDays: defaultQueryLogRetentionDays,
		Theme:                 string(themeOcean),
	})
	if err != nil {
		panic(err) // plain struct: cannot fail
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		panic(err)
	}
	return raw
}

// ConfigPath returns the default config file path in the user's
// XDG config directory.
func ConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "perk-workbench", "config.json")
}

func writeDefaultConfigFile(path string) error {
	data, err := json.MarshalIndent(defaultConfigValues(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
