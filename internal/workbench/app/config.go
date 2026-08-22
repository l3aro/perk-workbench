package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// PluginConfig identifies one configured plugin source. Exactly one of
// Builtin and Path must be set.
type PluginConfig struct {
	Builtin string `json:"builtin,omitempty"`
	Path    string `json:"path,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

var builtinPluginNames = []string{"sqlite", "mysql", "postgres", "mongodb"}

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
	// Appearance is the effective light/dark theme: "light" or "dark".
	// Omitted or empty keeps the built-in default (dark). While auto_theme
	// is enabled it is the fallback used when system detection is
	// unavailable and the value resolved into when auto is turned off.
	Appearance string `json:"appearance"`
	// AutoTheme follows the system light/dark appearance at startup.
	// Omitted means enabled (the built-in default).
	AutoTheme *bool `json:"auto_theme"`
	// DarkTheme is the theme used while appearance is dark: one of the
	// dark-scheme themes (ocean, nord, monokai, dracula, catppuccin,
	// solarized).
	DarkTheme string `json:"dark_theme"`
	// LightTheme is the theme used while appearance is light: one of the
	// light-scheme themes (light-ocean, light-nord, ...).
	LightTheme string `json:"light_theme"`
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
	// Plugins lists the configured built-in or external plugin descriptors.
	// Missing config files materialize the four bundled built-ins; an
	// explicit empty list disables all plugin instances.
	Plugins []PluginConfig `json:"plugins"`
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
	applyAppearanceConfig(config)
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
// exist, it writes and returns the default configuration, including all four
// bundled plugin descriptors. Legacy plugin fields are migrated in one
// atomic rewrite.
func LoadConfig(path string) (Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("reading config %q: %w", path, err)
		}
		raw := defaultConfigValues()
		data, marshalErr := json.MarshalIndent(raw, "", "  ")
		if marshalErr != nil {
			return Config{}, fmt.Errorf("encoding default config %q: %w", path, marshalErr)
		}
		if err := writeConfigFileAtomic(path, data); err != nil {
			return Config{}, fmt.Errorf("writing default config %q: %w", path, err)
		}
		return decodeConfig(path, raw)
	}
	if len(contents) == 0 {
		return Config{}, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(contents, &raw); err != nil {
		return Config{}, fmt.Errorf("parsing config %q: %w", path, err)
	}
	changed, err := migratePluginConfig(raw, path)
	if err != nil {
		return Config{}, err
	}
	themeChanged, err := migrateLegacyThemeRaw(raw, path)
	if err != nil {
		return Config{}, err
	}
	if themeChanged {
		changed = true
	}
	if changed {
		data, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return Config{}, err
		}
		if err := writeConfigFileAtomic(path, data); err != nil {
			return Config{}, fmt.Errorf("migrating config %q: %w", path, err)
		}
	}
	return decodeConfig(path, raw)
}

func decodeConfig(path string, raw map[string]json.RawMessage) (Config, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return Config{}, fmt.Errorf("encoding config %q: %w", path, err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parsing config %q: %w", path, err)
	}
	if err := validatePluginConfigs(path, config.Plugins); err != nil {
		return Config{}, err
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
	case config.Appearance != "" && !validAppearance(config.Appearance):
		return Config{}, fmt.Errorf("config %q: appearance %q is not one of %v", path, config.Appearance, appearanceNames())
	case config.DarkTheme != "" && !validDarkTheme(config.DarkTheme):
		return Config{}, fmt.Errorf("config %q: dark_theme %q is not one of %v", path, config.DarkTheme, darkThemeNames())
	case config.LightTheme != "" && !validLightTheme(config.LightTheme):
		return Config{}, fmt.Errorf("config %q: light_theme %q is not one of %v", path, config.LightTheme, lightThemeNames())
	case config.LogLevel != "" && !validLogLevel(config.LogLevel):
		return Config{}, fmt.Errorf("config %q: log_level %q is not one of %v", path, config.LogLevel, logLevelNames())
	case config.TableOpenTarget != "" && !validTableOpenTarget(config.TableOpenTarget):
		return Config{}, fmt.Errorf("config %q: table_open_target %q is not one of %v", path, config.TableOpenTarget, tableOpenTargetNames())
	}
	return config, nil

}
func defaultPluginConfigs() []PluginConfig {
	plugins := make([]PluginConfig, len(builtinPluginNames))
	for i, name := range builtinPluginNames {
		plugins[i] = PluginConfig{Builtin: name}
	}
	return plugins
}

func validBuiltinPlugin(name string) bool {
	for _, allowed := range builtinPluginNames {
		if name == allowed {
			return true
		}
	}
	return false
}

func migratePluginConfig(raw map[string]json.RawMessage, path string) (bool, error) {
	legacyTrust, hasTrust, err := parseLegacyPluginTrust(raw, path)
	if err != nil {
		return false, err
	}
	value, hasPlugins := raw["plugins"]
	if !hasPlugins {
		// Older config files predate the descriptor list. Materialize the
		// bundled plugins so the registry-backed connection form remains
		// usable, while preserving any legacy disabled built-ins.
		plugins := defaultPluginConfigs()
		changed := true
		if disabledValue, ok := raw["disabled_official_plugins"]; ok {
			var disabled []string
			if err := json.Unmarshal(disabledValue, &disabled); err != nil {
				return false, fmt.Errorf("parsing config %q: disabled_official_plugins: %w", path, err)
			}
			disabledSet := make(map[string]struct{}, len(disabled))
			for _, name := range disabled {
				disabledSet[name] = struct{}{}
			}
			plugins = slices.DeleteFunc(plugins, func(plugin PluginConfig) bool {
				_, disabled := disabledSet[plugin.Builtin]
				return disabled
			})
			delete(raw, "disabled_official_plugins")
		}
		if hasTrust {
			delete(raw, "plugin_trust")
		}
		encoded, err := json.Marshal(plugins)
		if err != nil {
			return false, err
		}
		raw["plugins"] = encoded
		return changed, nil
	}

	descriptors, legacy, err := decodePluginDescriptors(value, path)
	if err != nil {
		return false, err
	}
	changed := legacy
	for i := range descriptors {
		if descriptors[i].Path == "" || descriptors[i].SHA256 != "" {
			continue
		}
		resolved, resolveErr := plugin.ResolveExecutable(descriptors[i].Path, path)
		if resolveErr == nil {
			if digest, ok := legacyTrust[resolved]; ok {
				descriptors[i].SHA256 = digest
			}
		}
	}
	if hasTrust {
		changed = true
		delete(raw, "plugin_trust")
	}
	if _, ok := raw["disabled_official_plugins"]; ok {
		changed = true
		delete(raw, "disabled_official_plugins")
	}
	encoded, err := json.Marshal(descriptors)
	if err != nil {
		return false, err
	}
	if string(value) != string(encoded) {
		changed = true
	}
	raw["plugins"] = encoded
	if err := validatePluginConfigs(path, descriptors); err != nil {
		return false, err
	}
	return changed, nil
}

func decodePluginDescriptors(value json.RawMessage, path string) ([]PluginConfig, bool, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil {
		return nil, false, fmt.Errorf("parsing config %q: plugins: %w", path, err)
	}
	if len(items) == 0 {
		if string(value) == "null" {
			return nil, false, nil
		}
		return []PluginConfig{}, false, nil
	}
	legacy := false
	var first string
	if err := json.Unmarshal(items[0], &first); err == nil {
		legacy = true
	}
	descriptors := make([]PluginConfig, len(items))
	for i, item := range items {
		if legacy {
			var entry string
			if err := json.Unmarshal(item, &entry); err != nil {
				return nil, false, fmt.Errorf("config %q: plugins[%d] must be a descriptor or legacy string", path, i)
			}
			descriptors[i] = PluginConfig{Path: entry}
			continue
		}
		if err := json.Unmarshal(item, &descriptors[i]); err != nil {
			return nil, false, fmt.Errorf("config %q: plugins[%d] must be a descriptor: %w", path, i, err)
		}
	}
	return descriptors, legacy, nil
}

func parseLegacyPluginTrust(raw map[string]json.RawMessage, path string) (map[string]string, bool, error) {
	value, ok := raw["plugin_trust"]
	if !ok {
		return nil, false, nil
	}
	var trust map[string]string
	if err := json.Unmarshal(value, &trust); err != nil {
		return nil, true, fmt.Errorf("parsing config %q: plugin_trust: %w", path, err)
	}
	for key, digest := range trust {
		switch {
		case strings.TrimSpace(key) == "":
			return nil, true, fmt.Errorf("config %q: plugin_trust keys must not be blank", path)
		case !filepath.IsAbs(key):
			return nil, true, fmt.Errorf("config %q: plugin_trust key %q must be an absolute path", path, key)
		case strings.ContainsRune(key, '\x00'):
			return nil, true, fmt.Errorf("config %q: plugin_trust key must not contain a NUL byte", path)
		case !validSHA256Digest(digest) || digest != strings.ToLower(digest):
			return nil, true, fmt.Errorf("config %q: plugin_trust digest for %q must be lowercase 64 hexadecimal characters", path, key)
		}
	}
	return trust, true, nil
}
func validatePluginConfigs(path string, descriptors []PluginConfig) error {
	seenBuiltins := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	for i, descriptor := range descriptors {
		builtin := descriptor.Builtin
		external := descriptor.Path
		if (strings.TrimSpace(builtin) == "") == (strings.TrimSpace(external) == "") {
			return fmt.Errorf("config %q: plugins[%d] must set exactly one of builtin or path", path, i)
		}
		if strings.TrimSpace(builtin) != "" {
			if !validBuiltinPlugin(builtin) {
				return fmt.Errorf("config %q: plugins[%d].builtin %q is not one of %v", path, i, builtin, builtinPluginNames)
			}
			if descriptor.SHA256 != "" {
				return fmt.Errorf("config %q: plugins[%d].sha256 is only valid for path descriptors", path, i)
			}
			if _, exists := seenBuiltins[builtin]; exists {
				return fmt.Errorf("config %q: duplicate builtin plugin %q", path, builtin)
			}
			seenBuiltins[builtin] = struct{}{}
			continue
		}
		if strings.TrimSpace(external) == "" {
			return fmt.Errorf("config %q: plugins[%d].path must not be blank", path, i)
		}
		if strings.ContainsRune(external, '\x00') {
			return fmt.Errorf("config %q: plugins[%d].path must not contain a NUL byte", path, i)
		}
		if descriptor.SHA256 != "" && (!validSHA256Digest(descriptor.SHA256) || descriptor.SHA256 != strings.ToLower(descriptor.SHA256)) {
			return fmt.Errorf("config %q: plugins[%d].sha256 must be lowercase 64 hexadecimal characters", path, i)
		}
		if resolved, err := plugin.ResolveExecutable(external, path); err == nil {
			if _, exists := seenPaths[resolved]; exists {
				return fmt.Errorf("config %q: duplicate external plugin path %q", path, resolved)
			}
			seenPaths[resolved] = struct{}{}
		}
	}
	return nil
}

func migrateLegacyThemeRaw(raw map[string]json.RawMessage, path string) (bool, error) {
	legacy, ok := raw["theme"]
	if !ok {
		return false, nil
	}
	var name string
	if err := json.Unmarshal(legacy, &name); err != nil {
		return false, fmt.Errorf("config %q: theme %q: %w", path, string(legacy), err)
	}
	if !validDarkTheme(name) {
		name = string(themeOcean)
	}
	delete(raw, "theme")
	setJSONKey(raw, "dark_theme", name)
	setJSONKey(raw, "light_theme", string(themeLightOcean))
	setJSONKey(raw, "auto_theme", false)
	return true, nil
}

// tableOpenTargetNames returns the accepted table_open_target values.
func tableOpenTargetNames() []string {
	names := make([]string, len(tableTargetChoices))
	for i, tab := range tableTargetChoices {
		names[i] = tableTargetKey(tab)
	}
	return names
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

// themeNames returns the accepted theme names across both schemes.
func themeNames() []string {
	all := allThemes()
	names := make([]string, len(all))
	for i, choice := range all {
		names[i] = string(choice)
	}
	return names
}

// validTheme reports whether name is any known theme.
func validTheme(name string) bool {
	for _, choice := range allThemes() {
		if appTheme(name) == choice {
			return true
		}
	}
	return false
}

// darkThemeNames returns the accepted dark-scheme theme names.
func darkThemeNames() []string {
	all := darkThemes()
	names := make([]string, len(all))
	for i, choice := range all {
		names[i] = string(choice)
	}
	return names
}

// lightThemeNames returns the accepted light-scheme theme names.
func lightThemeNames() []string {
	all := lightThemes()
	names := make([]string, len(all))
	for i, choice := range all {
		names[i] = string(choice)
	}
	return names
}

// validDarkTheme reports whether name is a dark-scheme theme.
func validDarkTheme(name string) bool {
	for _, choice := range darkThemes() {
		if appTheme(name) == choice {
			return true
		}
	}
	return false
}

// validLightTheme reports whether name is a light-scheme theme.
func validLightTheme(name string) bool {
	for _, choice := range lightThemes() {
		if appTheme(name) == choice {
			return true
		}
	}
	return false
}

// appearanceNames returns the accepted appearance values.
func appearanceNames() []string {
	return []string{"dark", "light"}
}

// validAppearance reports whether value is a legal appearance.
func validAppearance(value string) bool {
	for _, name := range appearanceNames() {
		if value == name {
			return true
		}
	}
	return false
}

// saveThemeValue persists one theme slot (dark_theme or light_theme) into
// config.json, preserving every other key byte-for-byte — including unknown
// or future fields the Config struct does not model. A missing or empty file
// starts from the built-in defaults.
func saveThemeValue(path, key, theme string) error {
	if err := saveConfigValue(path, key, theme); err != nil {
		return err
	}
	if key == "light_theme" {
		appConfig.LightTheme = theme
	} else {
		appConfig.DarkTheme = theme
	}
	return nil
}

// SaveTheme persists a theme choice into config.json under the slot key for
// its scheme (dark_theme or light_theme), preserving every other key.
func SaveTheme(path string, name appTheme) error {
	key := "dark_theme"
	if themeScheme(name) == schemeLight {
		key = "light_theme"
	}
	return saveThemeValue(path, key, string(name))
}

// SaveAppearance persists the explicit appearance into config.json,
// preserving every other key.
func SaveAppearance(path, appearance string) error {
	if err := saveConfigValue(path, "appearance", appearance); err != nil {
		return err
	}
	appConfig.Appearance = appearance // keep the resolved config in sync
	return nil
}

// SaveAutoTheme persists the follow-system toggle into config.json,
// preserving every other key.
func SaveAutoTheme(path string, enabled bool) error {
	if err := saveConfigValue(path, "auto_theme", enabled); err != nil {
		return err
	}
	appConfig.AutoTheme = boolPtr(enabled) // keep the resolved config in sync
	return nil
}

// setJSONKey sets raw[key] to the JSON encoding of value unless the key
// already exists, preserving an explicit user value over the migration
// default.
func setJSONKey(raw map[string]json.RawMessage, key string, value any) {
	if _, ok := raw[key]; ok {
		return
	}
	if b, err := json.Marshal(value); err == nil {
		raw[key] = b
	}
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
	case tableTargetKey(tabQuery):
		return tabQuery
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

// validSHA256Digest reports whether value is a SHA-256 digest: exactly
// 64 hexadecimal characters. Both letter cases are accepted; digests
// are compared case-insensitively everywhere.
func validSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// readConfigRaw reads the config file as a JSON object map without
// writing or validating anything: a missing or empty file yields nil
// with no error, any other read/parse failure yields an error. The
// mutating config operations use it so a missing config never gets
// materialized by a read-only command.
func readConfigRaw(path string) (map[string]json.RawMessage, error) {
	contents, err := os.ReadFile(path)
	switch {
	case err == nil && len(contents) > 0:
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(contents, &raw); err != nil {
			return nil, fmt.Errorf("parsing config %q: %w", path, err)
		}
		return raw, nil
	case err == nil:
		return nil, nil
	default:
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}
}

// saveConfigValue rewrites config.json with a single key replaced, keeping
// every other key byte-for-byte. A missing or empty file starts from the
// built-in defaults. The write is atomic: a same-directory temporary file
// is fsynced and renamed over the original, so a failure never corrupts
// or partially overwrites the existing config.
func saveConfigValue(path, key string, value any) error {
	raw, err := readConfigRaw(path)
	if err != nil {
		return err
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
	return writeConfigFileAtomic(path, data)
}

// writeConfigFileAtomic writes data to path via a same-directory
// temporary file (mode 0600) that is fsynced and renamed over the
// target, so a failure at any point leaves the original file intact.
// The directory entry is fsynced best-effort so the rename itself is
// durable. The temporary file is removed when the write fails.
func writeConfigFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	committed = true
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
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
	display := m.tableTargetName(tab)
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
		Appearance:                 "dark",
		AutoTheme:                  boolPtr(true),
		DarkTheme:                  string(themeOcean),
		LightTheme:                 string(themeLightOcean),
		VimMode:                    boolPtr(true),
		NerdFont:                   boolPtr(true),
		LogLevel:                   "info",
		TableOpenTarget:            tableTargetKey(tabStructure),
		Plugins:                    defaultPluginConfigs(),
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
