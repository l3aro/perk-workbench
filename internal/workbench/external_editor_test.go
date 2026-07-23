package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestModel_ctrlEEditsFocusedText(t *testing.T) {
	// Given
	editor := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf 'SELECT 2' > \"$1\"\n"), 0o700); err != nil {
		t.Fatalf("writing editor script: %v", err)
	}
	t.Setenv("EDITOR", editor)
	t.Setenv("TMPDIR", t.TempDir())
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	model = updated.(Model)
	if command == nil {
		t.Fatal("SQL Huh Text Ctrl+E returned no editor command")
	}
	process, complete, err := sqlEditorProcess(model.editor.value, model.editorEditTag)
	if err != nil {
		t.Fatalf("creating SQL editor process: %v", err)
	}
	updated, _ = model.Update(complete(process.Run()))
	model = updated.(Model)

	// Then
	if got := model.editor.value; got != "SELECT 2" {
		t.Fatalf("SQL Huh Text editor value = %q, want SELECT 2", got)
	}
}

func TestModel_sqlEditorCompletionReportsError(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model.formMode.beginInsert(model.editor)
	model.editorEditTag = 1

	// When
	updated, _ := model.Update(sqlEditorFinishedMsg{tag: 1, err: errors.New("editor failed")})
	model = updated.(Model)

	// Then
	if !strings.Contains(model.Status, "editor failed") {
		t.Fatalf("editor error status = %q", model.Status)
	}
}

func TestModel_sqlEditorCompletionReportsStaleTarget(t *testing.T) {
	// Given
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model.formMode.beginInsert(model.editor)
	model.editorEditTag = 1
	model.Focus = focusSchema

	// When
	updated, _ := model.Update(sqlEditorFinishedMsg{tag: 1, value: "SELECT 2"})
	model = updated.(Model)

	// Then
	if got := model.Status; got != "editor target is no longer focused" {
		t.Fatalf("stale editor status = %q", got)
	}
}

func TestModel_ctrlEIgnoresConnectionInput(t *testing.T) {
	// Given
	t.Setenv("EDITOR", "true")
	model := readyModel(t)
	model.State = stateConnection
	model.connection.setFocus(connectionFocusForm)
	model.connection.values.name = "before"
	model.formMode.beginHuh(model.connection.focusForm())

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	model = updated.(Model)

	// Then
	if command != nil {
		t.Fatal("connection Ctrl+E returned an editor command")
	}
	if got := model.connection.values.name; got != "before" {
		t.Fatalf("connection value = %q, want unchanged value", got)
	}
}
