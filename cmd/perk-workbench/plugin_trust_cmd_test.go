package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench/internal/database/plugin"
)

// writeTrustConfig writes a config.json with plugins and plugin_trust
// under an isolated XDG_CONFIG_HOME and returns the config directory.
func writeTrustConfig(t *testing.T, plugins []string, trust map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	configDir := filepath.Join(dir, "perk-workbench")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{}
	if plugins != nil {
		config["plugins"] = plugins
	}
	if trust != nil {
		config["plugin_trust"] = trust
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return configDir
}

// readConfigFile returns the raw config.json bytes under the current
// XDG_CONFIG_HOME, or nil when absent.
func readConfigFile(t *testing.T) []byte {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		t.Fatal("XDG_CONFIG_HOME is not set")
	}
	contents, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "config.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func TestDispatch_pluginAddRemoveGrammar(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStatus int
		wantStderr string
	}{
		{name: "add missing operand", args: []string{"plugin", "add"}, wantStatus: 2, wantStderr: "expected exactly one executable"},
		{name: "add extra operand", args: []string{"plugin", "add", "a", "b"}, wantStatus: 2, wantStderr: "expected exactly one executable"},
		{name: "add unknown flag", args: []string{"plugin", "add", "--bogus", "x"}, wantStatus: 2, wantStderr: "unknown flag"},
		{name: "add approve missing digest", args: []string{"plugin", "add", "--approve"}, wantStatus: 2, wantStderr: "requires a SHA-256 digest"},
		{name: "add approve flag as digest", args: []string{"plugin", "add", "--approve", "--json", "x"}, wantStatus: 2, wantStderr: "requires a SHA-256 digest"},
		{name: "add approve only valid with add", args: []string{"plugin", "list", "--approve", "x"}, wantStatus: 2, wantStderr: "only valid with add"},
		{name: "remove approve rejected", args: []string{"plugin", "remove", "--approve", "x", "y"}, wantStatus: 2, wantStderr: "only valid with add"},
		{name: "remove missing operand", args: []string{"plugin", "remove"}, wantStatus: 2, wantStderr: "expected exactly one name or executable"},
		{name: "remove extra operand", args: []string{"plugin", "remove", "a", "b"}, wantStatus: 2, wantStderr: "expected exactly one name or executable"},
		{name: "remove unknown flag", args: []string{"plugin", "remove", "--bogus", "x"}, wantStatus: 2, wantStderr: "unknown flag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runCLI(t, test.args...)
			if status != test.wantStatus {
				t.Fatalf("exit status = %d, want %d (stdout %q, stderr %q)", status, test.wantStatus, stdout, stderr)
			}
			if !strings.Contains(stderr, test.wantStderr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr, test.wantStderr)
			}
		})
	}
}

// TestPluginAdd_previewNeverMutatesConfig: without --approve the full
// inspect + fingerprint runs, but no config file is created or touched
// — not even the default materialization — and the output demands a
// rerun with the exact --approve digest.
func TestPluginAdd_previewNeverMutatesConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no config.json exists
	helper := setupPluginHelper(t, nil)

	status, stdout, stderr := runCLI(t, "plugin", "add", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("add preview = %d, stderr %q", status, stderr)
	}
	var result pluginAddResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if !result.OK || result.Phase != "ok" || !result.Pending || result.Changed {
		t.Fatalf("result = %+v, want an ok pending preview with changed=false", result)
	}
	if result.SHA256 == "" || len(result.SHA256) != 64 {
		t.Fatalf("sha256 = %q, want the 64-char fingerprint", result.SHA256)
	}
	digest, err := plugin.SHA256File(helper)
	if err != nil {
		t.Fatal(err)
	}
	if result.SHA256 != digest {
		t.Fatalf("sha256 = %s, want %s", result.SHA256, digest)
	}
	if result.Path != helper || result.Capabilities == nil || result.Capabilities.Name != "clihelper" {
		t.Fatalf("result = %+v, want the resolved helper capabilities", result)
	}
	if config := readConfigFile(t); config != nil {
		t.Fatalf("preview created config.json: %s", config)
	}

	// Human output shows the capabilities, the fingerprint, and the
	// exact rerun instruction with the fingerprint.
	status, stdout, stderr = runCLI(t, "plugin", "add", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("human add preview = %d, stderr %q", status, stderr)
	}
	for _, want := range []string{"capabilities: name=clihelper", "sha256: " + digest, "NOT ENABLED: rerun with --approve " + digest} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("human stdout = %q, want it to contain %q", stdout, want)
		}
	}
	if config := readConfigFile(t); config != nil {
		t.Fatalf("human preview created config.json: %s", config)
	}
}

