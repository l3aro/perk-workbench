package workbench

import (
	"context"
	"errors"
	"fmt"

	"bubble-workbench/internal/sqlite"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

const compactWidth = 90

type modelState int

const (
	statePicking modelState = iota
	stateOpening
	stateReady
	stateFailure
)

type focus int

const (
	focusSchema focus = iota
	focusEditor
	focusResults
)

type Model struct {
	state                                   modelState
	target, status, pickerDir               string
	service                                 *sqlite.Service
	appContext                              context.Context
	queryContext                            context.Context
	openTarget                              func(string) tea.Cmd
	running, cancelRequested, pendingQuit   bool
	requestID, activeRequestID              uint64
	cancel                                  context.CancelFunc
	schema, picker                          list.Model
	results                                 table.Model
	editor                                  editor
	focus                                   focus
	width, height, schemaWidth, editorWidth int
	editorHeight, resultsHeight             int
	compact                                 bool
}

type pickerItem struct{ raw, title, description string }

func (i pickerItem) FilterValue() string { return i.title }
func (i pickerItem) Title() string       { return i.title }
func (i pickerItem) Description() string { return i.description }

type schemaItem struct{ title, description string }

func (i schemaItem) FilterValue() string { return i.title }
func (i schemaItem) Title() string       { return i.title }
func (i schemaItem) Description() string { return i.description }

type databaseOpenedMsg struct {
	target  string
	service *sqlite.Service
	objects []sqlite.SchemaObject
	err     error
}

type directoryReadMsg struct {
	dir   string
	items []pickerItem
	err   error
}

type pickerSelectionMsg struct {
	target string
	dir    bool
	err    error
}

type databaseOpener struct {
	ctx     context.Context
	command func(string) tea.Cmd
}

func New(target string, opener databaseOpener) Model {
	editor := newEditor()
	editor.textarea.SetVirtualCursor(false)
	model := Model{
		target:     target,
		appContext: opener.ctx,
		openTarget: opener.command,
		schema:     newList("Schema", false),
		picker:     newList("Choose database", true),
		results:    newResultsTable(),
		editor:     editor,
		focus:      focusEditor,
	}
	if target == "" {
		model.state = statePicking
	} else {
		model.state = stateOpening
	}
	return model
}

func Open(ctx context.Context) databaseOpener {
	return databaseOpener{
		ctx: ctx,
		command: func(target string) tea.Cmd {
			return func() tea.Msg {
				resolved, err := resolveTarget(target)
				if err != nil {
					return databaseOpenedMsg{err: err}
				}
				service, err := sqlite.Open(ctx, resolved)
				if err != nil {
					return databaseOpenedMsg{err: fmt.Errorf("opening database: %w", err)}
				}
				objects, err := service.ListSchema(ctx)
				if err != nil {
					if closeErr := service.Close(); closeErr != nil {
						return databaseOpenedMsg{err: fmt.Errorf("listing schema: %w", errors.Join(err, closeErr))}
					}
					return databaseOpenedMsg{err: fmt.Errorf("listing schema: %w", err)}
				}
				return databaseOpenedMsg{target: resolved, service: service, objects: objects}
			}
		},
	}
}

func (m Model) Service() *sqlite.Service { return m.service }

func (m Model) Init() tea.Cmd {
	if m.state == stateOpening {
		return m.openTarget(m.target)
	}
	return readDirectory(".")
}
