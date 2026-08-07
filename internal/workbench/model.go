package workbench

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

const compactWidth = 90
const defaultBrowsePageSize = core.BrowsePageSize

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
	focusChat      = core.FocusChat

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
	openDatabase                                                                                   OpenDatabase
	browseLoading, browsePending                                                                   bool
	browsePageTag, editorEditTag, completionRequestTag                                             uint64
	editorValidity                                                                                 sqlValidity
	sqlValidationTag                                                                               uint64
	schema, picker, recent                                                                         list.Model
	structure, browse, results, indexes, foreignKeys, queryLog                                     table.Model
	structureRows, indexRows, foreignKeyRows                                                       []table.Row
	structureColumns                                                                               []sharedsql.ColumnInfo
	completionColumns                                                                              map[string][]string
	completionTable                                                                                string
	indexInfo                                                                                      []sharedsql.IndexInfo
	foreignKeyInfo                                                                                 []sharedsql.ForeignKeyInfo
	referencingForeignKeyInfo                                                                      []sharedsql.ReferencingForeignKeyInfo
	browseNumericColumns, resultsNumericColumns                                                    []bool
	databaseInfo                                                                                   sharedsql.DatabaseInfo
	browseResult                                                                                   sharedsql.Result
	resultsRaw                                                                                     [][]*string
	resultsStatus, browseStatus                                                                    string
	queryLogEntries                                                                                []queryLogEntry
	queryLogDetail                                                                                 *queryLogEntry
	queryLogPage, queryLogPageSize                                                                 int
	queryLogPendingG                                                                               bool
	queryHistory                                                                                   []string
	historyIndex                                                                                   int
	editor                                                                                         *editor
	chat                                                                                           chatModel
	explainPicker                                                                                  *explainPicker
	chatHistoryPicker                                                                              *huh.Form
	formMode                                                                                       *formModeController
	columnForm                                                                                     columnForm
	tableForm                                                                                      tableForm
	tableFormRunning                                                                               bool
	browseForm                                                                                     browseForm
	browseFilterForm                                                                               *browseFilterForm
	browseSettings                                                                                 browseSettings
	browsePageSize                                                                                 int
	indexForm                                                                                      indexForm
	foreignKeyForm                                                                                 foreignKeyForm
	cellEditor                                                                                     *cellEditor
	cellViewer                                                                                     *cellViewer
	connection                                                                                     connectionForm
	connectionID                                                                                   string
	recentConnections                                                                              []recentConnection
	schemaObjects                                                                                  []sharedsql.SchemaObject
	expandedDatabases                                                                              map[string]bool
	expandedSchemas                                                                                map[string]bool
	commandPalette                                                                                 *commandPalette
	themePicker                                                                                    *themePicker
	quitDialog                                                                                     *confirmationDialog
	queryConfirmation                                                                              *queryConfirmation
	recentPath, queryLogPath, configPath                                                           string
	queryLogDatabase                                                                               *sql.DB
	keybindings                                                                                    Keybindings
	tableFilterInput                                                                               textinput.Model
	width, height, schemaWidth, editorWidth, chatWidth                                             int
	workspaceHeight, queryLogHeight                                                                int
	editorHeight, resultsHeight, tableViewportWidth                                                int
	structureOffset, browseOffset, resultsOffset, indexesOffset, foreignKeysOffset, queryLogOffset int
	structureColumn, browseColumn, resultsColumn, indexesColumn, foreignKeysColumn, queryLogColumn int
	compact, fullscreen, relationshipDiagram, tableFiltering                                       bool
	tableFilterTab                                                                                 workspaceTab
	structureFilter, indexesFilter, foreignKeysFilter                                              string
	lastClickTime                                                                                  time.Time
	lastClickX, lastClickY                                                                         int
	lastClickTab                                                                                   workspaceTab
	lastClickRow                                                                                   int
	lastFormClickTime                                                                              time.Time
	lastFormClickX, lastFormClickY                                                                 int
	chatKeepInsert                                                                                 bool
	formButtonHit                                                                                  bool
	vimMode                                                                                        bool
	contextMenu                                                                                    *contextMenuModel
	deleteConfirm                                                                                  *confirmationDialog
	deletePending                                                                                  string
	deletePendingName                                                                              string
	deletePendingDatabase                                                                          string
}

