package sql

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestQueryLanguageCommands_wireOmission: the commands field is omitted
// on the wire for nil and empty catalogs (legacy behavior preserved)
// and present with a catalog, and survives a full JSON round trip
// through the shared DTO.
func TestQueryLanguageCommands_wireOmission(t *testing.T) {
	for _, language := range []QueryLanguage{
		{},
		{Name: "SQL", EditorLabel: "SQL", Placeholder: "Enter a query…"},
		{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: nil},
		{Name: "KV", EditorLabel: "Command", Placeholder: "Enter a command…", Commands: []QueryCommand{}},
	} {
		data, err := json.Marshal(language)
		if err != nil {
			t.Fatalf("marshal %+v: %v", language, err)
		}
		if strings.Contains(string(data), "commands") {
			t.Fatalf("marshal %+v = %s, want commands omitted", language, data)
		}
	}

	language := QueryLanguage{
		Name:        "KV",
		EditorLabel: "Command",
		Placeholder: "Enter a command…",
		Commands: []QueryCommand{
			{Name: "GET", Usage: "GET key", Summary: "Get the value at key"},
			{Name: "SET", Usage: "SET key value", Summary: "Set the value at key"},
		},
	}
	data, err := json.Marshal(language)
	if err != nil {
		t.Fatalf("marshal with commands: %v", err)
	}
	var decoded QueryLanguage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Commands) != 2 || decoded.Commands[0] != language.Commands[0] || decoded.Commands[1] != language.Commands[1] {
		t.Fatalf("round trip commands = %+v, want %+v", decoded.Commands, language.Commands)
	}
	if !strings.Contains(string(data), `"commands":[{"name":"GET","usage":"GET key","summary":"Get the value at key"}`) {
		t.Fatalf("marshal with commands = %s, want the catalog inline", data)
	}

	// An explicit empty catalog stays a zero advertisement, like every
	// other empty field.
	if !IsZeroQueryLanguage(QueryLanguage{Commands: []QueryCommand{}}) {
		t.Fatal("empty command list must not turn a zero advertisement nonzero")
	}
}

// TestValidateQueryCommands_asciiNamesAreTotalForUniqueness: only ASCII
// letters/digits/underscores are accepted, so lowercase folding covers
// the whole allowed name space — two names that differ only in case are
// always duplicates, and case-folding collisions outside ASCII cannot
// slip through.
func TestValidateQueryCommands_asciiNamesAreTotalForUniqueness(t *testing.T) {
	valid := []QueryCommand{
		{Name: "GET", Usage: "GET key", Summary: "Get"},
		{Name: "SET", Usage: "SET key value", Summary: "Set"},
		{Name: "FOO_BAR2", Usage: "FOO_BAR2 arg", Summary: "S"},
	}
	if err := ValidateQueryCommands("KV", valid); err != nil {
		t.Fatalf("valid ASCII catalog rejected: %v", err)
	}
	for _, name := range []string{"GÉT", "ΣΕΤ", "gEt ", "GE-T", "GE.T"} {
		if err := ValidateQueryCommands("KV", []QueryCommand{{Name: name, Usage: "x", Summary: "x"}}); err == nil {
			t.Fatalf("name %q accepted, want rejection", name)
		}
	}
}
