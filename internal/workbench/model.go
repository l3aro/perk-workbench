package workbench

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/core"
	"github.com/l3aro/perk-workbench/internal/log"
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
	pickerDir                                                  string
	appContext                                                 context.Context
	openDatabase                                               OpenDatabase
	browseLoading, browsePending                               bool
	reconnectPending                                           bool
	openTag                                                    uint64
	treeAnim                                                   *treeAnim
	browsePageTag, editorEditTag, completionRequestTag         uint64
	editorValidity                                             sqlValidity
	sqlValidationTag                                           uint64
	schema, picker, recent                                     list.Model
	schemaFilter, recentFilter                                 textinput.Model
	structure, browse, results, indexes, foreignKeys, queryLog table.Model
	structureRows, indexRows, foreignKeyRows                   []table.Row
	structureColumns                                           []sharedsql.ColumnInfo
	completionColumns                                          map[string][]string
	completionTable                                            string
	indexInfo                                                  []sharedsql.IndexInfo
	foreignKeyInfo                                             []sharedsql.ForeignKeyInfo
	referencingForeignKeyInfo                                  []sharedsql.ReferencingForeignKeyInfo
	browseNumericColumns, resultsNumericColumns                []bool
	databaseInfo                                               sharedsql.DatabaseInfo
	browseResult                                               sharedsql.Result
	resultsRaw                                                 [][]*string
	resultsStatus, browseStatus                                string
	queryLogEntries                                            []queryLogEntry
	queryLogDetail                                             *queryLogEntry
	queryLogPage, queryLogPageSize                             int
	queryLogPendingG                                           bool
	queryHistory                                               []string
	historyIndex                                               int
	editor                                                     *editor
	chat                                                       chatModel
	explainPicker                                              *explainPicker
	chatHistoryPicker                                          *huh.Form
	formMode                                                   *formModeController
	columnForm                                                 columnForm
	tableForm                                                  tableForm
	tableFormRunning                                           bool
	browseForm                                                 browseForm
	browseFilterForm                                           *browseFilterForm
	browseSettings                                             browseSettings
	browsePageSize                                             int
	indexForm                                                  indexForm
	foreignKeyForm                                             foreignKeyForm
	cellEditor                                                 *cellEditor
	cellViewer                                                 *cellViewer
	connection                                                 connectionForm
	connectionID                                               string
	recentConnections                                          []recentConnection
	schemaObjects                                              []sharedsql.SchemaObject
	expandedDatabases                                          map[string]bool
	expandedSchemas                                            map[string]bool
	commandPalette                                             *commandPalette
	themePicker                                                *themePicker
	tableTargetPicker                                          *tableTargetPicker
	quitDialog                                                 *confirmationDialog
	queryConfirmation                                          *queryConfirmation
	recentPath, queryLogPath, configPath, notificationPath     string
	queryLogDatabase                                           *sql.DB
	notificationDatabase                                       *sql.DB
	notificationEntries                                        []notificationEntry
	notificationPopup                                          *notificationEntry
	notificationDetail                                         *notificationEntry
	notificationHistory                                        *notificationHistory
	notificationGeneration                                     uint64
	notificationPopupSwallowRelease                            bool
	statusRevision                                             uint64
	// skipStatusPopup suppresses the plain status popup for the current
	// update; the status change is surfaced as a log notification instead.
	skipStatusPopup bool
	// skipNotificationPersist keeps the next drained log notification
	// transient: the popup and event.log stay, but history persistence is
	// skipped. The opening transition logs before the connection profile
	// exists, so persisting it would bind it to the wrong scope.
	skipNotificationPersist                                                                        bool
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
	deletePendingConnection                                                                        *recentConnection
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
	title    string // menu title; empty renders the default "Row actions"
	visible  bool
}

type schemaItem struct {
	title, description string
	database, table    string
	schema             string
	kind               string
	root               bool
	open               bool   // on the path from the root to the open table
	count              int    // child tables/views; -1 = unknown (not rendered)
	rowCount           *int64 // estimated rows for tables; nil = unknown
}

