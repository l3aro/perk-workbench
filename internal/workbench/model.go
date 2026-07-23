package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
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
	focusQueryLog  = core.FocusQueryLog

	tabStructure   = core.TabStructure
	tabBrowse      = core.TabBrowse
	tabSQL         = core.TabSQL
	tabIndexes     = core.TabIndexes
	tabForeignKeys = core.TabForeignKeys
)

type Model struct {
	core.Workflow
	pickerDir                                                                                      string
	appContext                                                                                     context.Context
	openTarget                                                                                     func(string) tea.Cmd
	running, cancelRequested, pendingQuit, browseLoading                                           bool
	requestID, activeRequestID, browsePageTag, editorEditTag                                       uint64
	cancel                                                                                         context.CancelFunc
	schema, picker, recent                                                                         list.Model
	structure, browse, results, indexes, foreignKeys, queryLog                                     table.Model
	structureColumns                                                                               []sharedsql.ColumnInfo
	indexInfo                                                                                      []sharedsql.IndexInfo
	foreignKeyInfo                                                                                 []sharedsql.ForeignKeyInfo
	referencingForeignKeyInfo                                                                      []sharedsql.ReferencingForeignKeyInfo
	browseNumericColumns, resultsNumericColumns                                                    []bool
	databaseInfo                                                                                   sharedsql.DatabaseInfo
	browseResult                                                                                   sharedsql.Result
	resultsStatus, browseStatus                                                                    string
	queryLogEntries                                                                                []queryLogEntry
	queryLogPendingG                                                                               bool
	editor                                                                                         *editor
	formMode                                                                                       *formModeController
	columnForm                                                                                     columnForm
	browseForm                                                                                     browseForm
	indexForm                                                                                      indexForm
	foreignKeyForm                                                                                 foreignKeyForm
	connection                                                                                     connectionForm
	recentConnections                                                                              []recentConnection
	recentPath                                                                                     string
	width, height, schemaWidth, editorWidth                                                        int
	workspaceHeight, queryLogHeight                                                                int
	editorHeight, resultsHeight, tableViewportWidth                                                int
	structureOffset, browseOffset, resultsOffset, indexesOffset, foreignKeysOffset, queryLogOffset int
	compact, fullscreen, relationshipDiagram                                                       bool
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
	model := Model{
		Workflow:    core.New(target),
		appContext:  opener.ctx,
		openTarget:  opener.command,
		schema:      newSchemaList(),
		picker:      newList("Choose database", true),
		recent:      newList("Recent connections", true),
		structure:   newResultsTable(),
		browse:      newResultsTable(),
		results:     newResultsTable(),
		indexes:     newResultsTable(),
		foreignKeys: newResultsTable(),
		queryLog:    newResultsTable(),
		editor:      newEditor(),
		formMode:    &formModeController{},
		connection:  newConnectionForm(),
	}
	model.queryLog.SetColumns(tableColumns([]string{"Time", "Query", "Duration", "Fetch"}, nil))
	model.queryLog.Blur()
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
	if m.State == core.StateConnection {
		return m.connection.form.Init()
	}
	return nil
}
