package app

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/l3aro/perk-workbench/internal/database/plugin"
)

// ReadPluginTrust reads only the plugin_trust mapping from the config
// file without writing, validating, or materializing anything: a
// missing, empty, or malformed file yields nil. Read-only commands use
// it to report fingerprint state without ever mutating config.
func ReadPluginTrust(path string) map[string]string {
	raw, err := readConfigRaw(path)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var trust map[string]string
	if err := json.Unmarshal(raw["plugin_trust"], &trust); err != nil {
		return nil
	}
	if len(trust) == 0 {
		return nil
	}
	return trust
}

// SavePlugin pins one plugin executable: canonical is the canonical
// absolute path and digest its lowercase SHA-256. The entry is appended
// to the plugins list when no entry resolves to it, or replaced by the
// canonical path when an existing entry resolves to it (so a re-pin
// never duplicates the executable), and plugin_trust[canonical] is set
// to digest. Every other config key is preserved byte-for-byte and the
// file is rewritten atomically (same-directory temp + rename, mode
// 0600); the original is left intact on any failure. When the resulting
// plugins/trust state already matches, nothing is written and changed
// is false. The resolved in-memory config follows the persisted state.
func SavePlugin(path, canonical, digest string) (changed bool, err error) {
	plugins, trust, changed, err := savePlugin(path, canonical, digest)
	if err != nil {
		return false, err
	}
	appConfig.Plugins = plugins
	appConfig.PluginTrust = trust
	return changed, nil
}

// savePlugin is the pure persistence half of SavePlugin: it returns the
// resulting plugins/trust state so callers on another goroutine (the
// TUI approve command) can apply it on the model goroutine instead of
// racing the package-level appConfig.
func savePlugin(path, canonical, digest string) (plugins []string, trust map[string]string, changed bool, err error) {
	raw, err := readConfigRaw(path)
	if err != nil {
		return nil, nil, false, err
	}
	if raw == nil {
		raw = defaultConfigValues()
	}
	plugins, trust, err = configPlugins(raw, path)
	if err != nil {
		return nil, nil, false, err
	}

	newPlugins := slices.Clone(plugins)
	replaced := false
	for i, entry := range plugins {
		if entry == canonical {
			replaced = true // already the canonical entry
			break
		}
		if resolved, resolveErr := plugin.ResolveExecutable(entry, path); resolveErr == nil && resolved == canonical {
			// An existing entry already names this executable: re-pin it
			// in place under its canonical path instead of duplicating it.
			newPlugins[i] = canonical
			replaced = true
			break
		}
	}
	if !replaced {
		newPlugins = append(newPlugins, canonical)
	}
	newTrust := maps.Clone(trust)
	if newTrust == nil {
		newTrust = map[string]string{}
	}
	newTrust[canonical] = digest

	if slices.Equal(plugins, newPlugins) && maps.Equal(trust, newTrust) {
		return newPlugins, newTrust, false, nil
	}
	if err := writeConfigPlugins(raw, path, newPlugins, newTrust); err != nil {
		return nil, nil, false, err
	}
	return newPlugins, newTrust, true, nil
}

// RemovePlugin removes the configured plugin matching nameOrExecutable
// and its trust record. The operand matches the config entry exactly,
// or — when no entry string matches — resolves like a startup entry
// (bare names through PATH, relative paths against the config file's
// directory) and must equal exactly one configured entry's canonical
// path; ambiguous matches fail instead of removing multiple entries.
// The trust record is dropped only when no remaining entry resolves to
// the removed canonical path. The rewrite is atomic and preserves every
// unrelated key; the original file is left intact on failure. The
// resolved in-memory config follows the persisted state.
func RemovePlugin(path, nameOrExecutable string) (entry, canonical string, changed bool, err error) {
	entry, canonical, plugins, trust, changed, err := removePlugin(path, nameOrExecutable)
	if err != nil {
		return "", "", false, err
	}
	appConfig.Plugins = plugins
	appConfig.PluginTrust = trust
	return entry, canonical, changed, nil
}

