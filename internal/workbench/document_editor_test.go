package workbench

import (
	"context"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// fakeDocumentService is a DocumentReader/DocumentWriter-capable fake used
// to prove the workbench's document editor contract without a live server.
type fakeDocumentService struct {
	sharedsql.Service
	capabilities sharedsql.WriteCapabilities
	loaded       sharedsql.DocumentPayload // returned by ReadDocument
	browse       sharedsql.Result
	inserted     []sharedsql.DocumentPayload
	replaced     []sharedsql.DocumentPayload
	deleted      []sharedsql.DocumentPayload
	readIdentity sharedsql.DocumentPayload
}

func (s *fakeDocumentService) WriteCapabilities() sharedsql.WriteCapabilities { return s.capabilities }

func (s *fakeDocumentService) ReadDocument(context.Context, string, sharedsql.DocumentPayload) (sharedsql.DocumentPayload, error) {
	s.readIdentity = s.loaded
	return s.loaded, nil
}

func (s *fakeDocumentService) InsertDocument(_ context.Context, _ string, doc sharedsql.DocumentPayload) (sharedsql.Result, error) {
	s.inserted = append(s.inserted, doc)
	return sharedsql.Result{RowsAffected: 1}, nil
}

func (s *fakeDocumentService) ReplaceDocument(_ context.Context, _ string, id, doc sharedsql.DocumentPayload) (sharedsql.Result, error) {
	s.replaced = append(s.replaced, doc)
	s.readIdentity = id
	return sharedsql.Result{RowsAffected: 1}, nil
}

func (s *fakeDocumentService) DeleteDocument(_ context.Context, _ string, id sharedsql.DocumentPayload) (sharedsql.Result, error) {
	s.deleted = append(s.deleted, id)
	return sharedsql.Result{RowsAffected: 1}, nil
}

func (s *fakeDocumentService) BrowseTable(context.Context, string, sharedsql.BrowseOptions) (sharedsql.Result, error) {
	return s.browse, nil
}

const testObjectID = "000000000000000000000001"

func mongoIdentity() sharedsql.DocumentPayload {
	return sharedsql.DocumentPayload{
		Format: sharedsql.DocumentFormatMongoExtendedJSON,
		Data:   []byte(`{"$oid":"` + testObjectID + `"}`),
	}
}

func readyDocumentModel(t *testing.T, capabilities sharedsql.WriteCapabilities) (Model, *fakeDocumentService) {
	t.Helper()
	model := readyModel(t)
	service := &fakeDocumentService{capabilities: capabilities}
	model.Database = service
	model.databaseInfo = sharedsql.DatabaseInfo{Product: "MongoDB"}
	model.SelectedTable, model.Tab, model.Focus = "things", tabBrowse, focusWorkspace
	model.structure.columns = []sharedsql.ColumnInfo{
		{Name: "_id", Type: "objectId", PrimaryKey: 1},
		{Name: "name", Type: "string", Nullable: true},
	}
	model.browse.component.Result = sharedsql.Result{
		Columns: []string{"_id", "name"},
		Rows: [][]*string{
			{stringPointer(`ObjectId("` + testObjectID + `")`), stringPointer("first")},
		},
		DocumentIDs: []sharedsql.DocumentPayload{mongoIdentity()},
	}
	model.browse.component.Table.SetColumns(tableColumns([]string{"_id", "name"}, []table.Row{{"1", "first"}}))
	model.browse.component.Table.SetRows([]table.Row{{"1", "first"}})
	model.browse.component.Table.SetCursor(0)
	return model, service
}

func openInsertDocument(t *testing.T, model Model) Model {
	t.Helper()
	return updateBrowseForm(model, tea.KeyPressMsg{Code: 'a', Text: "a"})
}

func openEditDocument(t *testing.T, model Model) Model {
	t.Helper()
	return updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// TestDocumentEditor_insertStartsWithObjectForMongoFormat proves the known

// TestDocumentEditor_insertRejectsInvalidJSONBeforeConfirmation proves the
// JSON-aware editor validates before the confirmation opens and keeps the

// TestDocumentEditor_insertConfirmsAndSaves proves the confirm flow: the
// confirmation carries the exact text, and the save calls InsertDocument
// with the same payload, then closes and reloads browse.
func TestDocumentEditor_insertConfirmsAndSaves(t *testing.T) {
	model, service := readyDocumentModel(t, sharedsql.WriteCapabilities{
		Document: &sharedsql.DocumentWriteCapability{Format: sharedsql.DocumentFormatMongoExtendedJSON, Text: true},
	})
	model = openInsertDocument(t, model)
	model.browse.component.DocumentEditor.Edited = `{"name": "widget"}`

	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	if !model.browse.component.DocumentEditor.Confirming || model.browse.component.DocumentEditor.Confirmation == nil {
		t.Fatal("save did not open the confirmation")
	}
	if got := model.browse.component.DocumentEditor.Confirmation.Content(80); !strings.Contains(got, `{"name": "widget"}`) {
		t.Fatalf("confirmation content = %q, want exact document text", got)
	}

	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	if model.browse.component.DocumentEditor != nil {
		t.Fatal("editor remained open after successful insert")
	}
	if len(service.inserted) != 1 {
		t.Fatalf("InsertDocument calls = %d, want 1", len(service.inserted))
	}
	if got, want := string(service.inserted[0].Data), `{"name": "widget"}`; got != want {
		t.Fatalf("inserted data = %q, want %q", got, want)
	}
	if got, want := service.inserted[0].Format, sharedsql.DocumentFormatMongoExtendedJSON; got != want {
		t.Fatalf("inserted format = %q, want %q", got, want)
	}
	var insertEntry *queryLogEntry
	for index := range min(len(model.queryLog.component.Entries), 3) {
		if model.queryLog.component.Entries[index].Message == "inserted 1 row" {
			insertEntry = &model.queryLog.component.Entries[index]
		}
	}
	if insertEntry == nil {
		t.Fatalf("query log = %#v, want inserted 1 row entry", model.queryLog.component.Entries)
	}
	if got, want := insertEntry.Statement, "Table: things\nDocument:\n{\"name\": \"widget\"}"; got != want {
		t.Fatalf("query log statement = %q, want preview %q", got, want)
	}
}

// TestDocumentEditor_unknownFormatPassesExactBytes proves a raw textual

// TestDocumentEditor_editLoadsFullPayloadBeforeOpening proves edit opens
// only after ReadDocument resolves, with the full document (not the
// sampled grid cell), and that replace uses the row identity.
func TestDocumentEditor_editLoadsFullPayloadBeforeOpening(t *testing.T) {
	model, service := readyDocumentModel(t, sharedsql.WriteCapabilities{
		Document: &sharedsql.DocumentWriteCapability{Format: sharedsql.DocumentFormatMongoExtendedJSON, Text: true},
	})
	full := `{"_id": {"$oid":"` + testObjectID + `"}, "name": "first", "nested": {"a": [1, 2]}}`
	service.loaded = sharedsql.DocumentPayload{Format: sharedsql.DocumentFormatMongoExtendedJSON, Data: []byte(full)}

	model = openEditDocument(t, model)

	if model.browse.component.DocumentEditor == nil || !model.browse.component.DocumentEditor.Loading {
		t.Fatalf("editor = %#v, want loading editor before payload arrives", model.browse.component.DocumentEditor)
	}
	if got, want := model.browse.component.DocumentEditor.View(), "loading document"; !strings.Contains(got, want) {
		t.Fatalf("view = %q, want %q while loading", got, want)
	}

	updated, _ := model.Update(documentEditorLoadedMsg{payload: service.loaded})
	model = updated.(Model)

	if model.browse.component.DocumentEditor.Loading || model.browse.component.DocumentEditor.Form == nil {
		t.Fatal("editor did not open after the payload arrived")
	}
	if got, want := model.browse.component.DocumentEditor.Edited, full; got != want {
		t.Fatalf("edited = %q, want full loaded document %q", got, want)
	}
	if got, want := model.browse.component.DocumentEditor.Title, "Edit document"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}

	// Save the edited document: the replace must target the row identity.
	model.browse.component.DocumentEditor.Edited = `{"name": "edited"}`
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = resolveBrowseCommand(model, tea.KeyPressMsg{Code: 'y', Text: "y"})

	if len(service.replaced) != 1 {
		t.Fatalf("ReplaceDocument calls = %d, want 1", len(service.replaced))
	}
	if got, want := string(service.replaced[0].Data), `{"name": "edited"}`; got != want {
		t.Fatalf("replaced data = %q, want %q", got, want)
	}
	if got, want := string(service.readIdentity.Data), `{"$oid":"`+testObjectID+`"}`; got != want {
		t.Fatalf("replace identity = %q, want the row's extended-JSON _id, not the display cell", got)
	}
}

// TestDocumentEditor_editLoadFailureClosesEditor proves a failed load
// reports the error and leaves no editor behind.
func TestDocumentEditor_editLoadFailureClosesEditor(t *testing.T) {
	model, _ := readyDocumentModel(t, sharedsql.WriteCapabilities{
		Document: &sharedsql.DocumentWriteCapability{Format: sharedsql.DocumentFormatMongoExtendedJSON, Text: true},
	})
	model = openEditDocument(t, model)

	updated, _ := model.Update(documentEditorLoadedMsg{err: context.DeadlineExceeded})
	model = updated.(Model)

	if model.browse.component.DocumentEditor != nil {
		t.Fatal("editor remained open after load failure")
	}
	if !strings.Contains(model.Status, "loading document") {
		t.Fatalf("status = %q, want load failure", model.Status)
	}
}

// TestDocumentEditor_editIdentityUnavailable proves edit without a row
// identity reports the exact status.
func TestDocumentEditor_editIdentityUnavailable(t *testing.T) {
	model, _ := readyDocumentModel(t, sharedsql.WriteCapabilities{
		Document: &sharedsql.DocumentWriteCapability{Format: sharedsql.DocumentFormatMongoExtendedJSON, Text: true},
	})
	model.browse.component.Result.DocumentIDs = nil

	model = openEditDocument(t, model)

	if model.browse.component.DocumentEditor != nil {
		t.Fatal("editor opened without an identity")
	}
	if !strings.Contains(model.Status, "document identity unavailable") {
		t.Fatalf("status = %q, want identity-unavailable", model.Status)
	}
}

// TestDocumentEditor_nonTextHidesEditingButDeletes proves Text: false
// disables insert/edit while delete by identity still works, and the
// context menu offers delete only.
func TestDocumentEditor_nonTextHidesEditingButDeletes(t *testing.T) {
	model, service := readyDocumentModel(t, sharedsql.WriteCapabilities{
		Document: &sharedsql.DocumentWriteCapability{Format: sharedsql.DocumentFormatMongoExtendedJSON, Text: false},
	})

	model = openInsertDocument(t, model)
	if model.browse.component.DocumentEditor != nil {
		t.Fatal("insert editor opened on a non-text capability")
	}
	if !strings.Contains(model.Status, "document editing is unsupported for format") {
		t.Fatalf("status = %q, want editing-unsupported", model.Status)
	}

	model = openEditDocument(t, model)
	if model.browse.component.DocumentEditor != nil {
		t.Fatal("edit editor opened on a non-text capability")
	}

	options := model.browseRowMenuOptions()
	if len(options) != 1 || options[0].label != "Delete document" || options[0].keys != "d" {
		t.Fatalf("menu options = %#v, want delete document only", options)
	}

	updated, _ := model.Update(model.deleteRow()())
	model = updated.(Model)
	if len(service.deleted) != 1 {
		t.Fatalf("DeleteDocument calls = %d, want 1", len(service.deleted))
	}
	if got, want := string(service.deleted[0].Data), `{"$oid":"`+testObjectID+`"}`; got != want {
		t.Fatalf("deleted identity = %q, want the row's extended-JSON _id", got)
	}
	if !strings.Contains(model.Status, "row deleted") {
		t.Fatalf("status = %q, want row deleted", model.Status)
	}
}

// TestDocumentEditor_rowMenuLabelsAndPalette prove the document labels
// reach the context menu and the command palette.
func TestDocumentEditor_rowMenuLabelsAndPalette(t *testing.T) {
	model, _ := readyDocumentModel(t, sharedsql.WriteCapabilities{
		Document: &sharedsql.DocumentWriteCapability{Format: sharedsql.DocumentFormatMongoExtendedJSON, Text: true},
	})
	options := model.browseRowMenuOptions()
	want := []menuOption{
		{label: "Insert document", action: "insert_row", keys: "a"},
		{label: "Copy cell", action: "copy_cell", keys: "y"},
		{label: "Edit document", action: "edit_row", keys: "enter"},
		{label: "Delete document", action: "delete_row", keys: "d"},
	}
	if len(options) != len(want) {
		t.Fatalf("menu options = %#v, want %#v", options, want)
	}
	for index := range want {
		if options[index] != want[index] {
			t.Fatalf("menu option %d = %#v, want %#v", index, options[index], want[index])
		}
	}

	palette := newCommandPalette(model)
	labels := map[string]bool{}
	for _, item := range palette.items {
		labels[item.label] = true
	}
	for _, label := range []string{"insert document", "edit document"} {
		if !labels[label] {
			t.Fatalf("palette labels = %#v, want %q", labels, label)
		}
	}
	// The SQL-only cell edit entry merges into edit document.
	if labels["edit cell"] {
		t.Fatal("palette offers edit cell on a document store")
	}
}

// TestDocumentEditor_saveFailureKeepsEditor proves a rejected save leaves
// the editor open with the content intact.
func TestDocumentEditor_saveFailureKeepsEditor(t *testing.T) {
	model, service := readyDocumentModel(t, sharedsql.WriteCapabilities{
		Document: &sharedsql.DocumentWriteCapability{Format: sharedsql.DocumentFormatMongoExtendedJSON, Text: true},
	})
	model = openInsertDocument(t, model)
	model.browse.component.DocumentEditor.Edited = `{"name": "widget"}`
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyF5})
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: 'y', Text: "y"})
	// The save command is dispatched; intercept by delivering a failed
	// completion instead of driving it.
	updated, _ := model.Update(documentEditorSavedMsg{err: context.DeadlineExceeded})
	model = updated.(Model)

	if model.browse.component.DocumentEditor == nil || model.browse.component.DocumentEditor.Confirming {
		t.Fatalf("editor = %#v, want retained non-confirming editor after save failure", model.browse.component.DocumentEditor)
	}
	if got, want := model.browse.component.DocumentEditor.Edited, `{"name": "widget"}`; got != want {
		t.Fatalf("editor content = %q, want retained %q", got, want)
	}
	if len(service.inserted) != 0 {
		t.Fatal("failed save reached the driver")
	}
}
