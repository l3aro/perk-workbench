// Package perkv1 embeds the canonical machine-readable perk/v1
// contract: the JSON Schema document and the fixture frames plus their
// manifest, all under protocol/perk-v1. It is the single compiled-in
// source for tooling such as the plugin conformance runner, so the
// contract is never copied into a binary or another directory.
package perkv1

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed schema.json
var schema []byte

//go:embed fixtures/*.json
var fixtures embed.FS

// Schema returns the canonical perk/v1 JSON Schema document.
func Schema() []byte {
	return schema
}

// Manifest returns the fixture manifest (fixtures/manifest.json): the
// description of every canonical frame, its file name, validity, schema
// $ref, and the expected method, error code, kind, and rejection where
// applicable.
func Manifest() ([]byte, error) {
	return fixtures.ReadFile("fixtures/manifest.json")
}

// Fixture returns one canonical fixture frame by file name — the
// manifest entry's "file" value. The frame is a complete wire frame
// (newline-terminated JSON).
func Fixture(name string) ([]byte, error) {
	data, err := fixtures.ReadFile("fixtures/" + name)
	if err != nil {
		return nil, fmt.Errorf("perk/v1 fixture %q: %w", name, err)
	}
	return data, nil
}

// FixtureNames lists every fixture file embedded in this package, in
// sorted order. Tests use it to prove the fixture directory and the
// manifest never drift apart.
func FixtureNames() ([]string, error) {
	entries, err := fixtures.ReadDir("fixtures")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") && entry.Name() != "manifest.json" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Source is the canonical protocol-asset provider for the conformance
// engine. It implements the engine's source contract with the embedded
// assets.
type Source struct{}

// Schema returns the embedded JSON Schema document.
func (Source) Schema() []byte { return Schema() }

// Manifest returns the embedded fixture manifest.
func (Source) Manifest() ([]byte, error) { return Manifest() }

// Fixture returns one embedded fixture frame by file name.
func (Source) Fixture(name string) ([]byte, error) { return Fixture(name) }