func (i schemaItem) FilterValue() string {
	parts := []string{i.database, i.title}
	if i.schema != "" {
		parts = append(parts, i.schema)
	}
	if i.kind != "" {
		parts = append(parts, i.kind)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func (i schemaItem) Title() string       { return i.title }
func (i schemaItem) Description() string { return i.description }

type databaseOpenedMsg struct {
	target    string
	service   sharedsql.Service
	info      sharedsql.DatabaseInfo
	objects   []sharedsql.SchemaObject
	reconnect bool // sidebar database switch: no new connection profile
	openTag   uint64
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
		schemaFilter:      newFilterInput(),
		recentFilter:      newFilterInput(),
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
	// The profiles pane renders its own persistent filter input (like the
	// schema sidebar); the list's built-in filter bar and keybinding are
	// unused.
	model.recent.SetShowFilter(false)
	model.recent.KeyMap.Filter = key.NewBinding(key.WithDisabled())
	model.queryLog.SetColumns(tableColumns([]string{"Time", "Status", "Statement", "Duration", "Message"}, nil))
	model.queryLog.Blur()
	model.focusActiveTable()
	model.queryLogPath, _ = queryLogPath()
	model.queryLogEntries = loadQueryLog(model.queryLogPath, "")
	model.notificationPath, _ = notificationPath()
	model.renderQueryLog()
	model.recentPath, _ = recentConnectionsPath()
	model.configPath = ConfigPath()
	var migrated bool
	model.recentConnections, migrated = loadRecentConnections(model.recentPath)
	if migrated {
		// Best-effort: persist the assigned legacy profile IDs immediately.
		_ = saveRecentConnections(model.recentPath, model.recentConnections)
	}
	// Route every logged event into the notification popup pipeline.
	log.SetNotifier(enqueueLogNotification)
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	return model
}

func (m Model) openTarget(target string) tea.Cmd { return m.openTargetWith(target, false) }

// reopenTarget mirrors openTarget for sidebar database switching: the open
// completes without recording a new connection profile.
func (m Model) reopenTarget(target string) tea.Cmd { return m.openTargetWith(target, true) }

func (m Model) openTargetWith(target string, reconnect bool) tea.Cmd {
	tag := m.openTag
	return func() tea.Msg {
		opened, err := m.openDatabase(m.appContext, target)
		if err != nil {
			return databaseOpenedMsg{err: err, reconnect: reconnect, openTag: tag}
		}
		return databaseOpenedMsg{
			target:    opened.Target,
			service:   opened.Service,
			info:      opened.Info,
			objects:   opened.Objects,
			reconnect: reconnect,
			openTag:   tag,
		}
	}
}

// reconnectDatabase switches a PostgreSQL session to another database on
// the same server: the sidebar's non-connected roots become the connected
// database. PostgreSQL cannot address objects outside the connected
// database, so opening the root is the only way to reach it.
func (m Model) reconnectDatabase(database string) (tea.Model, tea.Cmd) {
	if m.reconnectPending {
		return m, nil // a switch is already in flight
	}
	target := m.postgresTargetFor(database)
	if target == "" {
		m.setStatus(safeText("cannot reconnect to " + database))
		return m, nil
	}
	// Stay in stateReady so the current view keeps rendering while the
	// switch loads; the previous database stays usable until the swap.
	m.reconnectPending = true
	m.openTag++
	m.treeAnim = nil
	m.setStatus(safeText("switching to " + database))
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
	if m.notificationDatabase != nil {
		_ = m.notificationDatabase.Close()
		m.notificationDatabase = nil
	}
	m.notificationEntries = nil
	m.notificationPopup = nil
	m.notificationDetail = nil
	m.notificationHistory = nil
	m.State = stateConnection
	m.reconnectPending = false
	m.treeAnim = nil
	m.openTag++ // supersede any open still in flight
	m.layout(m.width, m.height)
	m.Database = nil
	m.SelectedTable = ""
	m.BrowsePage = 0
	m.setStatus("")
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
	m.schema.ResetFilter()
	m.schemaFilter.SetValue("")
	m.schemaFilter.Blur()
	m.recent.ResetFilter()
	m.recentFilter.SetValue("")
	m.recentFilter.Blur()
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
	m.treeAnim = nil // the tree changed wholesale; no accordion to continue
	if m.expandedDatabases == nil {
		m.expandedDatabases = map[string]bool{}
	}
	if m.expandedSchemas == nil {
		m.expandedSchemas = map[string]bool{}
	}
	// Default expansion mirrors the toggle rule: server products open
	// exactly one database root (the connected one, else the first) and
	// PostgreSQL exactly one schema, so a fresh tree never shows every
	// database's or schema's children at once. Single-root products
	// (SQLite, MongoDB) have nothing to collapse.
	m.expandedDatabases = m.initialDatabaseExpansion(objects)
	m.expandedSchemas = m.initialSchemaExpansion(objects)
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

// toggleDatabase expands database when collapsed and collapses it when
// expanded; expanding one root collapses every other, so at most one
// database shows children at a time. It returns the accordion tick command.
func (m *Model) toggleDatabase(database string) tea.Cmd {
	expanding := !m.expandedDatabases[database]
	total := m.schemaChildRowCount(database, "", expanding)
	if m.expandedDatabases[database] {
		m.expandedDatabases[database] = false
	} else {
		for db := range m.expandedDatabases {
			m.expandedDatabases[db] = false
		}
		m.expandedDatabases[database] = true
	}
	return m.startTreeAnim(database, "", expanding, total)
}

// toggleSchema expands schema when collapsed and collapses it when
// expanded; expanding one schema collapses every other, so at most one
// schema shows its tables at a time. It returns the accordion tick command.
func (m *Model) toggleSchema(database, schema string) tea.Cmd {
	key := m.schemaExpansionKey(database, schema)
	expanding := !m.expandedSchemas[key]
	total := m.schemaChildRowCount(database, schema, expanding)
	if m.expandedSchemas[key] {
		m.expandedSchemas[key] = false
	} else {
		for k := range m.expandedSchemas {
			m.expandedSchemas[k] = false
		}
		m.expandedSchemas[key] = true
	}
	return m.startTreeAnim(database, schema, expanding, total)
}

// initialDatabaseExpansion returns the load-time database expansion: the
// root holding the open table, else the connected database, else the
// first root. Server products expand exactly one root so a fresh session
// never shows every database's tables at once; single-root products
// expand everything (their only root).
func (m Model) initialDatabaseExpansion(objects []sharedsql.SchemaObject) map[string]bool {
	expanded := map[string]bool{}
	if m.databaseInfo.Product != "MySQL" && m.databaseInfo.Product != "PostgreSQL" {
		for _, object := range objects {
			if object.Type == "database" {
				expanded[object.Database] = true
			}
		}
		return expanded
	}
	preferred := m.preferredDatabase()
	if m.SelectedTable != "" {
		switch m.databaseInfo.Product {
		case "MySQL":
			// MySQL qualifies tables as database.table.
			if database, _, ok := strings.Cut(m.SelectedTable, "."); ok {
				preferred = database
			}
		case "PostgreSQL":
			if connected := m.connectedDatabase(); connected != "" {
				preferred = connected
			}
		}
	}
	first := ""
	for _, object := range objects {
		if object.Type != "database" {
			continue
		}
		if first == "" {
			first = object.Database
		}
		if preferred != "" && object.Database == preferred {
			expanded[preferred] = true
			return expanded
		}
	}
	if first != "" {
		expanded[first] = true
	}
	return expanded
}

// preferredDatabase is the database the session is connected to when the
// product tracks one: PostgreSQL names it in the target URL, MySQL in the
// DSN's database field.
func (m Model) preferredDatabase() string {
	switch m.databaseInfo.Product {
	case "PostgreSQL":
		return m.connectedDatabase()
	case "MySQL":
		return m.mysqlDatabase()
	}
	return ""
}

// mysqlDatabase extracts the database name from the MySQL DSN target.
func (m Model) mysqlDatabase() string {
	dsn, ok := strings.CutPrefix(m.Target, "mysql:")
	if !ok {
		return ""
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return ""
	}
	return config.DBName
}

// initialSchemaExpansion returns the load-time PostgreSQL schema
// expansion: the schema holding the open table, else the first schema of
// the expanded database. Exactly one schema is expanded so a fresh
// session never shows every schema's tables at once.
func (m Model) initialSchemaExpansion(objects []sharedsql.SchemaObject) map[string]bool {
	expanded := map[string]bool{}
	preferred := ""
	if m.SelectedTable != "" {
		// PostgreSQL qualifies tables as schema.table.
		preferred, _, _ = strings.Cut(m.SelectedTable, ".")
	}
	for _, object := range objects {
		if object.Type == "schema" && object.Name == preferred {
			expanded[m.schemaExpansionKey(object.Database, object.Name)] = true
			return expanded
		}
	}
	for _, object := range objects {
		if object.Type == "schema" && m.expandedDatabases[object.Database] {
			expanded[m.schemaExpansionKey(object.Database, object.Name)] = true
			break
		}
	}
	return expanded
}

func (m *Model) rebuildSchemaTree() tea.Cmd {
	items := make([]list.Item, 0, len(m.schemaObjects))
	schemaCounts, databaseCounts := m.schemaChildCounts()
	animDatabase, animSchema, revealBudget := m.schemaReveal()
	revealUsed := 0
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
			item := schemaItem{title: object.Name, description: description, database: object.Database, root: true, count: count}
			item.open = m.schemaOpenPath(item)
			items = append(items, item)
		case "schema":
			// PostgreSQL only: schemas nest under the connected
			// database's root.
			if !m.expandedDatabases[object.Database] {
				// A closing subtree keeps rendering during its collapse.
				if animDatabase != object.Database || animSchema != "" {
					continue
				}
			}
			// A database-scope accordion counts schema rows as children.
			if animDatabase == object.Database && animSchema == "" {
				revealUsed++
				if revealUsed > revealBudget {
					continue
				}
			}
			description := ""
			if !m.expandedSchemas[m.schemaExpansionKey(object.Database, object.Name)] {
				description = "collapsed"
			}
			item := schemaItem{title: object.Name, description: description, database: object.Database, schema: object.Name, kind: "schema", count: schemaCounts[m.schemaExpansionKey(object.Database, object.Name)]}
			item.open = m.schemaOpenPath(item)
			items = append(items, item)
		default: // table or view
			if !m.expandedDatabases[object.Database] {
				if animDatabase != object.Database || animSchema != "" {
					continue
				}
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
					// A closing schema keeps rendering its tables during
					// the collapse; other schemas stay hidden.
					if animDatabase != object.Database || animSchema != schema {
						continue
					}
				}
			}
			// The accordion budgets the animated subtree's rows: all rows
			// of a database-scope reveal, or just the schema's tables.
			if animDatabase == object.Database && (animSchema == "" || animSchema == schema) {
				revealUsed++
				if revealUsed > revealBudget {
					continue
				}
			}
			item := schemaItem{title: table, description: object.Type, database: object.Database, schema: schema, table: object.Name, kind: object.Type}
			if object.Type == "table" {
				item.rowCount = object.RowCount
			}
			item.open = m.schemaOpenPath(item)
			items = append(items, item)
		}
	}
	return m.schema.SetItems(items)
}

