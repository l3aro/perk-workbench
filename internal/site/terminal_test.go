package site

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExtractDemoDB_writesValidSQLiteFile(t *testing.T) {
	path, err := extractDemoDB()
	if err != nil {
		t.Fatalf("extracting demo database: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading extracted database: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("SQLite format 3\x00")) {
		t.Fatalf("extracted file is not a SQLite database (first 16 bytes %q)", data[:16])
	}
	if len(data) != len(demoDatabase) {
		t.Fatalf("extracted size %d, want %d", len(data), len(demoDatabase))
	}
}

func TestTerminalServer_missingBinaryReturns503(t *testing.T) {
	server := &terminalServer{bin: filepath.Join(t.TempDir(), "does-not-exist")}
	req := httptest.NewRequest(http.MethodGet, "/ws/tui", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestTerminalServer_configuredBinAvoidsLookPath(t *testing.T) {
	// A relative bin path is resolved against PATH only when empty; an explicit
	// path must reach the executable check unchanged (here: missing -> 503).
	server := &terminalServer{bin: "definitely-not-a-real-binary"}
	req := httptest.NewRequest(http.MethodGet, "/ws/tui", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSplitUTF8_keepsIncompleteRunesPending(t *testing.T) {
	// "é" is U+00E9 = 0xC3 0xA9. A read ending after 0xC3 must hold it back.
	valid, rest := splitUTF8([]byte("abc\xc3"))
	if string(valid) != "abc" {
		t.Fatalf("valid = %q, want %q", valid, "abc")
	}
	if !bytes.Equal(rest, []byte{0xc3}) {
		t.Fatalf("rest = %v, want [195]", rest)
	}

	valid, rest = splitUTF8([]byte("abc\xc3\xa9"))
	if string(valid) != "abcé" || len(rest) != 0 {
		t.Fatalf("complete rune split = %q rest %v, want %q", valid, rest, "abcé")
	}

	valid, rest = splitUTF8([]byte("abc"))
	if string(valid) != "abc" || len(rest) != 0 {
		t.Fatalf("ascii split = %q rest %v", valid, rest)
	}
}

func TestDemoCommandArgs_pinsReadOnlySession(t *testing.T) {
	got := demoCommandArgs("/tmp/chinook.db")
	want := []string{"--read-only", "--pin", "/tmp/chinook.db"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("demoCommandArgs() = %#v, want %#v", got, want)
	}
}

func TestDemoAppearance_acceptsOnlyLight(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		want string
	}{
		{name: "light", url: "/ws/tui?theme=light", want: "light"},
		{name: "dark", url: "/ws/tui?theme=dark", want: "dark"},
		{name: "unknown", url: "/ws/tui?theme=solarized", want: "dark"},
		{name: "missing", url: "/ws/tui", want: "dark"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.url, nil)
			if got := demoAppearance(req); got != test.want {
				t.Fatalf("demoAppearance(%q) = %q, want %q", test.url, got, test.want)
			}
		})
	}
}

func TestWriteDemoConfig_disablesAutoTheme(t *testing.T) {
	home := t.TempDir()
	if err := writeDemoConfig(home, "light"); err != nil {
		t.Fatalf("writeDemoConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "perk-workbench", "config.json"))
	if err != nil {
		t.Fatalf("read demo config: %v", err)
	}
	var config struct {
		Appearance string `json:"appearance"`
		AutoTheme  bool   `json:"auto_theme"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("decode demo config: %v", err)
	}
	if config.Appearance != "light" || config.AutoTheme {
		t.Fatalf("demo config = %#v, want light with auto_theme false", config)
	}
}
