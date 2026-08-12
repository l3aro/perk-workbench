package browse

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// DocumentEditor is the whole-document editor for document stores (MongoDB
// today). Insert opens with a fresh document ({} for the JSON-aware Mongo
// format, empty for raw formats); edit first loads the full document via
// DocumentReader, then opens the same text form. Save validates JSON for
// the known Mongo format, confirms against the exact document text, then
// calls InsertDocument or ReplaceDocument. Failures keep the editor open
// with the content intact. The root owns the load/save execution; the
// component owns the editor construction, validation, and rendering.
type DocumentEditor struct {
	Form         *huh.Form
	Confirmation *uikit.ConfirmationDialog
	Title        string
	Confirming   bool
	Saving       bool
	Collection   string
	Inserting    bool
	Capability   sharedsql.DocumentWriteCapability
	Identity     *sharedsql.DocumentPayload
	Edited       string
	Width        int
	ScrollOffset int
	Loading      bool
}

// NewDocumentEditor builds an editor over the given initial text.
func NewDocumentEditor(collection string, inserting bool, capability sharedsql.DocumentWriteCapability, identity *sharedsql.DocumentPayload, initial string, width int) *DocumentEditor {
	editor := &DocumentEditor{
		Collection: collection,
		Inserting:  inserting,
		Capability: capability,
		Identity:   identity,
		Edited:     initial,
		Width:      width,
	}
	editor.BuildForm()
	return editor
}

// BuildForm constructs the multi-line document text field. Submit is
// Ctrl+S only: Enter inserts a newline, like the cell editor.
func (e *DocumentEditor) BuildForm() {
	e.Title = "Edit document"
	if e.Inserting {
		e.Title = "Insert document"
	}
	field := huh.NewText().Key("document").Title(e.Title).Value(&e.Edited)
	km := huh.NewDefaultKeyMap()
	km.Text.Submit = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	km.Text.Next = key.NewBinding(key.WithDisabled())
	e.Form = uikit.NewForm(huh.NewGroup(field)).WithShowHelp(true).WithWidth(e.Width).WithKeyMap(km)
}

// Focus focuses the document field.
func (e *DocumentEditor) Focus() tea.Cmd {
	if e.Form == nil {
		return nil
	}
	return e.Form.GetFocusedField().Focus()
}

// Blur blurs the document field.
func (e *DocumentEditor) Blur() {
	if e.Form != nil {
		_ = e.Form.GetFocusedField().Blur()
	}
}

// BeginConfirmation validates the document text — JSON for the known Mongo
// format, nothing for raw formats (the driver validates) — and opens the
// save confirmation carrying the exact document text. A validation failure
// returns an error and leaves the editor open with the content intact.
func (e *DocumentEditor) BeginConfirmation() (tea.Cmd, error) {
	if e.Capability.Format == sharedsql.DocumentFormatMongoExtendedJSON && !json.Valid([]byte(e.Edited)) {
		return nil, fmt.Errorf("invalid JSON: expected a document")
	}
	e.Confirming = true
	title := "Save document changes?"
	if e.Inserting {
		title = "Insert document?"
	}
	e.Confirmation = uikit.YesNoConfirmation(title, e.Edited, "confirm")
	return nil, nil
}

// Preview renders the structured document-write preview for the query-log
// entry: Table, the document identity (edit), and the document text.
func (e *DocumentEditor) Preview() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Table: %s", e.Collection)
	if !e.Inserting && e.Identity != nil {
		fmt.Fprintf(&builder, "\nKey:\n  _id = %s", string(e.Identity.Data))
	}
	if e.Inserting {
		builder.WriteString("\nDocument:")
	} else {
		builder.WriteString("\nChanges:")
	}
	builder.WriteString("\n" + e.Edited)
	return builder.String()
}

// View renders the editor body: the loading line, or the document form.
func (e *DocumentEditor) View() string {
	if e.Loading {
		return uikit.StatusStyle.Render("loading document")
	}
	if e.Form == nil {
		return ""
	}
	return e.Form.View()
}
