package app

import (
	"context"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/chrome"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

var _ chat.Executor = chatExecutor{}

// SetAI wires the assistant provider and history store into the chat
// component and enables the pane.
func (m *Model) SetAI(client chat.Client, history chat.History) {
	m.chat.component.SetAI(client, history)
}

func (m *Model) toggleAI() {
	if !m.chat.component.Enabled {
		return
	}
	m.chat.component.Visible = !m.chat.component.Visible
	if !m.chat.component.Visible && m.Focus == focusChat {
		m.Focus = focusWorkspace
		m.focusActiveTable()
	}
	m.applyLayout(m.layout.width, m.layout.height)
}

func (m *Model) resizeChat() {
	width := max(m.layout.chatWidth-6, 1)
	height := max(m.layout.height-4, 1)
	if m.layout.compact {
		width = max(m.layout.width-6, 1)
	}
	m.chat.component.Resize(uikit.Layout{
		Width:         width,
		Height:        max(height-4, 1),
		ViewportWidth: width,
	})
}

// chatModeBadge renders the chat pane's mode badge: the vim-mode
// INSERT/NORMAL indicator and the run state (spinner, YOLO).
func (m Model) chatModeBadge() string {
	left := ""
	if m.vimMode {
		// The modal INSERT/NORMAL state only exists in vim mode.
		if m.chat.component.ChatMode == chat.ModeInsert {
			left = modeInsertStyle.Render("INSERT")
		} else {
			left = modeNormalStyle.Render("NORMAL")
		}
	}
	right := ""
	run := m.chat.component.ActiveRun()
	if run.Loading {
		right = chat.SpinnerFrames[run.SpinnerFrame%len(chat.SpinnerFrames)]
	}
	if m.chat.component.YoloWrites {
		if right != "" {
			right += " "
		}
		right += statusFailedStyle.Render("YOLO")
	}
	if right == "" {
		return left
	}
	return chrome.PaneStatus(left, right, m.chat.component.Viewport.Width())
}

// applyChatSQL puts the assistant's latest SQL statement into the editor.
func (m Model) applyChatSQL() tea.Cmd {
	statement := chat.SQL(m.chat.component.ActiveRun().Messages)
	if statement == "" {
		return nil
	}
	m.queryLog.editor.setValue(statement)
	m.Focus, m.Tab = focusWorkspace, tabQuery
	m.setStatus("AI SQL added to editor")
	m.queryLog.editorValidity = sqlValidityPending
	return m.scheduleSQLValidation()
}

// updateChat routes one chat-owned message or pane key into the
// component and applies its events.
func (m Model) updateChat(message tea.Msg, keys uikit.KeyMatcher) (tea.Model, tea.Cmd) {
	model, event, cmd := m.chat.component.Update(message, uikit.Layout{
		Width:         m.layout.chatWidth,
		Height:        m.layout.height,
		ViewportWidth: m.chat.component.Viewport.Width(),
	}, keys, m.chatLayout())
	m.chat.component = model
	return m.applyChatEvent(event, cmd)
}

// chatExecutor is the root-owned read-only boundary for assistant tool
// queries: the component requests, the root executes against the session
// service.
type chatExecutor struct {
	service sharedsql.Service
}

func (e chatExecutor) ExecuteReadOnly(ctx context.Context, statement string) (sharedsql.Result, error) {
	return e.service.ExecuteReadOnly(ctx, statement)
}

// chatLayout builds the context snapshot root hands to the chat component
// for one update.
func (m Model) chatLayout() chat.Context {
	return chat.Context{
		ConnectionID: m.connectionID,
		Database:     m.databaseInfo,
		Schema:       m.schema.component.Objects,
		Query:        m.queryLog.editor.value,
		Results:      resultsSnapshot(m.queryLog.results),
	}
}

// applyChatEvent applies one chat event: status transitions, clipboard
// copies, editor application, schema refresh, and the write-confirmation
// gate all stay root-owned.
func (m Model) applyChatEvent(event chat.Event, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	switch e := event.(type) {
	case nil:
		return m, cmd
	case chat.StatusChanged:
		m.setStatus(uikit.SafeText(e.Text))
		return m, cmd
	case chat.ClipboardRequested:
		if cmd == nil {
			return m, copyQueryLogStatement(e.Text)
		}
		return m, tea.Batch(cmd, copyQueryLogStatement(e.Text))
	case chat.SQLRequested:
		if e.Source == "editor" {
			// applyChatSQL: put the assistant's statement in the editor.
			m.queryLog.editor.setValue(e.Statement)
			m.Focus, m.Tab = focusWorkspace, tabQuery
			m.setStatus("AI SQL added to editor")
			m.queryLog.editorValidity = sqlValidityPending
			if cmd == nil {
				return m, m.scheduleSQLValidation()
			}
			return m, tea.Batch(cmd, m.scheduleSQLValidation())
		}
		// Interactive execution path for assistant statements: mutations
		// reload the schema; read-only statements never do.
		return m.startQueryStatement(e.Statement, !e.ReadOnly)
	case chat.SchemaRequested:
		if cmd == nil {
			return m, m.loadSchema()
		}
		return m, tea.Batch(cmd, m.loadSchema())
	case chat.ConfirmationRequested:
		if m.chat.component.YoloWrites {
			// YOLO writes skip the dialog: execute directly and reply so
			// the tool round continues.
			statement := e.Statement
			gen := e.Generation
			db := m.Database
			return m, func() tea.Msg {
				res, err := db.Execute(context.Background(), statement)
				content := ""
				errStr := ""
				if err != nil {
					errStr = "executing statement: " + err.Error()
				} else {
					content = chat.FormatResult(res)
				}
				return chat.WriteResultMsg{Gen: gen, Content: content, Err: errStr}
			}
		}
		m.chat.writePending = &chat.WriteRequest{Statement: e.Statement, Generation: e.Generation, Deadline: time.Now().Add(chat.ToolPhaseTimeout)}
		m.chat.writeConfirmation = yesNoConfirmation("Run assistant SQL write?", e.Statement, "run")
		m.overlay.formMode.Mode = formModeConfirm
		return m, nil
	}
	return m, cmd
}

// resultsSnapshot converts the results table into the raw result snapshot
// the chat context carries.
func resultsSnapshot(table table.Model) sharedsql.Result {
	result := sharedsql.Result{Columns: make([]string, len(table.Columns()))}
	for i, column := range table.Columns() {
		result.Columns[i] = column.Title
	}
	rows := table.Rows()
	result.Rows = make([][]*string, len(rows))
	for i, row := range rows {
		cells := make([]*string, len(row))
		for j, cell := range row {
			value := cell
			cells[j] = &value
		}
		result.Rows[i] = cells
	}
	return result
}
