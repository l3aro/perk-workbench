//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func main() {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "plugins", "official-manifest.json"))
	if err != nil {
		panic(err)
	}
	output := fmt.Sprintf("// Code generated from plugins/official-manifest.json; DO NOT EDIT.\n\npackage main\n\nvar officialManifestData = []byte(%s)\n", strconv.Quote(string(manifest)))
	if err := os.WriteFile("official_manifest_generated.go", []byte(output), 0o644); err != nil {
		panic(err)
	}
}
