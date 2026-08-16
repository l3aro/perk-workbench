package app

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
)

// TestPluginManagerHelperChild is the re-executed plugin child for the
// plugin-manager tests. It serves the minimal perk/v1 lifecycle (the
// initialize handshake only — the manager never exchanges session
// traffic) and always ends with os.Exit, never returning to the
// testing framework.
func TestPluginManagerHelperChild(t *testing.T) {
	if os.Getenv("PERK_PLUGIN_MANAGER_HELPER") != "1" {
		return
	}
	name := os.Getenv("PERK_PLUGIN_MANAGER_NAME")
	if name == "" {
		name = "managerhelper"
	}
	display := os.Getenv("PERK_PLUGIN_MANAGER_DISPLAY")
	if display == "" {
		display = "Manager Helper"
	}
	targets := strings.Split(os.Getenv("PERK_PLUGIN_MANAGER_TARGETS"), ",")
	if len(targets) == 1 && targets[0] == "" {
		targets = []string{"mhelper:"}
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		frame, err := reader.ReadBytes('\n')
		if err != nil {
			os.Exit(0) // stdin closed: normal end of service
		}
		var incoming struct {
			ID     *uint64 `json:"id"`
			Method string  `json:"method"`
		}
		if err := json.Unmarshal(frame, &incoming); err != nil || incoming.ID == nil {
			continue
		}
		if incoming.Method != "perk/v1/initialize" {
			respondManagerHelper(*incoming.ID, struct{}{})
			continue
		}
		wireTargets := make([]map[string]string, 0, len(targets))
		for _, prefix := range targets {
			wireTargets = append(wireTargets, map[string]string{"prefix": prefix})
		}
		respondManagerHelper(*incoming.ID, map[string]any{
			"protocol_version": 1,
			"capabilities": map[string]any{
				"name":    name,
				"display": display,
				"targets": wireTargets,
			},
		})
	}
}

func respondManagerHelper(id uint64, result any) {
	payload, err := json.Marshal(result)
	if err != nil {
		os.Exit(1)
	}
	frame, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: payload})
	if err != nil {
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(append(frame, '\n'))
}

// writeManagerHelperScriptAt writes an executable wrapper that
// re-executes the current test binary as the plugin-manager helper
// child.
func writeManagerHelperScriptAt(t *testing.T, path string) string {
	t.Helper()
	t.Setenv("PERK_PLUGIN_MANAGER_HELPER_BINARY", os.Args[0])
	script := "#!/bin/sh\nexec \"$PERK_PLUGIN_MANAGER_HELPER_BINARY\" -test.run=TestPluginManagerHelperChild\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// setupManagerHelper prepares one manager-test plugin child and its env.
func setupManagerHelper(t *testing.T) string {
	t.Helper()
	t.Setenv("PERK_PLUGIN_MANAGER_HELPER", "1")
	return writeManagerHelperScriptAt(t, filepath.Join(t.TempDir(), "manager-plugin"))
}

// openPluginManager drives the command palette to the plugins overlay.
func openPluginManager(t *testing.T) Model {
	t.Helper()
	model := New("", context.Background(), testOpen, false)
	model = resizeModel(model, 100, 24)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl, Text: "p"})
	model = updated.(Model)
	if !model.overlay.commandPalette.visible {
		t.Fatal("ctrl+p did not open the command palette")
	}
	for _, character := range "/plugins" {
		updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
		model = updated.(Model)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // exit filtering
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // select
	model = updated.(Model)
	if model.overlay.pluginManager == nil {
		t.Fatal("selecting plugins from the palette did not open the plugin manager")
	}
	return model
}

// appConfigFile returns the config.json path under the current
// XDG_CONFIG_HOME.
func appConfigFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "perk-workbench", "config.json")
}

func TestPluginManager_paletteOpensMenuAndEscCloses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := openPluginManager(t)
	if model.overlay.pluginManager.view != "menu" {
		t.Fatalf("view = %q, want the menu", model.overlay.pluginManager.view)
	}
	content := model.pluginManagerContent()
	for _, want := range []string{"Add plugin", "Remove plugin", "Back"} {
		if !strings.Contains(content, want) {
			t.Fatalf("menu content = %q, want it to contain %q", content, want)
		}
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay.pluginManager != nil {
		t.Fatal("esc from the menu did not close the plugin manager")
	}
}

