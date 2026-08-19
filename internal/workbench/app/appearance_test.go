package app

import (
	"context"
	"testing"
)

// snapshotThemeState captures the global theme/appearance state so a test
// that mutates it restores the environment for the tests that follow.
func snapshotThemeState(t *testing.T) {
	t.Helper()
	prevConfig := appConfig
	prevTheme := activeTheme
	prevScheme := runtimeScheme
	prevRuntimeTheme := runtimeTheme
	prevDetected := detectedScheme
	t.Cleanup(func() {
		appConfig = prevConfig
		setTheme(prevTheme)
		runtimeScheme = prevScheme
		runtimeTheme = prevRuntimeTheme
		detectedScheme = prevDetected
	})
}

func TestAppearanceFromBackground(t *testing.T) {
	for _, tc := range []struct {
		payload string
		want    string
	}{
		{"rgb:0000/0000/0000", "dark"},
		{"rgb:1c1c/1c1c/1c1c", "dark"},
		{"rgb:3030/3030/3030", "dark"},
		{"rgb:7c7c/7c7c/7c7c", "dark"},  // ~43% gray -> below threshold
		{"rgb:9999/9999/9999", "dark"},  // 60% gray -> linearized ~0.33 still dark
		{"rgb:cccc/cccc/cccc", "light"}, // ~80% gray -> linearized light
		{"rgb:dddd/dddd/dddd", "light"},
		{"rgb:ffff/ffff/ffff", "light"},
		{"rgb:ff/ff/ff", "light"}, // 8-bit variant
		{"rgb:f/f/f", "light"},    // 4-bit variant
		{"garbage", ""},
		{"rgb:abc/def/ghi", ""}, // non-hex component
		{"", ""},
	} {
		if got := AppearanceFromBackground(tc.payload); got != tc.want {
			t.Fatalf("AppearanceFromBackground(%q) = %q, want %q", tc.payload, got, tc.want)
		}
	}
}

func TestResolveScheme(t *testing.T) {
	original := detectedScheme
	t.Cleanup(func() { detectedScheme = original })

	// Auto off uses the persisted appearance directly.
	if got := resolveScheme(Config{AutoTheme: boolPtr(false), Appearance: "light"}); got != schemeLight {
		t.Fatalf("resolveScheme(auto off, light) = %q, want light", got)
	}
	// Auto on prefers system detection.
	detectedScheme = schemeDark
	if got := resolveScheme(Config{AutoTheme: boolPtr(true)}); got != schemeDark {
		t.Fatalf("resolveScheme(auto, detected dark) = %q, want dark", got)
	}
	// Auto on falls back to persisted appearance when detection is absent.
	detectedScheme = ""
	if got := resolveScheme(Config{AutoTheme: boolPtr(true), Appearance: "light"}); got != schemeLight {
		t.Fatalf("resolveScheme(auto, no detect, light fallback) = %q, want light", got)
	}
	// Auto on with no detection and no appearance falls back to dark.
	if got := resolveScheme(Config{AutoTheme: boolPtr(true)}); got != schemeDark {
		t.Fatalf("resolveScheme(auto, no detect, no appearance) = %q, want dark", got)
	}
}

func TestThemeForScheme_repairsMismatchedSlot(t *testing.T) {
	// A hand-edited config that puts a light theme in the dark slot (or vice
	// versa) is repaired to that scheme's default, never accepted as-is.
	if got := themeForScheme(schemeDark, Config{DarkTheme: "light-ocean"}); got != themeOcean {
		t.Fatalf("themeForScheme(dark, light-ocean) = %q, want ocean (repaired)", got)
	}
	if got := themeForScheme(schemeLight, Config{LightTheme: "ocean"}); got != themeLightOcean {
		t.Fatalf("themeForScheme(light, ocean) = %q, want light-ocean (repaired)", got)
	}
	// Valid slots pass through.
	if got := themeForScheme(schemeDark, Config{DarkTheme: "nord"}); got != themeNord {
		t.Fatalf("themeForScheme(dark, nord) = %q, want nord", got)
	}
	if got := themeForScheme(schemeLight, Config{LightTheme: "light-nord"}); got != themeLightNord {
		t.Fatalf("themeForScheme(light, light-nord) = %q, want light-nord", got)
	}
}

