package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk/internal/sqlite"
)

const compactWidth = 90
const browsePageSize = 25

type modelState int

const (
	stateConnection modelState = iota
	statePicking
	stateOpening
	stateReady
	stateFailure
)

type focus int

const (
	focusSchema focus = iota
	focusWorkspace
)

type workspaceTab int

const (
	tabStructure workspaceTab = iota
	tabBrowse
	tabSQL
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
	schema, picker, recent                  list.Model
	structure, browse, results              table.Model
	editor                                  editor
	connection                              connectionForm
	recentConnections                       []recentConnection
	recentPath                              string
	focus                                   focus
	tab                                     workspaceTab
	selectedTable                           string
	browsePage                              int
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
		schema:     newList("Schema", true),
		picker:     newList("Choose database", true),
		recent:     newList("Recent connections", true),
		structure:  newResultsTable(),
		browse:     newResultsTable(),
		results:    newResultsTable(),
		editor:     editor,
		connection: newConnectionForm(),
		focus:      focusWorkspace,
		tab:        tabSQL,
	}
	if target == "" {
		model.state = stateConnection
		model.recentPath, _ = recentConnectionsPath()
		model.recentConnections = loadRecentConnections(model.recentPath)
		_ = model.recent.SetItems(recentListItems(model.recentConnections))
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
				if dsn, ok := strings.CutPrefix(target, "mysql:"); ok {
					service, err := sqlite.OpenMySQL(ctx, dsn)
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
					return databaseOpenedMsg{target: dsn, service: service, objects: objects}
				}
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
	return nil
}

func newConnectionForm() connectionForm {
	name := textinput.New()
	name.Prompt = "Name: "
	name.Placeholder = "Local database"
	name.Focus()

	target := textinput.New()
	target.Prompt = "Target: "
	target.Placeholder = "path/to/database.db or :memory:"

	host := textinput.New()
	host.Prompt = "Host: "
	host.Placeholder = "localhost"

	port := textinput.New()
	port.Prompt = "Port: "
	port.SetValue("3306")

	user := textinput.New()
	user.Prompt = "Username: "

	pass := textinput.New()
	pass.Prompt = "Password: "
	pass.EchoMode = textinput.EchoPassword
	pass.EchoCharacter = '*'

	return connectionForm{name: name, target: target, host: host, port: port, user: user, pass: pass, focus: connectionFocusName}
}
