package workbench

import (
	"testing"
)

// TestSQLValidation_borderTracksStatementValidity drives the debounce → async
// prepare → border-color pipeline end to end against a real in-memory schema.
func TestSQLValidation_borderTracksStatementValidity(t *testing.T) {
	// Given — a pending editor value that does not exist in the schema.
	model := readyModel(t)
	model.queryLog.editor.setValue("SELECT * FROM missing_table")
	model.queryLog.editorValidity = sqlValidityPending

	// When — the debounce tick fires and the validation round trips.
	updated, command := model.Update(model.scheduleSQLValidation()())
	model = updated.(Model)
	if command == nil {
		t.Fatal("tick produced no validation command")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)

	// Then — danger border for an invalid statement.
	if model.queryLog.editorValidity != sqlValidityInvalid {
		t.Fatalf("editor validity = %v, want invalid", model.queryLog.editorValidity)
	}
	if got, want := model.editorBorderColor(), colorDanger; got != want {
		t.Fatalf("border color = %q, want %q", got, want)
	}

	// When — the statement becomes valid.
	model.queryLog.editor.setValue("SELECT 1")
	model.queryLog.editorValidity = sqlValidityPending
	updated, command = model.Update(model.scheduleSQLValidation()())
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)

	// Then — success border.
	if model.queryLog.editorValidity != sqlValidityValid {
		t.Fatalf("editor validity = %v, want valid", model.queryLog.editorValidity)
	}
	if got, want := model.editorBorderColor(), colorSuccess; got != want {
		t.Fatalf("border color = %q, want %q", got, want)
	}

	// When — the editor is emptied before the check lands.
	model.queryLog.editor.setValue("")
	updated, command = model.Update(model.scheduleSQLValidation()())
	model = updated.(Model)
	if command != nil {
		t.Fatalf("empty editor produced a validation command")
	}

	// Then — neutral border, no validation run.
	if model.queryLog.editorValidity != sqlValidityPending {
		t.Fatalf("editor validity = %v, want pending", model.queryLog.editorValidity)
	}
	if got, want := model.editorBorderColor(), colorBorder; got != want {
		t.Fatalf("border color = %q, want %q", got, want)
	}
}

// TestSQLValidation_staleResultIsDropped guards the stale-result guard: a
// validation that started for an older revision must not paint the border.
func TestSQLValidation_staleResultIsDropped(t *testing.T) {
	// Given — a validation scheduled for "SELECT 1".
	model := readyModel(t)
	model.queryLog.editor.setValue("SELECT 1")
	updated, command := model.Update(model.scheduleSQLValidation()())
	model = updated.(Model)
	validationMsg := command() // the round trip already ran against "SELECT 1"

	// When — the value changes before the result is delivered.
	model.queryLog.editor.setValue("SELECT * FROM missing_table")

	// Then — the stale result is dropped, the border stays pending.
	updated, _ = model.Update(validationMsg)
	model = updated.(Model)
	if model.queryLog.editorValidity != sqlValidityPending {
		t.Fatalf("editor validity = %v, want pending after stale result", model.queryLog.editorValidity)
	}
}
