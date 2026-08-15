package perkv1

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEmbeddedContract pins the embedded canonical assets to each
// other: the schema parses, the manifest parses and every entry names a
// fixture that exists in the embed and parses as JSON, every embedded
// fixture is listed in the manifest (nothing may drift out of it), and
// every fixture frame is one LF-terminated wire frame.
func TestEmbeddedContract(t *testing.T) {
	if len(Schema()) == 0 || !json.Valid(Schema()) {
		t.Fatal("schema.json is missing or not parseable JSON")
	}
	var manifest struct {
		Fixtures []struct {
			File string `json:"file"`
		} `json:"fixtures"`
	}
	raw, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("manifest does not parse: %v", err)
	}
	if len(manifest.Fixtures) == 0 {
		t.Fatal("manifest lists no fixtures")
	}

	names, err := FixtureNames()
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, entry := range manifest.Fixtures {
		if entry.File == "" {
			t.Fatal("manifest entry with a blank file name")
		}
		if listed[entry.File] {
			t.Fatalf("manifest lists %q twice", entry.File)
		}
		listed[entry.File] = true
		frame, err := Fixture(entry.File)
		if err != nil {
			t.Fatalf("fixture %q: %v", entry.File, err)
		}
		if !json.Valid(frame) {
			t.Fatalf("fixture %q is not parseable JSON", entry.File)
		}
		text := string(frame)
		if !strings.HasSuffix(text, "\n") || strings.HasSuffix(strings.TrimSuffix(text, "\n"), "\n") {
			t.Fatalf("fixture %q must be exactly one LF-terminated frame", entry.File)
		}
	}
	for _, name := range names {
		if !listed[name] {
			t.Fatalf("fixture %q is not listed in the manifest", name)
		}
	}
}