// removePlugin is the pure persistence half of RemovePlugin: it returns
// the removed entry and canonical path plus the resulting plugins/trust
// state so async callers can apply it on the model goroutine instead of
// racing the package-level appConfig.
func removePlugin(path, nameOrExecutable string) (entry, canonical string, plugins []string, trust map[string]string, changed bool, err error) {
	raw, err := readConfigRaw(path)
	if err != nil {
		return "", "", nil, nil, false, err
	}
	plugins, trust, err = configPlugins(raw, path)
	if err != nil {
		return "", "", nil, nil, false, err
	}

	// Exact entry-string match takes precedence: naming the configured
	// entry is unambiguous even when other entries resolve to the same
	// executable.
	var exact []int
	for i, item := range plugins {
		if item == nameOrExecutable {
			exact = append(exact, i)
		}
	}
	index := -1
	switch len(exact) {
	case 1:
		index = exact[0]
		canonical, err = plugin.ResolveExecutable(plugins[index], path)
		if err != nil {
			// The entry itself is unresolvable; removing it by name is
			// still valid, and no canonical path exists for its pin.
			canonical = ""
		}
	case 0:
		resolved, resolveErr := plugin.ResolveExecutable(nameOrExecutable, path)
		if resolveErr != nil {
			return "", "", nil, nil, false, fmt.Errorf("plugin %q: not configured", nameOrExecutable)
		}
		var matches []int
		for i, item := range plugins {
			if candidate, candidateErr := plugin.ResolveExecutable(item, path); candidateErr == nil && candidate == resolved {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 0:
			return "", "", nil, nil, false, fmt.Errorf("plugin %q: not configured", nameOrExecutable)
		case 1:
			index = matches[0]
			canonical = resolved
		default:
			return "", "", nil, nil, false, fmt.Errorf("plugin %q: ambiguous: matches %d configured entries; remove by exact entry string", nameOrExecutable, len(matches))
		}
	default:
		return "", "", nil, nil, false, fmt.Errorf("plugin %q: ambiguous: matches %d configured entries", nameOrExecutable, len(exact))
	}

	entry = plugins[index]
	newPlugins := slices.Delete(slices.Clone(plugins), index, index+1)
	newTrust := maps.Clone(trust)
	if canonical != "" && !pluginStillConfigured(newPlugins, path, canonical) {
		delete(newTrust, canonical)
	}
	if slices.Equal(plugins, newPlugins) && maps.Equal(trust, newTrust) {
		return entry, canonical, newPlugins, newTrust, false, nil
	}
	if err := writeConfigPlugins(raw, path, newPlugins, newTrust); err != nil {
		return "", "", nil, nil, false, err
	}
	return entry, canonical, newPlugins, newTrust, true, nil
}

// pluginStillConfigured reports whether any entry in plugins resolves
// to canonical, so a shared trust record survives removing one of the
// entries that name the same executable.
func pluginStillConfigured(plugins []string, configPath, canonical string) bool {
	for _, item := range plugins {
		if resolved, err := plugin.ResolveExecutable(item, configPath); err == nil && resolved == canonical {
			return true
		}
	}
	return false
}

// configPlugins extracts the plugins list and plugin_trust mapping from
// a raw config object; absent keys yield nil with no error.
func configPlugins(raw map[string]json.RawMessage, path string) ([]string, map[string]string, error) {
	var plugins []string
	if value, ok := raw["plugins"]; ok {
		if err := json.Unmarshal(value, &plugins); err != nil {
			return nil, nil, fmt.Errorf("parsing config %q: plugins: %w", path, err)
		}
	}
	var trust map[string]string
	if value, ok := raw["plugin_trust"]; ok {
		if err := json.Unmarshal(value, &trust); err != nil {
			return nil, nil, fmt.Errorf("parsing config %q: plugin_trust: %w", path, err)
		}
	}
	return plugins, trust, nil
}

// writeConfigPlugins replaces the plugins and plugin_trust keys of a raw
// config object and atomically persists it. An empty trust map removes
// the plugin_trust key entirely instead of writing an empty object.
func writeConfigPlugins(raw map[string]json.RawMessage, path string, plugins []string, trust map[string]string) error {
	pluginsJSON, err := json.Marshal(plugins)
	if err != nil {
		return err
	}
	raw["plugins"] = pluginsJSON
	if len(trust) == 0 {
		delete(raw, "plugin_trust")
	} else {
		trustJSON, err := json.Marshal(trust)
		if err != nil {
			return err
		}
		raw["plugin_trust"] = trustJSON
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFileAtomic(path, data)
}