// TestPluginManager_addPreviewShowsTrustDataWithoutConfigMutation: the
// async preview renders the canonical path, identity/display, target
// prefixes, query language, write interfaces, and the SHA-256, and the
// config file is neither created nor modified.
func TestPluginManager_addPreviewShowsTrustDataWithoutConfigMutation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PERK_PLUGIN_MANAGER_NAME", "mgrredis")
	t.Setenv("PERK_PLUGIN_MANAGER_DISPLAY", "Redis (manager)")
	t.Setenv("PERK_PLUGIN_MANAGER_TARGETS", "redis:")
	helper := setupManagerHelper(t)
	model := openPluginManager(t)

	// Menu → Add.
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay.pluginManager.view != "add" {
		t.Fatalf("view = %q, want add", model.overlay.pluginManager.view)
	}
	for _, character := range helper {
		updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
		model = updated.(Model)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || !model.overlay.pluginManager.busy {
		t.Fatal("enter did not start the async preview")
	}
	message := command().(pluginPreviewMsg)
	if message.err != nil {
		t.Fatalf("preview error = %v", message.err)
	}
	updated, _ = model.Update(message)
	model = updated.(Model)

	manager := model.overlay.pluginManager
	if manager == nil || manager.view != "preview" || manager.preview == nil {
		t.Fatalf("manager = %+v, want the preview view with data", manager)
	}
	preview := manager.preview
	if preview.Path != helper || preview.Name != "mgrredis" || preview.Display != "Redis (manager)" {
		t.Fatalf("preview = %+v, want the resolved identity", preview)
	}
	if len(preview.Targets) != 1 || preview.Targets[0] != "redis:" {
		t.Fatalf("targets = %v, want the advertised prefix", preview.Targets)
	}
	if preview.Language != "sql" || preview.Writes != "none" {
		t.Fatalf("language/writes = %q/%q, want the defaults", preview.Language, preview.Writes)
	}
	digest, err := plugin.SHA256File(helper)
	if err != nil {
		t.Fatal(err)
	}
	if preview.SHA256 != digest {
		t.Fatalf("sha256 = %s, want %s", preview.SHA256, digest)
	}
	content := model.pluginManagerContent()
	for _, want := range []string{"path: " + helper, "name: mgrredis", `display: "Redis (manager)"`, "targets: redis:", "query language: sql", "writes: none", "sha256: " + digest} {
		if !strings.Contains(content, want) {
			t.Fatalf("preview content = %q, want it to contain %q", content, want)
		}
	}
	if strings.Contains(content, "stderr") {
		t.Fatalf("preview content = %q, must never show plugin stderr", content)
	}
	if _, err := os.Stat(appConfigFile(t)); !os.IsNotExist(err) {
		t.Fatalf("preview created config.json (%v): previews must not mutate config", err)
	}
}

