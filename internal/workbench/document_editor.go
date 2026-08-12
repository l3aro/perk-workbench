package workbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// documentEditor is the whole-document editor for document stores (MongoDB
// today). Insert opens with a fresh document ({} for the JSON-aware Mongo
// format, empty for raw formats); edit first loads the full document via
// DocumentReader, then opens the same text form. Save validates JSON for
// the known Mongo format, confirms against the exact document text, then
// calls InsertDocument or ReplaceDocument. Failures keep the editor open
// with the content intact.
type documentEditor struct {
	form         *huh.Form
	confirmation *confirmationDialog
	title        string
	confirming   bool
	saving       bool
	collection   string
	inserting    bool
	capability   sharedsql.DocumentWriteCapability
	identity     *sharedsql.DocumentPayload
	edited       string
	width        int
	scrollOffset int
	loading      bool
}

type documentEditorLoadedMsg struct {
	payload sharedsql.DocumentPayload
	err     error
}

type documentEditorSavedMsg struct {
	statement string
	inserting bool
	startedAt time.Time
	err       error
}

// openInsertDocument opens the document insert editor. The known Mongo
// format starts at {}; any other non-empty textual format opens a raw-text
// editor.
func (m *Model) openInsertDocument() tea.Cmd {
	capability := m.documentCapability()
	if capability == nil {
		m.setStatus(safeText(m.rowWriteUnsupportedError().Error()))
		return nil
	}
	if !capability.Text || capability.Format == "" {
		m.setStatus(fmt.Sprintf("document editing is unsupported for format %s", capability.Format))
		return nil
	}
	initial := ""
	if capability.Format == sharedsql.DocumentFormatMongoExtendedJSON {
		initial = "{}"
	}
	m.documentEditor = newDocumentEditor(m.SelectedTable, true, *capability, nil, initial, max(m.tableViewportWidth, 40))
	return m.openForm(m.documentEditor.form.Init(), m.documentEditor.focus)
}

// openEditDocument opens the document editor for the selected row: the
// full document is loaded asynchronously via DocumentReader, then the same
// text form opens titled "Edit document".
func (m *Model) openEditDocument() tea.Cmd {
	capability := m.documentCapability()
	if capability == nil {
		m.setStatus(safeText(m.rowWriteUnsupportedError().Error()))
		return nil
	}
	if !capability.Text || capability.Format == "" {
		m.setStatus(fmt.Sprintf("document editing is unsupported for format %s", capability.Format))
		return nil
	}
	identity := m.browseDocumentIdentity()
	if identity == nil {
		m.setStatus("document identity unavailable")
		return nil
	}
	reader := m.documentReader()
	if reader == nil {
		m.setStatus(fmt.Sprintf("document editing is unsupported for format %s", capability.Format))
		return nil
	}
	m.documentEditor = &documentEditor{
		collection: m.SelectedTable,
		capability: *capability,
		identity:   identity,
		width:      max(m.tableViewportWidth, 40),
		loading:    true,
	}
	collection := m.SelectedTable
	id := *identity
	return func() tea.Msg {
		payload, err := reader.ReadDocument(m.appContext, collection, id)
		return documentEditorLoadedMsg{payload: payload, err: err}
	}
}

// newDocumentEditor builds an editor over the given initial text.
func newDocumentEditor(collection string, inserting bool, capability sharedsql.DocumentWriteCapability, identity *sharedsql.DocumentPayload, initial string, width int) *documentEditor {
	editor := &documentEditor{
		collection: collection,
		inserting:  inserting,
		capability: capability,
		identity:   identity,
		edited:     initial,
		width:      width,
	}
	editor.buildForm()
	return editor
}

// buildForm constructs the multi-line document text field. Submit is
// Ctrl+S only: Enter inserts a newline, like the cell editor.
func (e *documentEditor) buildForm() {
	e.title = "Edit document"
	if e.inserting {
		e.title = "Insert document"
	}
	field := huh.NewText().Key("document").Title(e.title).Value(&e.edited)
	km := huh.NewDefaultKeyMap()
	km.Text.Submit = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	km.Text.Next = key.NewBinding(key.WithDisabled())
	e.form = newForm(huh.NewGroup(field)).WithShowHelp(true).WithWidth(e.width).WithKeyMap(km)
}

func (e *documentEditor) focus() tea.Cmd {
	if e.form == nil {
		return nil
	}
	return e.form.GetFocusedField().Focus()
}

func (e *documentEditor) blur() {
	if e.form != nil {
		_ = e.form.GetFocusedField().Blur()
	}
}