type pickerItem struct{ raw, title, description string }

func (i pickerItem) FilterValue() string { return i.title }
func (i pickerItem) Title() string       { return i.title }
func (i pickerItem) Description() string { return i.description }

type menuOption struct {
	label  string
	action string
	keys   string
}

type contextMenuModel struct {
	options  []menuOption
	selected int
	x, y     int // screen position (top-left of border)
	database string
	schema   string
	table    string
	visible  bool
}

type schemaItem struct {
	title, description string
	database, table    string
	schema             string
	kind               string
	root               bool
	count              int // child tables/views; -1 = unknown (not rendered)
}

func (i schemaItem) FilterValue() string {
	return strings.TrimSpace(i.database + " " + i.title)
}

func (i schemaItem) Title() string       { return i.title }
func (i schemaItem) Description() string { return i.description }

type databaseOpenedMsg struct {
	target    string
	service   sharedsql.Service
	info      sharedsql.DatabaseInfo
	objects   []sharedsql.SchemaObject
	reconnect bool // sidebar database switch: no new connection profile
	err       error
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

type OpenDatabase func(context.Context, string) (sharedsql.Opened, error)

func New(target string, ctx context.Context, openDatabase OpenDatabase, readOnly bool) Model {
	model := Model{
		Workflow:          core.New(target),
		appContext:        ctx,
		openDatabase:      openDatabase,
		schema:            newSchemaList(),
		picker:            newList("Choose database", true),
		recent:            newList("", true),
		expandedDatabases: map[string]bool{},
		expandedSchemas:   map[string]bool{},
		structure:         newResultsTable(),
		browse:            newResultsTable(),
		results:           newResultsTable(),
		indexes:           newResultsTable(),
		foreignKeys:       newResultsTable(),
		queryLog:          newResultsTable(),
		editor:            newEditor(),
		chat:              newChatModel(),
		formMode:          &formModeController{},
		connection:        newConnectionForm(),
		completionColumns: map[string][]string{},
		keybindings:       DefaultKeybindings(),
		vimMode:           vimModeEnabled(),
		browsePageSize:    browsePageSizeDefault(),
		historyIndex:      -1,
		queryLogPageSize:  queryLogPageSize(),
	}
	model.ReadOnly = readOnly || appConfig.ReadOnly
	if appConfig.ReadOnly {
		// Pre-check the per-connection toggle so fresh forms keep the
		// configured default; the user can still opt a connection back.
		model.connection.values.readOnly = true
	}
	model.commandPalette = newCommandPalette(model)
	model.recent.SetShowTitle(false)
	model.queryLog.SetColumns(tableColumns([]string{"Time", "Status", "Statement", "Duration", "Message"}, nil))
	model.queryLog.Blur()
	model.focusActiveTable()
	model.queryLogPath, _ = queryLogPath()
	model.queryLogEntries = loadQueryLog(model.queryLogPath, "")
	model.renderQueryLog()
	model.recentPath, _ = recentConnectionsPath()
	model.configPath = ConfigPath()
	var migrated bool
	model.recentConnections, migrated = loadRecentConnections(model.recentPath)
	if migrated {
		// Best-effort: persist the assigned legacy profile IDs immediately.
		_ = saveRecentConnections(model.recentPath, model.recentConnections)
	}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	return model
}

func (m Model) openTarget(target string) tea.Cmd { return m.openTargetWith(target, false) }

// reopenTarget mirrors openTarget for sidebar database switching: the open
// completes without recording a new connection profile.
func (m Model) reopenTarget(target string) tea.Cmd { return m.openTargetWith(target, true) }

func (m Model) openTargetWith(target string, reconnect bool) tea.Cmd {
	return func() tea.Msg {
		opened, err := m.openDatabase(m.appContext, target)
		if err != nil {
			return databaseOpenedMsg{err: err, reconnect: reconnect}
		}
		return databaseOpenedMsg{
			target:    opened.Target,
			service:   opened.Service,
			info:      opened.Info,
			objects:   opened.Objects,
			reconnect: reconnect,
		}
	}
}

// reconnectDatabase switches a PostgreSQL session to another database on
// the same server: the sidebar's non-connected roots become the connected
// database. PostgreSQL cannot address objects outside the connected
// database, so opening the root is the only way to reach it.
func (m Model) reconnectDatabase(database string) (tea.Model, tea.Cmd) {
	target := m.postgresTargetFor(database)
	if target == "" {
		m.Status = safeText("cannot reconnect to " + database)
		return m, nil
	}
	m.BeginOpening(target, "opening "+safeText(database))
	return m, m.reopenTarget(target)
}

// postgresTargetFor rewrites the current PostgreSQL target to connect to
// database on the same host, preserving user, port, and TLS settings.
func (m Model) postgresTargetFor(database string) string {
	u, err := url.Parse(strings.TrimSpace(m.Target))
	if err != nil || u.Scheme != "postgres" || u.Host == "" {
		return ""
	}
	u.Path = "/" + database // String() escapes the raw path once
	return u.String()
}

// connectedDatabase returns the PostgreSQL database the session is
// connected to, taken from the target URL's (unescaped) path.
func (m Model) connectedDatabase() string {
	u, err := url.Parse(strings.TrimSpace(m.Target))
	if err != nil || u.Scheme != "postgres" || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// databaseRootConnected reports whether database is the connected
// PostgreSQL database. The target URL's database name is authoritative;
// schema children are only a fallback when the target carries no path.
func (m Model) databaseRootConnected(database string) bool {
	if connected := m.connectedDatabase(); connected != "" {
		return connected == database
	}
	for _, object := range m.schemaObjects {
		if object.Type == "schema" && object.Database == database {
			return true
		}
	}
	return false
}

func (m *Model) Service() sharedsql.Service { return m.Database }

func (m *Model) SetKeybindings(b Keybindings) {
	m.keybindings = b
	m.commandPalette = newCommandPalette(*m)
}

// browsePageSizeDefault returns the configured default browse page size,
// falling back to the built-in value when unset.
func browsePageSizeDefault() int {
	if appConfig.BrowsePageSize > 0 {
		return appConfig.BrowsePageSize
	}
	return core.BrowsePageSize
}

func (m *Model) disconnect() {
	if m.Database != nil {
		_ = m.Database.Close()
	}
	m.State = stateConnection
	m.layout(m.width, m.height)
	m.Database = nil
	m.SelectedTable = ""
	m.BrowsePage = 0
	m.Status = ""
	m.queryLogEntries = nil
	m.queryLogPage = 0
	m.queryHistory = nil
	m.historyIndex = -1
	m.queryLog.SetRows(nil)
	m.editor.setValue("")
	m.editorValidity = sqlValidityPending
	m.sqlValidationTag++
	m.completionColumns = map[string][]string{}
	m.completionTable = ""
	m.schemaObjects = nil
	m.expandedDatabases = map[string]bool{}
	m.expandedSchemas = map[string]bool{}
	m.databaseInfo = sharedsql.DatabaseInfo{}
	m.connectionID = ""
	m.schema.SetItems(nil)
	m.structure.SetRows(nil)
	m.browse.SetRows(nil)
	m.resultsRaw = nil
	m.browseResult = sharedsql.Result{}
	m.results.SetRows(nil)
	m.indexes.SetRows(nil)
	m.foreignKeys.SetRows(nil)
	m.recentPath, _ = recentConnectionsPath()
	m.recentConnections, _ = loadRecentConnections(m.recentPath)
	_ = m.recent.SetItems(recentListItems(m.recentConnections))
	m.chat.yoloWrites = false
	for _, run := range m.chat.runs {
		if run.roundState != nil {
			run.roundState.releaseContexts()
		}
		if run.cancel != nil {
			run.cancel()
		}
	}
	m.chat.runs = map[string]*chatRun{}
	m.chat.activeID = ""
}
func (m Model) Init() tea.Cmd {
	if m.State == core.StateOpening {
		return m.openTarget(m.Target)
	}
	if m.State == core.StateConnection {
		return m.connection.form.Init()
	}
	return nil
}

func (m *Model) setSchemaObjects(objects []sharedsql.SchemaObject) tea.Cmd {
	m.schemaObjects = objects
	if m.expandedDatabases == nil {
		m.expandedDatabases = map[string]bool{}
	}
	if m.expandedSchemas == nil {
		m.expandedSchemas = map[string]bool{}
	}
	for _, object := range objects {
		switch object.Type {
		case "database":
			m.expandedDatabases[object.Database] = true
		case "schema":
			m.expandedSchemas[m.schemaExpansionKey(object.Database, object.Name)] = true
		}
	}
	cmd := m.rebuildSchemaTree()
	// Keep the cursor on the connected root: switching databases rebuilds
	// the tree, and with roots in stable alphabetical order the selection
	// must land where the user picked, not on the first item.
	if m.databaseInfo.Product == "PostgreSQL" {
		if connected := m.connectedDatabase(); connected != "" {
			for index, item := range m.schema.Items() {
				if root, ok := item.(schemaItem); ok && root.root && root.database == connected {
					m.schema.Select(index)
					break
				}
			}
		}
	}
	return cmd
}

// schemaExpansionKey identifies a schema under a database root; the
// separator cannot appear in either name.
func (m Model) schemaExpansionKey(database, schema string) string {
	return database + "\x00" + schema
}

func (m *Model) rebuildSchemaTree() tea.Cmd {
	items := make([]list.Item, 0, len(m.schemaObjects))
	schemaCounts, databaseCounts := m.schemaChildCounts()
	for _, object := range m.schemaObjects {
		switch object.Type {
		case "database":
			description := ""
			if !m.expandedDatabases[object.Database] {
				description = "collapsed"
			}
			// PostgreSQL objects outside the connected database are not
			// introspected, so only its root gets a child count.
			count := -1
			if m.databaseInfo.Product != "PostgreSQL" || m.databaseRootConnected(object.Database) {
				count = databaseCounts[object.Database]
			}
			items = append(items, schemaItem{title: object.Name, description: description, database: object.Database, root: true, count: count})
		case "schema":
			// PostgreSQL only: schemas nest under the connected
			// database's root.
			if !m.expandedDatabases[object.Database] {
				continue
			}
			description := ""
			if !m.expandedSchemas[m.schemaExpansionKey(object.Database, object.Name)] {
				description = "collapsed"
			}
			items = append(items, schemaItem{title: object.Name, description: description, database: object.Database, schema: object.Name, kind: "schema", count: schemaCounts[m.schemaExpansionKey(object.Database, object.Name)]})
		default: // table or view
			if !m.expandedDatabases[object.Database] {
				continue
			}
			table := object.Name
			schema := ""
			if m.databaseInfo.Product == "PostgreSQL" {
				// The name carries schema.table; only the connected
				// database's tables are listed.
				var found bool
				schema, table, found = strings.Cut(object.Name, ".")
				if !found {
					continue
				}
				if !m.expandedSchemas[m.schemaExpansionKey(object.Database, schema)] {
					continue
				}
			}
			items = append(items, schemaItem{title: table, description: object.Type, database: object.Database, schema: schema, table: object.Name, kind: object.Type})
		}
	}
	return m.schema.SetItems(items)
}

// schemaChildCounts tallies the table/view objects under each database and
// schema so expander rows can show child counts from data already loaded.
func (m Model) schemaChildCounts() (schemaCounts, databaseCounts map[string]int) {
	schemaCounts = map[string]int{}
	databaseCounts = map[string]int{}
	for _, object := range m.schemaObjects {
		switch object.Type {
		case "table", "view":
			databaseCounts[object.Database]++
			if m.databaseInfo.Product == "PostgreSQL" {
				if schema, _, ok := strings.Cut(object.Name, "."); ok {
					schemaCounts[m.schemaExpansionKey(object.Database, schema)]++
				}
			}
		}
	}
	return schemaCounts, databaseCounts
}

func (m Model) schemaTable(item schemaItem) string {
	table := item.table
	if table == "" {
		table = item.title
	}
	if m.databaseInfo.Product == "MySQL" {
		return item.database + "." + table
	}
	return table
}