// schemaOpenPath reports whether item lies on the path from its root down to
// the currently open table, which is the sidebar's "opened" state.
func (m Model) schemaOpenPath(item schemaItem) bool {
	if m.SelectedTable == "" {
		return false
	}
	switch m.databaseInfo.Product {
	case "PostgreSQL":
		if !m.databaseRootConnected(item.database) {
			return false
		}
		schema, _, _ := strings.Cut(m.SelectedTable, ".")
		switch {
		case item.root:
			return true
		case item.kind == "schema":
			return item.schema == schema
		case item.table != "":
			return item.table == m.SelectedTable
		}
	case "MySQL":
		database, table, _ := strings.Cut(m.SelectedTable, ".")
		switch {
		case item.root:
			return item.database == database
		case item.table != "":
			return item.database == database && item.table == table
		}
	default: // SQLite: a single root per file, tables are bare names.
		switch {
		case item.root:
			return true
		case item.table != "":
			return item.table == m.SelectedTable
		}
	}
	return false
}

// schemaChildCounts tallies the table/view objects under each database and
// schema so expander rows can show child counts from data already loaded.
func (m Model) schemaChildCounts() (schemaCounts, databaseCounts map[string]int) {
	schemaCounts = map[string]int{}
	databaseCounts = map[string]int{}
	for _, object := range m.schemaObjects {
		switch object.Type {
		case "table", "view", "collection":
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

// applySchemaFilter pushes the visible filter input's value into the schema
// list, which filters its items and reports the committed state the status
// line mirrors.
func (m *Model) applySchemaFilter() {
	if query := strings.TrimSpace(m.schemaFilter.Value()); query != "" {
		m.schema.SetFilterText(query)
		return
	}
	m.schema.ResetFilter()
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