// beginConfirmation validates the document text — JSON for the known Mongo
// format, nothing for raw formats (the driver validates) — and opens the
// save confirmation carrying the exact document text. A validation failure
// returns an error and leaves the editor open with the content intact.
func (e *documentEditor) beginConfirmation() (tea.Cmd, error) {
	if e.capability.Format == sharedsql.DocumentFormatMongoExtendedJSON && !json.Valid([]byte(e.edited)) {
		return nil, fmt.Errorf("invalid JSON: expected a document")
	}
	e.confirming = true
	title := "Save document changes?"
	if e.inserting {
		title = "Insert document?"
	}
	e.confirmation = yesNoConfirmation(title, e.edited, "confirm")
	return nil, nil
}

// preview renders the structured document-write preview for the query-log
// entry: Table, the document identity (edit), and the document text.
func (e *documentEditor) preview() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Table: %s", e.collection)
	if !e.inserting && e.identity != nil {
		fmt.Fprintf(&builder, "\nKey:\n  _id = %s", string(e.identity.Data))
	}
	if e.inserting {
		builder.WriteString("\nDocument:")
	} else {
		builder.WriteString("\nChanges:")
	}
	builder.WriteString("\n" + e.edited)
	return builder.String()
}

// executeDocumentSave runs the confirmed insert or whole-document replace.
func (m Model) executeDocumentSave() tea.Cmd {
	if m.ReadOnly {
		return func() tea.Msg { return documentEditorSavedMsg{err: fmt.Errorf("connection is read-only")} }
	}
	e := m.documentEditor
	payload := sharedsql.DocumentPayload{Format: e.capability.Format, Data: []byte(e.edited)}
	preview := e.preview()
	collection, startedAt := e.collection, time.Now()
	writer := m.documentWriter()
	if writer == nil {
		return func() tea.Msg { return documentEditorSavedMsg{err: m.rowWriteUnsupportedError()} }
	}
	if e.inserting {
		return func() tea.Msg {
			result, err := writer.InsertDocument(m.appContext, collection, payload)
			if err == nil && result.RowsAffected != 1 {
				err = fmt.Errorf("inserted %d rows, want 1", result.RowsAffected)
			}
			return documentEditorSavedMsg{statement: preview, inserting: true, startedAt: startedAt, err: err}
		}
	}
	identity := *e.identity
	return func() tea.Msg {
		result, err := writer.ReplaceDocument(m.appContext, collection, identity, payload)
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("updated %d rows, want 1", result.RowsAffected)
		}
		return documentEditorSavedMsg{statement: preview, startedAt: startedAt, err: err}
	}
}

// updateDocumentEditorLoaded completes an edit-open: the full document
// arrives, the editor form opens with it. Invalid UTF-8 disables editing.
func (m Model) updateDocumentEditorLoaded(message documentEditorLoadedMsg) (tea.Model, tea.Cmd) {
	editor := m.documentEditor
	if editor == nil {
		return m, nil
	}
	if message.err != nil {
		m.documentEditor = nil
		m.setStatus(safeText(fmt.Sprintf("loading document: %v", message.err)))
		return m, nil
	}
	if !utf8.Valid(message.payload.Data) {
		m.documentEditor = nil
		m.setStatus(fmt.Sprintf("document editing is unsupported for format %s", editor.capability.Format))
		return m, nil
	}
	editor.edited = string(message.payload.Data)
	editor.loading = false
	editor.buildForm()
	return m, m.openForm(editor.form.Init(), editor.focus)
}

// updateDocumentEditorSaved completes a save. Success closes the editor and
// reloads browse; failure keeps the editor open so the rejected text
// survives, and restores the form from its confirming state.
func (m Model) updateDocumentEditorSaved(message documentEditorSavedMsg) (tea.Model, tea.Cmd) {
	if message.statement != "" {
		text := "updated 1 row"
		if message.inserting {
			text = "inserted 1 row"
		}
		m.appendQueryLog(actionLogEntry(message.statement, message.startedAt, message.err, text))
	}
	if message.err != nil {
		if m.documentEditor != nil {
			m.documentEditor.saving = false
			m.documentEditor.confirming = false
			m.documentEditor.confirmation = nil
		}
		m.setStatus(safeText(fmt.Sprintf("saving document: %v", message.err)))
		return m, nil
	}
	m.documentEditor = nil
	m.setStatus("document saved")
	return m, m.loadBrowse()
}

func (e *documentEditor) View() string {
	if e.loading {
		return statusStyle.Render("loading document")
	}
	if e.form == nil {
		return ""
	}
	return e.form.View()
}
