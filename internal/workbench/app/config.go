package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	"github.com/l3aro/perk-workbench/internal/log"
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
	// NotificationRetentionDays keeps notification entries for this many
	// days. Omitted or 0 keeps the built-in default (30).
	NotificationRetentionDays int `json:"notification_retention_days"`
	// NotificationTimeoutSeconds is how long a notification popup stays
	// visible. Omitted or 0 keeps the built-in default (10); values above
	// one day are rejected.
	NotificationTimeoutSeconds int `json:"notification_timeout_seconds"`
	// ReadOnly opens every connection read-only by default. The
	// per-connection form toggle still opts a connection back to read-write.
	ReadOnly bool `json:"read_only"`
	// Theme is the startup theme: one of ocean, nord, monokai, dracula,
	// catppuccin, solarized.
	Theme string `json:"theme"`
	// VimMode enables modal vim-style editing: normal mode navigates with
	// j/k-style keys and insert mode (i/Enter) types. Disabled, the focused
	// input is always editable — click to type, no mode switch. Omitted
	// means enabled (the built-in default).
	VimMode *bool `json:"vim_mode"`
	// NerdFont renders the schema tree's node markers as Nerd Font icons
	// (database, folder, table). Omitted means enabled (the built-in
	// default); terminals without a Nerd Font can set false to fall back to
	// geometric symbols.
	NerdFont *bool `json:"nerd_font"`
	// LogLevel is the minimum severity written to the event log and
	// surfaced as notifications: debug, info, warn, or error. Omitted
	// keeps the built-in default (info).
	LogLevel string `json:"log_level"`
	// TableOpenTarget is the workspace tab focused after selecting a table
	// in the schema tree: structure (columns), browse, sql, indexes, or
	// foreign_keys. Omitted keeps the built-in default (structure).
	TableOpenTarget string `json:"table_open_target"`
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
	log.SetLevel(logLevelFromConfig(config.LogLevel))
	for _, choice := range themeChoices {
		if appTheme(config.Theme) == choice {
			setTheme(choice)
			return
		}
	}
}

// logLevelFromConfig maps the config log_level value to the log package
// severity. Unknown and omitted values keep the built-in default (info).
func logLevelFromConfig(value string) log.Level {
	switch value {
	case "debug":
		return log.LevelDebug
	case "warn":
		return log.LevelWarn
	case "error":
		return log.LevelError
	default:
		return log.LevelInfo
	}
}

// vimModeEnabled reports whether modal vim-style editing is on. The
// built-in default is on; config.json opts out with "vim_mode": false.
func vimModeEnabled() bool {
	return appConfig.VimMode == nil || *appConfig.VimMode
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
	case config.NotificationRetentionDays < 0:
		return Config{}, fmt.Errorf("config %q: notification_retention_days must be non-negative, got %d", path, config.NotificationRetentionDays)
	case config.NotificationTimeoutSeconds < 0:
		return Config{}, fmt.Errorf("config %q: notification_timeout_seconds must be non-negative, got %d", path, config.NotificationTimeoutSeconds)
	case config.NotificationTimeoutSeconds > maxNotificationTimeoutSeconds:
		return Config{}, fmt.Errorf("config %q: notification_timeout_seconds must be at most %d, got %d", path, maxNotificationTimeoutSeconds, config.NotificationTimeoutSeconds)
	case config.Theme != "" && !validTheme(config.Theme):
		return Config{}, fmt.Errorf("config %q: theme %q is not one of %v", path, config.Theme, themeNames())
	case config.LogLevel != "" && !validLogLevel(config.LogLevel):
		return Config{}, fmt.Errorf("config %q: log_level %q is not one of %v", path, config.LogLevel, logLevelNames())
	case config.TableOpenTarget != "" && !validTableOpenTarget(config.TableOpenTarget):
		return Config{}, fmt.Errorf("config %q: table_open_target %q is not one of %v", path, config.TableOpenTarget, tableOpenTargetNames())
	}
	return config, nil
}

// tableOpenTargetNames returns the accepted table_open_target values.
func tableOpenTargetNames() []string {
	names := make([]string, len(tableTargetChoices))
	for i, tab := range tableTargetChoices {
		names[i] = tableTargetKey(tab)
	}
	return names
}

func validTheme(name string) bool {
	for _, choice := range themeChoices {
		if appTheme(name) == choice {
			return true
		}
	}
	return false
}