// TestPluginManager_enablePinsAtomicallyAndRequiresRestart: the
// explicit enable re-verifies with the previewed digest and persists
// the pin; the overlay closes with the restart-required status.
func TestPluginManager_enablePinsAtomicallyAndRequiresRestart(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("PERK_PLUGIN_MANAGER_NAME", "mgrredis")
	t.Setenv("PERK_PLUGIN_MANAGER_TARGETS", "redis:")
	snapshotAppConfig(t)
	helper := setupManagerHelper(t)
	model := openPluginManager(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	for _, character := range helper {
		updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
		model = updated.(Model)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	message := command().(pluginPreviewMsg)
	updated, _ = model.Update(message)
	model = updated.(Model)

	// Explicit confirmation: enter on the preview.
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || !model.overlay.pluginManager.busy {
		t.Fatal("enter on the preview did not start the approve")
	}
	approve := command().(pluginApproveMsg)
	if approve.err != nil {
		t.Fatalf("approve error = %v", approve.err)
	}
	updated, _ = model.Update(approve)
	model = updated.(Model)

	if model.overlay.pluginManager != nil {
		t.Fatal("successful enable left the plugin manager open")
	}
	if model.Status != "plugin enabled; restart required" {
		t.Fatalf("status = %q, want the restart-required notice", model.Status)
	}
	contents, err := os.ReadFile(appConfigFile(t))
	if err != nil {
		t.Fatalf("config was not written: %v", err)
	}
	var config struct {
		Plugins []string          `json:"plugins"`
		Trust   map[string]string `json:"plugin_trust"`
	}
	if err := json.Unmarshal(contents, &config); err != nil {
		t.Fatalf("config %q is not JSON: %v", contents, err)
	}
	if len(config.Plugins) != 1 || config.Plugins[0] != helper {
		t.Fatalf("plugins = %v, want the canonical helper path", config.Plugins)
	}
	digest, err := plugin.SHA256File(helper)
	if err != nil {
		t.Fatal(err)
	}
	if config.Trust[helper] != digest {
		t.Fatalf("plugin_trust = %v, want the pinned digest", config.Trust)
	}
	info, err := os.Stat(appConfigFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
	}
	// The resolved in-memory config follows, so a later remove list sees
	// the new entry.
	if len(appConfig.Plugins) != 1 || appConfig.Plugins[0] != helper {
		t.Fatalf("appConfig.Plugins = %v, want the enabled plugin", appConfig.Plugins)
	}
}

// TestPluginManager_previewCancelLeavesConfigUntouched: esc walks the
// preview back through the add view and menu and never writes config.
func TestPluginManager_previewCancelLeavesConfigUntouched(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	helper := setupManagerHelper(t)
	model := openPluginManager(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	for _, character := range helper {
		updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
		model = updated.(Model)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(command().(pluginPreviewMsg))
	model = updated.(Model)
	if model.overlay.pluginManager.view != "preview" {
		t.Fatalf("view = %q, want preview", model.overlay.pluginManager.view)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay.pluginManager.view != "add" {
		t.Fatalf("view = %q, want add after cancelling the preview", model.overlay.pluginManager.view)
	}
	if model.overlay.pluginManager.preview != nil {
		t.Fatal("cancelled preview still holds data")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay.pluginManager.view != "menu" {
		t.Fatalf("view = %q, want menu", model.overlay.pluginManager.view)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay.pluginManager != nil {
		t.Fatal("esc did not close the plugin manager")
	}
	if _, err := os.Stat(appConfigFile(t)); !os.IsNotExist(err) {
		t.Fatalf("cancelled flow created config.json (%v)", err)
	}
}

// TestPluginManager_approveDriftAfterPreviewFailsClosed: bytes changing
// between preview and enable are caught by the re-verification; the
// overlay shows the error and config stays untouched.
func TestPluginManager_approveDriftAfterPreviewFailsClosed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	helper := setupManagerHelper(t)
	model := openPluginManager(t)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	for _, character := range helper {
		updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
		model = updated.(Model)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(command().(pluginPreviewMsg))
	model = updated.(Model)

	// Drift the executable while keeping it a working helper.
	contents, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, append(contents, []byte("\n# drifted\n")...), 0o755); err != nil {
		t.Fatal(err)
	}

	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	approve := command().(pluginApproveMsg)
	if approve.err == nil || !strings.Contains(approve.err.Error(), "changed since preview") {
		t.Fatalf("approve error = %v, want the drift refusal", approve.err)
	}
	updated, _ = model.Update(approve)
	model = updated.(Model)
	if model.overlay.pluginManager == nil || model.overlay.pluginManager.view != "preview" {
		t.Fatalf("manager = %+v, want the preview view showing the error", model.overlay.pluginManager)
	}
	if !strings.Contains(model.pluginManagerContent(), "changed since preview") {
		t.Fatalf("preview content = %q, want the drift error", model.pluginManagerContent())
	}
	if _, err := os.Stat(appConfigFile(t)); !os.IsNotExist(err) {
		t.Fatalf("drifted enable created config.json (%v)", err)
	}
}

// TestPluginManager_removeConfirmsAndRequiresRestart: picking a
// configured entry and confirming removes it and its trust record with
// the restart-required status; cancelling keeps config untouched.
func TestPluginManager_removeConfirmsAndRequiresRestart(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	helper := setupManagerHelper(t)
	digest := strings.Repeat("ab", 32)
	path := filepath.Join(configDir, "perk-workbench", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"plugins": ["`+helper+`"], "plugin_trust": {"`+helper+`": "`+digest+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshotAppConfig(t)
	SetAppConfig(Config{Plugins: []string{helper}, PluginTrust: map[string]string{helper: digest}})

	model := openPluginManager(t)
	// Menu → Remove.
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay.pluginManager.view != "remove" {
		t.Fatalf("view = %q, want remove", model.overlay.pluginManager.view)
	}
	if !strings.Contains(model.pluginManagerContent(), helper) {
		t.Fatalf("remove content = %q, want the configured entry", model.pluginManagerContent())
	}

	// Cancel the confirmation first: nothing changes.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay.pluginManager.removeConfirm == nil {
		t.Fatal("enter did not open the removal confirmation")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if model.overlay.pluginManager.removeConfirm != nil || model.overlay.pluginManager.view != "remove" {
		t.Fatal("declining the confirmation did not return to the remove view")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), helper) {
		t.Fatalf("declined removal changed the config: %s", contents)
	}

	// Confirm: the entry and its trust record are removed.
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if model.overlay.pluginManager != nil {
		t.Fatal("confirmed removal left the plugin manager open")
	}
	if model.Status != "plugin removed; restart required" {
		t.Fatalf("status = %q, want the restart-required notice", model.Status)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), helper) || strings.Contains(string(contents), "plugin_trust") {
		t.Fatalf("removal left the entry or trust record in config: %s", contents)
	}
	if len(appConfig.Plugins) != 0 {
		t.Fatalf("appConfig.Plugins = %v, want empty after removal", appConfig.Plugins)
	}
}

// TestPluginManager_removeListEmptyIsExplicit: with no configured
// plugins the remove view says so and offers no selection.
func TestPluginManager_removeListEmptyIsExplicit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := openPluginManager(t)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if !strings.Contains(model.pluginManagerContent(), "no plugins configured") {
		t.Fatalf("remove content = %q, want the empty notice", model.pluginManagerContent())
	}
	// Enter on an empty list must not open a confirmation.
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay.pluginManager.removeConfirm != nil || command != nil {
		t.Fatal("enter on an empty remove list opened a confirmation or ran a command")
	}
}
