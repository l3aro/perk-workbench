package browse

import (
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// The browse feature owns its modal overlays — the cell editor, the
// document editor, the cell viewer, the row/edit and filter forms — and
// drives their state transitions here. The root routes messages into these
// methods while an overlay is open and applies the returned outcome: save
// execution and status reporting stay root-owned, geometry and state live
// in the component.

// Resize propagates a window resize into the open cell-viewer overlay.
func (m *Model) Resize(width, height int) {
	if m.CellViewer != nil {
		m.CellViewer.Resize(max(width-8, 1), max(height-10, 1))
	}
}

// SetPage mirrors the root's global browse page into the component's
// rendering mirror (the pager button enablement).
func (m *Model) SetPage(page int) {
	m.Page = page
}

// UpdateCellViewer drives the open cell-viewer overlay: Escape closes it,
// every other message passes through to the viewer.
func (m Model) UpdateCellViewer(msg tea.Msg) (Model, tea.Cmd) {
	if keyPress, ok := msg.(tea.KeyPressMsg); ok && keyPress.Key().Code == tea.KeyEscape {
		m.CellViewer = nil
		return m, nil
	}
	cmd := m.CellViewer.Update(msg)
	return m, cmd
}

// CellEditorResult is one root-applied outcome of a cell-editor update.
type CellEditorResult struct {
	// Close marks the editor closed; the root has nothing else to do.
	Close bool
	// CancelConfirmation exits the confirmation back to the input; the
	// root resets the form mode.
	CancelConfirmation bool
	// Save runs the confirmed save; the root executes the row update.
	Save bool
	// ButtonHit marks a dialog-button click; the root swallows the
	// trailing mouse release.
	ButtonHit bool
}

// UpdateCellEditor drives the open cell editor: Escape (cancel the
// confirmation, then close), the confirmation dialog, the Save/Cancel
// dialog buttons, the form.save key, and the huh field. The root applies
// the returned outcome and executes the save flow.
func (m Model) UpdateCellEditor(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher) (Model, CellEditorResult, tea.Cmd) {
	editor := m.CellEditor
	if editor == nil {
		return m, CellEditorResult{}, nil
	}
	keyPress, isKeyPress := msg.(tea.KeyPressMsg)
	if isKeyPress && keyPress.Key().Code == tea.KeyEscape {
		if editor.Confirming {
			editor.Confirming = false
			editor.Confirm = nil
			return m, CellEditorResult{CancelConfirmation: true}, editor.Input.Init()
		}
		m.CellEditor = nil
		return m, CellEditorResult{Close: true}, nil
	}
	if editor.Confirming {
		completed, action := editor.Confirm.Update(msg, layout.Width, layout.Height)
		if !completed {
			return m, CellEditorResult{}, nil
		}
		if action != "save" {
			m.CellEditor = nil
			return m, CellEditorResult{Close: true}, nil
		}
		return m, CellEditorResult{Save: true}, nil
	}
	if mouse, ok := msg.(tea.MouseClickMsg); ok && mouse.Button == tea.MouseLeft {
		switch editor.ButtonAt(mouse.X, mouse.Y, layout) {
		case "save":
			return m, CellEditorResult{ButtonHit: true}, editor.BeginConfirmation()
		case "cancel":
			m.CellEditor = nil
			return m, CellEditorResult{Close: true, ButtonHit: true}, nil
		}
	}
	// Only Ctrl+S (form.save) submits the cell editor; Enter neither
	// submits nor advances. Without this guard, a single-field form
	// transitions to StateCompleted on Enter.
	if isKeyPress && keyPress.Key().Code == tea.KeyEnter {
		return m, CellEditorResult{}, nil
	}
	if isKeyPress && keys.Match(keyPress, "form.save", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}) {
		return m, CellEditorResult{}, editor.BeginConfirmation()
	}
	model, command := editor.Input.Update(msg)
	editor.Input = model.(*huh.Form)
	if editor.Input.State != huh.StateCompleted {
		return m, CellEditorResult{}, command
	}
	return m, CellEditorResult{}, editor.BeginConfirmation()
}

// DocumentEditorResult is one root-applied outcome of a document-editor
// update.
type DocumentEditorResult struct {
	Close              bool
	CancelConfirmation bool
	// Save runs the confirmed save; the root executes the insert or
	// whole-document replace.
	Save bool
	// Status carries a validation-failure message the root reports; the
	// editor stays open with the content intact.
	Status string
}

