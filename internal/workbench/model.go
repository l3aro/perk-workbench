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
	"github.com/l3aro/perk/internal/core"
	"github.com/l3aro/perk/internal/mysql"
	sharedsql "github.com/l3aro/perk/internal/sql"
	"github.com/l3aro/perk/internal/sqlite"
)

const compactWidth = 90
const browsePageSize = core.BrowsePageSize

type modelState = core.State
type focus = core.Focus
type workspaceTab = core.Tab

const (
	stateConnection = core.StateConnection
	statePicking    = core.StatePicking
	stateOpening    = core.StateOpening
	stateReady      = core.StateReady
	stateFailure    = core.StateFailure

	focusSchema    = core.FocusSchema
	focusWorkspace = core.FocusWorkspace

	tabStructure = core.TabStructure
	tabBrowse    = core.TabBrowse
	tabSQL       = core.TabSQL
)

type Model struct {
	core.Workflow
	pickerDir                                       string
	appContext                                      context.Context
	openTarget                                      func(string) tea.Cmd
	running, cancelRequested, pendingQuit           bool
	requestID, activeRequestID                      uint64
	cancel                                          context.CancelFunc
	schema, picker, recent                          list.Model
	structure, browse, results                      table.Model
	structureColumns                                []sharedsql.ColumnInfo
	browseNumericColumns, resultsNumericColumns     []bool
	databaseInfo                                    sharedsql.DatabaseInfo
	browseResult                                    sharedsql.Result
	editor                                          editor
	columnForm                                      columnForm
	browseForm                                      browseForm
	connection                                      connectionForm
	recentConnections                               []recentConnection
	recentPath                                      string
	width, height, schemaWidth, editorWidth         int
	editorHeight, resultsHeight, tableViewportWidth int
	structureOffset, browseOffset, resultsOffset    int
	compact                                         bool
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
	service sharedsql.Service
	info    sharedsql.DatabaseInfo
	objects []sharedsql.SchemaObject
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
		Workflow:   core.New(target),
		appContext: opener.ctx,
		openTarget: opener.command,
		schema:     newSchemaList(),
		picker:     newList("Choose database", true),
		recent:     newList("Recent connections", true),
		structure:  newResultsTable(),
		browse:     newResultsTable(),
		results:    newResultsTable(),
		editor:     editor,
		connection: newConnectionForm(),
	}
	model.focusActiveTable()
	if target == "" {
		model.recentPath, _ = recentConnectionsPath()
		model.recentConnections = loadRecentConnections(model.recentPath)
		_ = model.recent.SetItems(recentListItems(model.recentConnections))
	}
	return model
}

func Open(ctx context.Context) databaseOpener {
	return databaseOpener{
		ctx: ctx,
		command: func(target string) tea.Cmd {
			return func() tea.Msg {
				if dsn, ok := strings.CutPrefix(target, "mysql:"); ok {
					service, err := mysql.Open(ctx, dsn)
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
					return databaseOpenedMsg{target: dsn, service: service, info: service.Info(), objects: objects}
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
				return databaseOpenedMsg{target: resolved, service: service, info: service.Info(), objects: objects}
			}
		},
	}
}

func (m Model) Service() sharedsql.Service { return m.Database }

func (m Model) Init() tea.Cmd {
	if m.State == core.StateOpening {
		return m.openTarget(m.Target)
	}
	return nil
}

func newConnectionForm() connectionForm {
	name := textinput.New()
	setConnectionPrompt(&name, "Name")
	name.Placeholder = "Local database"
	name.Focus()

	target := textinput.New()
	setConnectionPrompt(&target, "Target")
	target.Placeholder = "path/to/database.db or :memory:"

	host := textinput.New()
	setConnectionPrompt(&host, "Host")
	host.Placeholder = "localhost"

	port := textinput.New()
	setConnectionPrompt(&port, "Port")
	port.SetValue("3306")

	user := textinput.New()
	setConnectionPrompt(&user, "Username")

	pass := textinput.New()
	setConnectionPrompt(&pass, "Password")
	pass.EchoMode = textinput.EchoPassword
	pass.EchoCharacter = '*'

	form := connectionForm{name: name, target: target, host: host, port: port, user: user, pass: pass}
	form.setFocus(connectionFocusRecent)
	return form
}
