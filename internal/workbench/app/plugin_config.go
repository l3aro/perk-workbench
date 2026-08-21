package app

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/l3aro/perk-workbench/internal/database/plugin"
)

// ReadPluginTrust returns the external descriptor pins without materializing
// or validating config. It remains a read-only compatibility helper for
// command previews; built-ins never appear in the result.
func ReadPluginTrust(path string) map[string]string {
	raw, err := readConfigRaw(path)
	if err != nil || raw == nil {
		return nil
	}
	legacy, hasLegacy, err := parseLegacyPluginTrust(raw, path)
	if err != nil {
		return nil
	}
	if hasLegacy && len(legacy) > 0 {
		return legacy
	}
	var descriptors []PluginConfig
	if err := json.Unmarshal(raw["plugins"], &descriptors); err != nil {
		return nil
	}
	trust := map[string]string{}
	for _, descriptor := range descriptors {
		if descriptor.Path == "" || descriptor.SHA256 == "" {
			continue
		}
		if resolved, err := plugin.ResolveExecutable(descriptor.Path, path); err == nil {
			trust[resolved] = descriptor.SHA256
		}
	}
	if len(trust) == 0 {
		return nil
	}
	return trust
}

// SavePlugin appends or replaces one external descriptor and stores its
// lowercase SHA-256 pin. The mutation is atomic and leaves unrelated config
// keys untouched.
func SavePlugin(path, canonical, digest string) (changed bool, err error) {
	plugins, changed, err := savePlugin(path, canonical, digest)
	if err != nil {
		return false, err
	}
	appConfig.Plugins = plugins
	return changed, nil
}

func savePlugin(path, canonical, digest string) (plugins []PluginConfig, changed bool, err error) {
	raw, err := readConfigRaw(path)
	if err != nil {
		return nil, false, err
	}
	if raw == nil {
		raw = defaultConfigValues()
	}
	if _, err := migratePluginConfig(raw, path); err != nil {
		return nil, false, err
	}
	plugins, err = pluginDescriptors(raw, path)
	if err != nil {
		return nil, false, err
	}
	next := slices.Clone(plugins)
	replaced := false
	for i, descriptor := range next {
		if descriptor.Path == "" {
			continue
		}
		resolved, resolveErr := plugin.ResolveExecutable(descriptor.Path, path)
		if resolveErr == nil && resolved == canonical {
			next[i] = PluginConfig{Path: canonical, SHA256: digest}
			replaced = true
			break
		}
	}
	if !replaced {
		next = append(next, PluginConfig{Path: canonical, SHA256: digest})
	}
	if slices.Equal(plugins, next) {
		return next, false, nil
	}
	if err := writeConfigPlugins(raw, path, next); err != nil {
		return nil, false, err
	}
	return next, true, nil
}

// RemovePlugin removes a configured built-in name or external path. External
// operands may use the exact configured spelling or resolve to one configured
// executable. Ambiguous canonical matches fail without writing.
func RemovePlugin(path, nameOrExecutable string) (entry, canonical string, changed bool, err error) {
	entry, canonical, plugins, changed, err := removePlugin(path, nameOrExecutable)
	if err != nil {
		return "", "", false, err
	}
	appConfig.Plugins = plugins
	return entry, canonical, changed, nil
}

func removePlugin(path, nameOrExecutable string) (entry, canonical string, plugins []PluginConfig, changed bool, err error) {
	raw, err := readConfigRaw(path)
	if err != nil {
		return "", "", nil, false, err
	}
	if raw == nil {
		raw = defaultConfigValues()
	}
	if _, err := migratePluginConfig(raw, path); err != nil {
		return "", "", nil, false, err
	}
	plugins, err = pluginDescriptors(raw, path)
	if err != nil {
		return "", "", nil, false, err
	}

	index := -1
	for i, descriptor := range plugins {
		if descriptor.Builtin == nameOrExecutable || descriptor.Path == nameOrExecutable {
			if index != -1 {
				return "", "", nil, false, fmt.Errorf("plugin %q: ambiguous configured entries", nameOrExecutable)
			}
			index = i
		}
	}
	if index == -1 {
		resolved, resolveErr := plugin.ResolveExecutable(nameOrExecutable, path)
		if resolveErr != nil {
			return "", "", nil, false, fmt.Errorf("plugin %q: not configured", nameOrExecutable)
		}
		for i, descriptor := range plugins {
			if descriptor.Path == "" {
				continue
			}
			candidate, candidateErr := plugin.ResolveExecutable(descriptor.Path, path)
			if candidateErr == nil && candidate == resolved {
				if index != -1 {
					return "", "", nil, false, fmt.Errorf("plugin %q: ambiguous configured entries", nameOrExecutable)
				}
				index = i
				canonical = resolved
			}
		}
	}
	if index == -1 {
		return "", "", nil, false, fmt.Errorf("plugin %q: not configured", nameOrExecutable)
	}
	removed := plugins[index]
	entry = removed.Builtin
	if entry == "" {
		entry = removed.Path
		if canonical == "" {
			canonical, _ = plugin.ResolveExecutable(removed.Path, path)
		}
	}
	next := slices.Delete(slices.Clone(plugins), index, index+1)
	if slices.Equal(plugins, next) {
		return entry, canonical, next, false, nil
	}
	if err := writeConfigPlugins(raw, path, next); err != nil {
		return "", "", nil, false, err
	}
	return entry, canonical, next, true, nil
}

func pluginDescriptors(raw map[string]json.RawMessage, path string) ([]PluginConfig, error) {
	value, ok := raw["plugins"]
	if !ok || string(value) == "null" {
		return defaultPluginConfigs(), nil
	}
	var plugins []PluginConfig
	if err := json.Unmarshal(value, &plugins); err != nil {
		return nil, fmt.Errorf("parsing config %q: plugins: %w", path, err)
	}
	if err := validatePluginConfigs(path, plugins); err != nil {
		return nil, err
	}
	return plugins, nil
}

func writeConfigPlugins(raw map[string]json.RawMessage, path string, plugins []PluginConfig) error {
	pluginsJSON, err := json.Marshal(plugins)
	if err != nil {
		return err
	}
	raw["plugins"] = pluginsJSON
	delete(raw, "plugin_trust")
	delete(raw, "disabled_official_plugins")
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFileAtomic(path, data)
}