// logLevelNames returns the accepted log_level values.
func logLevelNames() []string {
	return []string{"debug", "info", "warn", "error"}
}

func validLogLevel(value string) bool {
	for _, name := range logLevelNames() {
		if value == name {
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
	if err := saveConfigValue(path, "theme", theme); err != nil {
		return err
	}
	appConfig.Theme = theme // keep the resolved config in sync
	return nil
}

// SaveVimMode persists the vim-mode toggle into config.json, replacing only
// the vim_mode key and preserving every other key byte-for-byte.
func SaveVimMode(path string, enabled bool) error {
	if err := saveConfigValue(path, "vim_mode", enabled); err != nil {
		return err
	}
	appConfig.VimMode = boolPtr(enabled) // keep the resolved config in sync
	return nil
}

// SaveTableOpenTarget persists the table-open target into config.json,
// replacing only the table_open_target key and preserving every other key
// byte-for-byte.
func SaveTableOpenTarget(path, target string) error {
	if err := saveConfigValue(path, "table_open_target", target); err != nil {
		return err
	}
	appConfig.TableOpenTarget = target // keep the resolved config in sync
	return nil
}

// tableOpenTargetTab returns the workspace tab focused after selecting a
// table in the schema tree. The built-in default is the Structure (columns)
// tab; config.json opts into another tab with "table_open_target".
func tableOpenTargetTab() workspaceTab {
	switch appConfig.TableOpenTarget {
	case tableTargetKey(tabBrowse):
		return tabBrowse
	case tableTargetKey(tabSQL):
		return tabSQL
	case tableTargetKey(tabIndexes):
		return tabIndexes
	case tableTargetKey(tabForeignKeys):
		return tabForeignKeys
	default:
		return tabStructure
	}
}

func validTableOpenTarget(value string) bool {
	for _, tab := range tableTargetChoices {
		if tableTargetKey(tab) == value {
			return true
		}
	}
	return false
}

// saveConfigValue rewrites config.json with a single key replaced, keeping
// every other key byte-for-byte. A missing or empty file starts from the
// built-in defaults.
func saveConfigValue(path, key string, value any) error {
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
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw[key] = valueJSON
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func boolPtr(value bool) *bool { return &value }

// toggleVimMode flips the vim-mode flag and persists it to config.json so it
// survives the next launch. Switching off also transitions the currently
// focused text input into insert mode, so typing works without a re-click.
// Persistence is best-effort: a failure is shown in the status line without
// reverting the toggle.
func (m *Model) toggleVimMode() tea.Cmd {
	m.vimMode = !m.vimMode
	var command tea.Cmd
	if !m.vimMode {
		command = m.beginInsertForCurrentFocus()
	}
	state := "off"
	if m.vimMode {
		state = "on"
	}
	if m.configPath == "" {
		m.setStatus("vim mode: " + state)
		return command
	}
	if err := SaveVimMode(m.configPath, m.vimMode); err != nil {
		m.setStatus("vim mode: " + state + " (not saved: " + err.Error() + ")")
		return command
	}
	m.setStatus("vim mode: " + state)
	return command
}

// commitTableOpenTarget applies the table-open target and persists it to
// config.json so it survives the next launch. Persistence is best-effort: a
// failure is shown in the status line without reverting the choice.
func (m *Model) commitTableOpenTarget(tab workspaceTab) {
	display := tableTargetName(tab)
	if m.configPath == "" {
		m.setStatus("open table → " + display)
		return
	}
	if err := SaveTableOpenTarget(m.configPath, tableTargetKey(tab)); err != nil {
		m.setStatus("open table → " + display + " (not saved: " + err.Error() + ")")
		return
	}
	m.setStatus("open table → " + display)
}

// defaultConfigValues returns the built-in defaults as a JSON map, used when
// materializing a missing config file.
func defaultConfigValues() map[string]json.RawMessage {
	data, err := json.Marshal(Config{
		BrowsePageSize:             core.BrowsePageSize,
		QueryLogPageSize:           defaultQueryLogPageSize,
		QueryLogRetentionDays:      defaultQueryLogRetentionDays,
		NotificationRetentionDays:  defaultNotificationRetentionDays,
		NotificationTimeoutSeconds: defaultNotificationTimeoutSeconds,
		Theme:                      string(themeOcean),
		VimMode:                    boolPtr(true),
		NerdFont:                   boolPtr(true),
		LogLevel:                   "info",
		TableOpenTarget:            tableTargetKey(tabStructure),
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