// UpdateDocumentEditor drives the open document editor: Escape (cancel the
// confirmation, then close), the confirmation dialog, the form.save key,
// and the huh field. Input is ignored while the editor loads or saves —
// those transitions arrive via the load/save messages the root routes
// before this branch. The root applies the returned outcome and executes
// the save flow.
func (m Model) UpdateDocumentEditor(msg tea.Msg, layout uikit.Layout, keys uikit.KeyMatcher) (Model, DocumentEditorResult, tea.Cmd) {
	editor := m.DocumentEditor
	if editor == nil {
		return m, DocumentEditorResult{}, nil
	}
	if editor.Saving || editor.Loading {
		return m, DocumentEditorResult{}, nil
	}
	keyPress, isKeyPress := msg.(tea.KeyPressMsg)
	if isKeyPress && keyPress.Key().Code == tea.KeyEscape {
		if editor.Confirming {
			editor.Confirming = false
			editor.Confirmation = nil
			return m, DocumentEditorResult{CancelConfirmation: true}, editor.Form.Init()
		}
		m.DocumentEditor = nil
		return m, DocumentEditorResult{Close: true}, nil
	}
	if editor.Confirming {
		completed, action := editor.Confirmation.Update(msg, layout.Width, layout.Height)
		if !completed {
			return m, DocumentEditorResult{}, nil
		}
		if action != "confirm" {
			m.DocumentEditor = nil
			return m, DocumentEditorResult{Close: true}, nil
		}
		editor.Confirming = false
		editor.Saving = true
		return m, DocumentEditorResult{Save: true}, nil
	}
	// Only Ctrl+S (form.save) submits the document editor; Enter inserts
	// a newline.
	if isKeyPress && keyPress.Key().Code == tea.KeyEnter {
		return m, DocumentEditorResult{}, nil
	}
	if isKeyPress && keys.Match(keyPress, "form.save", []uikit.Scope{uikit.ScopeForm, uikit.ScopeView, uikit.ScopeGlobal}) {
		cmd, err := editor.BeginConfirmation()
		if err != nil {
			return m, DocumentEditorResult{Status: uikit.SafeText(err.Error())}, nil
		}
		return m, DocumentEditorResult{}, cmd
	}
	model, command := editor.Form.Update(msg)
	editor.Form = model.(*huh.Form)
	if editor.Form.State != huh.StateCompleted {
		return m, DocumentEditorResult{}, command
	}
	cmd, err := editor.BeginConfirmation()
	if err != nil {
		return m, DocumentEditorResult{Status: uikit.SafeText(err.Error())}, nil
	}
	return m, DocumentEditorResult{}, cmd
}

// DocumentLoadOutcome is the result of completing an edit-open: the editor
// either opened with the full document (Opened) or closed (Closed, a load
// failure or invalid UTF-8).
type DocumentLoadOutcome struct {
	Opened bool
	Closed bool
	Err    error
	// Format is the editor's capability format, reported when the loaded
	// document is not valid UTF-8.
	Format sharedsql.DocumentFormat
}

// ApplyDocumentLoaded completes an edit-open: the full document arrives
// and the editor opens with it; a load error or invalid UTF-8 closes the
// editor. The returned outcome tells the root what to report.
func (m Model) ApplyDocumentLoaded(payload sharedsql.DocumentPayload, err error) (Model, DocumentLoadOutcome, tea.Cmd) {
	editor := m.DocumentEditor
	if editor == nil {
		return m, DocumentLoadOutcome{}, nil
	}
	if err != nil {
		m.DocumentEditor = nil
		return m, DocumentLoadOutcome{Closed: true, Err: err}, nil
	}
	if !utf8.Valid(payload.Data) {
		format := editor.Capability.Format
		m.DocumentEditor = nil
		return m, DocumentLoadOutcome{Closed: true, Format: format}, nil
	}
	editor.Edited = string(payload.Data)
	editor.Loading = false
	editor.BuildForm()
	return m, DocumentLoadOutcome{Opened: true}, editor.Form.Init()
}

// DocumentSaveFailed restores the editor from its confirming state after a
// rejected save so the rejected text survives.
func (m *Model) DocumentSaveFailed() {
	if m.DocumentEditor != nil {
		m.DocumentEditor.Saving = false
		m.DocumentEditor.Confirming = false
		m.DocumentEditor.Confirmation = nil
	}
}

// CloseDocumentEditor closes the document editor.
func (m *Model) CloseDocumentEditor() {
	m.DocumentEditor = nil
}

// CloseCellEditor closes the cell editor.
func (m *Model) CloseCellEditor() {
	m.CellEditor = nil
}

// FilterFormResult is one root-applied outcome of a filter-form update:
// the form's action plus the derived settings (on apply) or the apply
// error (the root reports it as a status).
type FilterFormResult struct {
	Action   FilterAction
	Settings Settings
	Err      error
}

// UpdateFilterForm drives the open browse filter form: key routing, the
// form-mode sync, and close-on-discard. An applied form carries the
// derived settings; the root reloads the browse page from the top.
func (m Model) UpdateFilterForm(msg tea.Msg, keys uikit.KeyMatcher, formMode *uikit.FormModeController) (Model, FilterFormResult, tea.Cmd) {
	command, action := m.FilterForm.Update(msg, keys)
	if m.FilterForm.Editing {
		formMode.Mode = uikit.FormModeInsert
	} else {
		formMode.Mode = uikit.FormModeNormal
	}
	switch action {
	case FilterDiscard:
		m.FilterForm = nil
	case FilterApply:
		settings, err := m.FilterForm.Apply()
		if err != nil {
			return m, FilterFormResult{Action: action, Err: err}, command
		}
		m.Settings = settings
		m.FilterForm = nil
		m.PageTag++
		m.Page = 0
		return m, FilterFormResult{Action: action, Settings: settings}, command
	}
	return m, FilterFormResult{Action: action}, command
}

// UpdateForm drives the open row/edit form: key routing, the saving flag,
// and close-on-discard. The returned action tells the root which write
// flow to execute.
func (m Model) UpdateForm(msg tea.Msg, layout uikit.Layout, formMode *uikit.FormModeController) (Model, FormAction, tea.Cmd) {
	m.Form.Height = layout.Height
	command, action := m.Form.Update(msg, formMode)
	switch action {
	case FormSave:
		m.Form.Saving = true
	case FormDiscard:
		m.Form = Form{}
	}
	return m, action, command
}