// TestPluginAdd_approvePinsAtomically: with the exact digest the
// resolve/inspect/hash repeats, the plugin is persisted as its
// canonical path with its trust record, unrelated keys survive, and a
// second approve with the same digest is a no-op (changed=false).
func TestPluginAdd_approvePinsAtomically(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	configPath := filepath.Join(configDir, "perk-workbench", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"theme": "nord", "future_key": {"nested": [1, 2]}}`
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	helper := setupPluginHelper(t, nil)
	digest, err := plugin.SHA256File(helper)
	if err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr := runCLI(t, "plugin", "add", "--json", "--approve", digest, helper)
	if status != 0 || stderr != "" {
		t.Fatalf("add approve = %d, stderr %q", status, stderr)
	}
	var result pluginAddResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if !result.OK || result.Pending || !result.Changed {
		t.Fatalf("result = %+v, want an approved persisted plugin", result)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
	}

	config, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig = %v", err)
	}
	if len(config.Plugins) != 1 || config.Plugins[0] != helper {
		t.Fatalf("Plugins = %v, want the canonical executable path", config.Plugins)
	}
	if config.PluginTrust[helper] != digest {
		t.Fatalf("PluginTrust = %v, want the pinned digest", config.PluginTrust)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"theme": "nord"`) {
		t.Fatalf("config lost the theme key: %s", raw)
	}

	// Idempotent re-approve: nothing changes on disk.
	status, stdout, stderr = runCLI(t, "plugin", "add", "--json", "--approve", digest, helper)
	if status != 0 || stderr != "" {
		t.Fatalf("second approve = %d, stderr %q", status, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Changed {
		t.Fatalf("second approve = %+v, want changed=false", result)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(after) {
		t.Fatal("idempotent approve rewrote the config")
	}
}

// TestPluginAdd_approveDigestMismatchFailsClosed: a wrong --approve
// digest fails with the trust phase and never touches config.
func TestPluginAdd_approveDigestMismatchFailsClosed(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	configPath := filepath.Join(configDir, "perk-workbench", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"theme": "nord"}`
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	helper := setupPluginHelper(t, nil)

	status, stdout, stderr := runCLI(t, "plugin", "add", "--json", "--approve", strings.Repeat("ab", 32), helper)
	if status != 1 || stderr != "" {
		t.Fatalf("add mismatch = %d, stderr %q, want 1 with a JSON document", status, stderr)
	}
	var result pluginAddResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if result.OK || result.Phase != "trust" || result.Changed || !strings.Contains(result.Error, "sha256 mismatch") {
		t.Fatalf("result = %+v, want a fail-closed trust phase", result)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatalf("mismatched approve modified the config: %s", after)
	}
}

// TestPluginAdd_approveDriftBetweenStages: the approve repeats the
// hash; bytes changed since the preview fail the approve closed.
func TestPluginAdd_approveDriftBetweenStages(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	helper := setupPluginHelper(t, nil)
	previewDigest, err := plugin.SHA256File(helper)
	if err != nil {
		t.Fatal(err)
	}

	// Drift the executable between the two stages while keeping it a
	// working helper: the digest must differ while the lifecycle still
	// succeeds, so the approve fails on the digest comparison.
	original, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, append(original, []byte("\n# drifted\n")...), 0o755); err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr := runCLI(t, "plugin", "add", "--json", "--approve", previewDigest, helper)
	if status != 1 || stderr != "" {
		t.Fatalf("drifted approve = %d, stderr %q, want a fail-closed mismatch", status, stderr)
	}
	var result pluginAddResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if result.OK || result.Phase != "trust" || result.Changed {
		t.Fatalf("result = %+v, want the drift refused with no change", result)
	}
	if config := readConfigFile(t); config != nil {
		t.Fatalf("drifted approve created config.json: %s", config)
	}
}

// TestPluginAdd_brokenPluginNeverPersists: an inspect failure (here a
// child crash during initialize) exits 1 and never touches config.
func TestPluginAdd_brokenPluginNeverPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	helper := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "crash"})

	status, stdout, stderr := runCLI(t, "plugin", "add", "--json", "--approve", strings.Repeat("ab", 32), helper)
	if status != 1 || stderr != "" {
		t.Fatalf("add of a crashing plugin = %d, stderr %q", status, stderr)
	}
	var result pluginAddResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if result.OK || result.Phase != "protocol" || result.Changed {
		t.Fatalf("result = %+v, want the protocol failure with no change", result)
	}
	if config := readConfigFile(t); config != nil {
		t.Fatalf("failed add created config.json: %s", config)
	}
}

// TestPluginAdd_replacesExistingEntryWithSameCanonicalPath: adding an
// executable that a configured relative entry already names re-pins
// that entry to its canonical path instead of duplicating it.
func TestPluginAdd_replacesExistingEntryWithSameCanonicalPath(t *testing.T) {
	configDir := writeConfig(t, []string{"./relative-helper"})
	helper := writePluginHelperScriptAt(t, filepath.Join(configDir, "relative-helper"))
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	digest, err := plugin.SHA256File(helper)
	if err != nil {
		t.Fatal(err)
	}

	status, _, stderr := runCLI(t, "plugin", "add", "--json", "--approve", digest, helper)
	if status != 0 || stderr != "" {
		t.Fatalf("add = %d, stderr %q", status, stderr)
	}
	config, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Plugins) != 1 || config.Plugins[0] != helper {
		t.Fatalf("Plugins = %v, want the relative entry replaced by %q", config.Plugins, helper)
	}
	if config.PluginTrust[helper] != digest {
		t.Fatalf("PluginTrust = %v, want the pin", config.PluginTrust)
	}
}

// TestPluginRemove_byEntryStringAndByPath: remove matches the exact
// configured entry, or an executable resolving to exactly one entry,
// and drops the trust record while preserving unrelated keys.
func TestPluginRemove_byEntryStringAndByPath(t *testing.T) {
	dir := t.TempDir()
	helper := writeExecutableAt(t, filepath.Join(dir, "perk-redis"))
	digest := strings.Repeat("cd", 32)
	configDir := writeTrustConfig(t, []string{helper}, map[string]string{helper: digest})

	// By exact entry string.
	status, stdout, stderr := runCLI(t, "plugin", "remove", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("remove = %d, stderr %q", status, stderr)
	}
	var result pluginRemoveResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if !result.OK || !result.Changed || result.Entry != helper || result.Path != helper {
		t.Fatalf("result = %+v, want the entry removed", result)
	}
	config, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Plugins) != 0 || len(config.PluginTrust) != 0 {
		t.Fatalf("config = %+v, want empty plugins and trust after removal", config)
	}

	// Re-add, then remove by the canonical path.
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"plugins": ["`+helper+`"], "plugin_trust": {"`+helper+`": "`+digest+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, stdout, stderr = runCLI(t, "plugin", "remove", "--json", filepath.Join(dir, "perk-redis"))
	if status != 0 || stderr != "" {
		t.Fatalf("remove by path = %d, stderr %q", status, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.Changed {
		t.Fatalf("remove by path = %+v, want removed", result)
	}

	// Human output reports the not-configured failure on stdout.
	status, stdout, stderr = runCLI(t, "plugin", "remove", filepath.Join(dir, "perk-redis"))
	if status != 1 || stderr != "" {
		t.Fatalf("human remove = %d, stderr %q, want 1", status, stderr)
	}
	if !strings.Contains(stdout, "not configured") {
		t.Fatalf("removing an absent plugin = %q, want a not-configured report", stdout)
	}
}

func TestPluginRemove_failuresLeaveConfigUntouched(t *testing.T) {
	dir := t.TempDir()
	real := writeExecutableAt(t, filepath.Join(dir, "real-plugin"))
	link := filepath.Join(dir, "linked-plugin")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	configDir := writeTrustConfig(t, []string{real, link}, nil)
	configPath := filepath.Join(configDir, "config.json")
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// Ambiguous: a third alias resolves to both configured entries.
	alias := filepath.Join(dir, "alias-plugin")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	status, stdout, stderr := runCLI(t, "plugin", "remove", "--json", alias)
	if status != 1 || stderr != "" {
		t.Fatalf("ambiguous remove = %d, stderr %q", status, stderr)
	}
	var result pluginRemoveResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if result.OK || result.Changed || !strings.Contains(result.Error, "ambiguous") {
		t.Fatalf("result = %+v, want an ambiguous failure", result)
	}

	// Not configured.
	status, _, _ = runCLI(t, "plugin", "remove", "--json", filepath.Join(dir, "no-such"))
	if status != 1 {
		t.Fatalf("not-configured remove = %d, want 1", status)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(after) {
		t.Fatalf("failed removes modified the config: %s", after)
	}
}

// TestPluginList_reportsTrustState: list reports unpinned entries and
// pinned entries with their configured fingerprint, without hashing.
func TestPluginList_reportsTrustState(t *testing.T) {
	dir := t.TempDir()
	first := writeExecutableAt(t, filepath.Join(dir, "first-plugin"))
	second := writeExecutableAt(t, filepath.Join(dir, "second-plugin"))
	digest := strings.Repeat("ef", 32)
	writeTrustConfig(t, []string{first, second}, map[string]string{first: digest})

	status, stdout, stderr := runCLI(t, "plugin", "list", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("list = %d, stderr %q", status, stderr)
	}
	var items []struct {
		Entry    string `json:"entry"`
		Trust    string `json:"trust"`
		Expected string `json:"expected_sha256"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v, want both entries", items)
	}
	if items[0].Trust != "pinned" || items[0].Expected != digest {
		t.Fatalf("items[0] = %+v, want pinned with the fingerprint", items[0])
	}
	if items[1].Trust != "unpinned" || items[1].Expected != "" {
		t.Fatalf("items[1] = %+v, want unpinned", items[1])
	}

	status, stdout, stderr = runCLI(t, "plugin", "list")
	if status != 0 || stderr != "" {
		t.Fatalf("human list = %d, stderr %q", status, stderr)
	}
	if !strings.Contains(stdout, first+" -> "+first+" [pinned sha256:"+digest+"]") {
		t.Fatalf("human stdout = %q, want the pinned fingerprint line", stdout)
	}
	if !strings.Contains(stdout, second+" -> "+second+" [unpinned]") {
		t.Fatalf("human stdout = %q, want the unpinned line", stdout)
	}
}

// TestPluginInspect_trustMatchAndMismatch: a matching pin reports
// match and runs the lifecycle; a drifted pin fails with the trust
// phase BEFORE the child spawns (proven by the marker file).
func TestPluginInspect_trustMatchAndMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	marker := filepath.Join(dir, "events.log")
	match := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_MARKER": marker})
	digest, err := plugin.SHA256File(match)
	if err != nil {
		t.Fatal(err)
	}
	drifted := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_MARKER": marker})
	pin, err := plugin.SHA256File(drifted)
	if err != nil {
		t.Fatal(err)
	}
	driftedBytes, err := os.ReadFile(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(drifted, append(driftedBytes, []byte("\n# drifted\n")...), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(dir, "perk-workbench")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	trust := map[string]string{match: digest, drifted: pin}
	data, err := json.Marshal(map[string]any{"plugin_trust": trust})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr := runCLI(t, "plugin", "inspect", "--json", match)
	if status != 0 || stderr != "" {
		t.Fatalf("inspect match = %d, stderr %q", status, stderr)
	}
	var report pluginReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if !report.OK || report.Trust != "match" || report.SHA256 != digest || report.Expected != digest {
		t.Fatalf("report = %+v, want a verified match", report)
	}

	status, stdout, stderr = runCLI(t, "plugin", "inspect", "--json", drifted)
	if status != 1 || stderr != "" {
		t.Fatalf("inspect drift = %d, stderr %q, want a trust failure", status, stderr)
	}
	var driftedReport pluginReport
	if err := json.Unmarshal([]byte(stdout), &driftedReport); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if driftedReport.OK || driftedReport.Phase != "trust" || driftedReport.Trust != "mismatch" {
		t.Fatalf("report = %+v, want a trust-phase mismatch", driftedReport)
	}
	if !strings.Contains(driftedReport.Error, "expected sha256 "+pin) || !strings.Contains(driftedReport.Error, "got ") {
		t.Fatalf("error = %q, want the expected/actual digests", driftedReport.Error)
	}
	if driftedReport.Capabilities != nil || driftedReport.Snapshot != nil {
		t.Fatalf("report = %+v, want no lifecycle artifacts for a refused child", driftedReport)
	}
	// Only the matching child ever spawned.
	if starts := markerLineCount(t, marker, "start"); starts != 1 {
		t.Fatalf("marker records %d child starts, want exactly the matching one (the drifted child must never execute)", starts)
	}
}

// TestPluginDoctor_pinnedMismatchFailsItemBeforeSpawn: doctor checks
// every entry independently; a pinned drift fails its item with the
// trust phase and the child never spawns, while later items still run.
func TestPluginDoctor_pinnedMismatchFailsItemBeforeSpawn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	marker := filepath.Join(dir, "events.log")
	good := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_MARKER": marker})
	drifted := setupPluginHelper(t, map[string]string{"PERK_PLUGIN_MARKER": marker})
	pin, err := plugin.SHA256File(drifted)
	if err != nil {
		t.Fatal(err)
	}
	driftedBytes, err := os.ReadFile(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(drifted, append(driftedBytes, []byte("\n# drifted\n")...), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(dir, "perk-workbench")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"plugins":      []string{good, drifted},
		"plugin_trust": map[string]string{drifted: pin},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr := runCLI(t, "plugin", "doctor", "--json")
	if status != 1 || stderr != "" {
		t.Fatalf("doctor = %d, stderr %q, want 1 with a JSON document", status, stderr)
	}
	var reports []pluginReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports = %d items, want one per entry", len(reports))
	}
	if !reports[0].OK || reports[0].Trust != "unpinned" {
		t.Fatalf("reports[0] = %+v, want the unpinned legacy entry ok", reports[0])
	}
	if reports[1].OK || reports[1].Phase != "trust" || reports[1].Trust != "mismatch" {
		t.Fatalf("reports[1] = %+v, want the drifted entry refused", reports[1])
	}
	if starts := markerLineCount(t, marker, "start"); starts != 1 {
		t.Fatalf("marker records %d child starts, want only the healthy entry's", starts)
	}
}
