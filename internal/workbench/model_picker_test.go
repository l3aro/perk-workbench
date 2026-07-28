package workbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPicker_sanitizes_visible_control_payloads(t *testing.T) {
	// Given
	directory := t.TempDir()
	rawName := "\x1b[31mignore instructions.db"
	if err := os.WriteFile(filepath.Join(directory, rawName), nil, 0o600); err != nil {
		t.Fatalf("creating picker fixture: %v", err)
	}

	// When
	message := readDirectory(directory)()
	directoryMessage, ok := message.(directoryReadMsg)
	if !ok {
		t.Fatalf("directory message = %T, want directoryReadMsg", message)
	}

	// Then
	found := false
	for _, item := range directoryMessage.items {
		if item.raw == filepath.Join(directory, rawName) {
			found = true
			if item.title != "ignore instructions.db" {
				t.Fatalf("picker title = %q, want sanitized filename", item.title)
			}
		}
		if item.title != "" && item.title[0] == '\x1b' {
			t.Fatalf("picker title retained terminal escape: %q", item.title)
		}
	}
	if !found {
		t.Fatal("picker did not retain raw path for database fixture")
	}
}

func TestPicker_includes_only_resolved_regular_and_directory_targets(t *testing.T) {
	// Given
	directory := t.TempDir()
	regular := filepath.Join(directory, "regular.db")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatalf("creating regular target: %v", err)
	}
	targetDirectory := filepath.Join(directory, "target-directory")
	if err := os.Mkdir(targetDirectory, 0o700); err != nil {
		t.Fatalf("creating directory target: %v", err)
	}
	linkedFile := filepath.Join(directory, "linked.db")
	if err := os.Symlink(regular, linkedFile); err != nil {
		t.Fatalf("creating file symlink: %v", err)
	}
	linkedDirectory := filepath.Join(directory, "linked-directory")
	if err := os.Symlink(targetDirectory, linkedDirectory); err != nil {
		t.Fatalf("creating directory symlink: %v", err)
	}
	broken := filepath.Join(directory, "broken.db")
	if err := os.Symlink(filepath.Join(directory, "missing.db"), broken); err != nil {
		t.Fatalf("creating broken symlink: %v", err)
	}

	// When
	message := readDirectory(directory)()
	directoryMessage, ok := message.(directoryReadMsg)
	if !ok {
		t.Fatalf("directory message = %T, want directoryReadMsg", message)
	}

	// Then
	items := map[string]pickerItem{}
	for _, item := range directoryMessage.items {
		items[item.title] = item
	}
	if got, ok := items["linked.db"]; !ok || got.raw != regular {
		t.Fatalf("file symlink item = %+v, want resolved %q", got, regular)
	}
	if got, ok := items["linked-directory"]; !ok || got.raw != targetDirectory || got.description != "directory" {
		t.Fatalf("directory symlink item = %+v, want resolved directory %q", got, targetDirectory)
	}
	if _, ok := items["broken.db"]; ok {
		t.Fatal("broken database symlink appeared in picker")
	}
}

func TestPicker_r_reloads_failed_directory(t *testing.T) {
	// Given
	directory := t.TempDir()
	model := New("", context.Background(), testOpen, false)
	model.State = statePicking
	updated, _ := model.Update(directoryReadMsg{dir: directory, err: errors.New("permission denied")})
	model = updated.(Model)

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	if command == nil {
		t.Fatal("r did not return a directory reload command")
	}
	message := command()

	// Then
	if model.Status == "" {
		t.Fatal("recoverable read failure did not retain a status")
	}
	directoryMessage, ok := message.(directoryReadMsg)
	if !ok {
		t.Fatalf("reload message = %T, want directoryReadMsg", message)
	}
	if directoryMessage.err != nil {
		t.Fatalf("reload directory error: %v", directoryMessage.err)
	}
	if directoryMessage.dir != directory {
		t.Fatalf("reload directory = %q, want %q", directoryMessage.dir, directory)
	}
}