func TestAppearanceToggle_persistsWhenAutoOff(t *testing.T) {
	snapshotThemeState(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Explicit dark, auto off: toggling flips the scheme and persists it.
	SetAppConfig(Config{AutoTheme: boolPtr(false), Appearance: "dark", DarkTheme: "ocean", LightTheme: "light-nord"})
	model := New("", context.Background(), testOpen, false)
	// Establish a consistent on-disk state (auto already off) before toggling.
	if err := SaveAutoTheme(model.configPath, false); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppearance(model.configPath, "dark"); err != nil {
		t.Fatal(err)
	}
	model.toggleAppearance()

	if runtimeScheme != schemeLight {
		t.Fatalf("runtimeScheme = %q, want light after toggle", runtimeScheme)
	}
	if activeTheme != themeLightNord {
		t.Fatalf("activeTheme = %q, want light-nord", activeTheme)
	}
	config, err := LoadConfig(model.configPath)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if config.Appearance != "light" {
		t.Fatalf("persisted appearance = %q, want light", config.Appearance)
	}
	if config.AutoTheme == nil || *config.AutoTheme {
		t.Fatalf("persisted auto_theme = %v, want false (toggle turned it off and pinned)", config.AutoTheme)
	}
}

func TestAppearanceToggle_sessionOnlyWhenAutoOn(t *testing.T) {
	snapshotThemeState(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Auto-following dark (no detection): toggling is a session-only override
	// that must not rewrite the persisted appearance.
	SetAppConfig(Config{AutoTheme: boolPtr(true), Appearance: "dark", DarkTheme: "ocean", LightTheme: "light-nord"})
	model := New("", context.Background(), testOpen, false)
	// Establish a consistent on-disk state: auto on, appearance dark.
	if err := SaveAutoTheme(model.configPath, true); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppearance(model.configPath, "dark"); err != nil {
		t.Fatal(err)
	}
	model.toggleAppearance()

	if runtimeScheme != schemeLight {
		t.Fatalf("runtimeScheme = %q, want light after toggle", runtimeScheme)
	}
	if activeTheme != themeLightNord {
		t.Fatalf("activeTheme = %q, want light-nord", activeTheme)
	}
	config, err := LoadConfig(model.configPath)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if config.Appearance != "dark" {
		t.Fatalf("persisted appearance = %q, want untouched dark (session-only override)", config.Appearance)
	}
}

func TestAppearancePicker_commitsExplicitAndAuto(t *testing.T) {
	snapshotThemeState(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Start auto; pick "light" via the picker.
	SetAppConfig(Config{AutoTheme: boolPtr(true), Appearance: "dark", DarkTheme: "ocean", LightTheme: "light-nord"})
	model := New("", context.Background(), testOpen, false)
	picker := newAppearancePicker()
	picker.selected = 1 // light
	model.commitAppearance(picker.value())

	config, err := LoadConfig(model.configPath)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if config.Appearance != "light" || config.AutoTheme == nil || *config.AutoTheme {
		t.Fatalf("pinned appearance = %q auto=%v, want light/false", config.Appearance, config.AutoTheme)
	}
	if runtimeScheme != schemeLight || activeTheme != themeLightNord {
		t.Fatalf("runtime = %q / %q, want light / light-nord", runtimeScheme, activeTheme)
	}

	// Pick "auto" to re-enable following; with no detection it falls back to
	// persisted appearance, keeping auto enabled.
	picker.selected = 0 // auto
	model.commitAppearance(picker.value())
	config, err = LoadConfig(model.configPath)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if config.AutoTheme == nil || !*config.AutoTheme {
		t.Fatalf("auto_theme = %v, want true after choosing auto", config.AutoTheme)
	}
}
