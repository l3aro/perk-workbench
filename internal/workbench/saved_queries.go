package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"charm.land/huh/v2"
)

const (
	maxSavedQueries    = 100
	maxSavedQueryRunes = 10_000
)

type savedQueryPicker struct {
	form      *huh.Form
	selection string
}

func newSavedQueryPicker(queries []string, width int) *savedQueryPicker {
	if len(queries) == 0 {
		return nil
	}
	picker := &savedQueryPicker{selection: queries[0]}
	options := make([]huh.Option[string], len(queries))
	for index, query := range queries {
		options[index] = huh.NewOption(safeText(query), query)
	}
	picker.form = huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Key("saved-query").Title("Saved queries").Options(options...).Value(&picker.selection),
	)).WithShowHelp(width >= 40).WithWidth(max(width, 1))
	return picker
}

func savedQueriesPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk", "saved-queries.json"), nil
}

func loadSavedQueries(path string) []string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var queries []string
	if json.Unmarshal(contents, &queries) != nil {
		return nil
	}
	result := make([]string, 0, min(len(queries), maxSavedQueries))
	for _, query := range queries {
		if strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > maxSavedQueryRunes {
			continue
		}
		result = append(result, query)
		if len(result) == maxSavedQueries {
			break
		}
	}
	return result
}

func saveSavedQueries(path string, queries []string) error {
	contents, err := json.Marshal(queries)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "saved-queries-*.json")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
