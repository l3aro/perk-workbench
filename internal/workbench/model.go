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
	// Global lifecycle: application context, database opener, and the
	// connection scope that binds query log, notifications, and chat
	// history to the active profile. Feature-owned state lives in the
	// grouped structs below; root coordinates, features render.
	appContext       context.Context
	openDatabase     OpenDatabase
	reconnectPending bool
	openTag          uint64
	connectionID     string
	databaseInfo     sharedsql.DatabaseInfo
	keybindings      Keybindings
	vimMode          bool
	configPath       string
	connection       connectionState
	schema           schemaState
	queryLog         queryState
	browse           browseState
	structure        structureState
	notifications    notificationState
	overlay          overlayState
	layout           layoutState
	chat             chatModel
}

// connectionState owns the connection screen: the profile form, recent
// profile list and filter, the database file picker, and persisted
// profile storage.
type connectionState struct {
	form              connectionForm
	picker            list.Model
	pickerDir         string
	recent            list.Model
	recentFilter      textinput.Model
	recentConnections []recentConnection
	recentPath        string
}

// schemaState owns the schema sidebar: its list and filter, the loaded
// schema objects, database/schema expansion, and the accordion animation.
type schemaState struct {
	list              list.Model
	filter            textinput.Model
	objects           []sharedsql.SchemaObject
	expandedDatabases map[string]bool
	expandedSchemas   map[string]bool
	anim              *treeAnim
}

// queryState owns the SQL workspace: the editor, result table and raw
// result, completion, validation, and query-log history state.
type queryState struct {
	table                 table.Model // query log table
	entries               []queryLogEntry
	detail                *queryLogEntry
	page, pageSize        int
	pendingG              bool
	path                  string
	database              *sql.DB
	history               []string
	historyIndex          int
	editor                *editor
	editorValidity        sqlValidity
	editorEditTag         uint64
	validationTag         uint64
	completionRequestTag  uint64
	completionColumns     map[string][]string
	completionTable       string
	results               table.Model
	resultsRaw            [][]*string
	resultsStatus         string
	resultsNumericColumns []bool
}

// browseState owns the browse tab: the result table, row/document
// editors, cell viewer, settings, filter form, and paging state.
type browseState struct {
	table            table.Model
	result           sharedsql.Result
	status           string
	numericColumns   []bool
	loading, pending bool
	pageTag          uint64
	settings         browseSettings
	form             browseForm
	filterForm       *browseFilterForm
	pageSize         int
	cellEditor       *cellEditor
	documentEditor   *documentEditor
	cellViewer       *cellViewer
}

// structureState owns the structure/index/foreign-key tabs: their tables,
// forms, filters, row metadata, and the relationship diagram.
type structureState struct {
	table                     table.Model
	rows                      []table.Row
	columns                   []sharedsql.ColumnInfo
	structureFilter           string
	indexes                   table.Model
	indexRows                 []table.Row
	indexInfo                 []sharedsql.IndexInfo
	indexesFilter             string
	indexForm                 indexForm
	foreignKeys               table.Model
	foreignKeyRows            []table.Row
	foreignKeyInfo            []sharedsql.ForeignKeyInfo
	referencingForeignKeyInfo []sharedsql.ReferencingForeignKeyInfo
	foreignKeysFilter         string
	foreignKeyForm            foreignKeyForm
	columnForm                columnForm
	tableForm                 tableForm
	tableFormRunning          bool
	relationshipDiagram       bool
	tableFiltering            bool
	tableFilterInput          textinput.Model
	tableFilterTab            workspaceTab
}

// notificationState owns the notification pipeline: history persistence,
// popup/detail state, filter/sort/page behavior, and status-popup
// suppression.
type notificationState struct {
	database                *sql.DB
	entries                 []notificationEntry
	popup                   *notificationEntry
	detail                  *notificationEntry
	history                 *notificationHistory
	generation              uint64
	popupSwallowRelease     bool
	path                    string
	skipStatusPopup         bool
	skipNotificationPersist bool
	statusRevision          uint64
}

// overlayState owns transient root overlays: palette, pickers, menus,
// confirmations, quit/delete dialogs, and the shared form-mode
// controller.
type overlayState struct {
	commandPalette          *commandPalette
	themePicker             *themePicker
	tableTargetPicker       *tableTargetPicker
	quitDialog              *confirmationDialog
	queryConfirmation       *queryConfirmation
	explainPicker           *explainPicker
	contextMenu             *contextMenuModel
	deleteConfirm           *confirmationDialog
	deletePending           string
	deletePendingName       string
	deletePendingDatabase   string
	deletePendingConnection *recentConnection
	formMode                *formModeController
}

// layoutState owns terminal geometry and pointer tracking: window and
// pane dimensions, table offsets/columns, and click debounce state.
type layoutState struct {
	width, height, schemaWidth, editorWidth, chatWidth int
	workspaceHeight, queryLogHeight                    int
	editorHeight, resultsHeight, tableViewportWidth    int
	structureOffset, browseOffset, resultsOffset       int
	indexesOffset, foreignKeysOffset, queryLogOffset   int
	structureColumn, browseColumn, resultsColumn       int
	indexesColumn, foreignKeysColumn, queryLogColumn   int
	compact, fullscreen                                bool
	lastClickTime                                      time.Time
	lastClickX, lastClickY                             int
	lastClickTab                                       workspaceTab
	lastClickRow                                       int
	lastFormClickTime                                  time.Time
	lastFormClickX, lastFormClickY                     int
	formButtonHit                                      bool
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
	// Positional fields joined with a unit separator: database, title,
	// schema, kind. The schema sidebar filter glob-matches the title,
	// schema, and kind fields (skipping the containing database); fuzzy
	// matching replaces the separator with a space to keep the historical
	// behavior.
	return strings.Join([]string{i.database, i.title, i.schema, i.kind}, "\x00")
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
		Workflow:     core.New(target),
		appContext:   ctx,
		openDatabase: openDatabase,
		connection: connectionState{
			form:         newConnectionForm(),
			picker:       newList("Choose database", true),
			recent:       newList("", true),
			recentFilter: newFilterInput(),
		},
		schema: schemaState{
			list:              newSchemaList(),
			filter:            newFilterInput(),
			expandedDatabases: map[string]bool{},
			expandedSchemas:   map[string]bool{},
		},
		queryLog: queryState{
			table:             newResultsTable(),
			results:           newResultsTable(),
			editor:            newEditor(),
			completionColumns: map[string][]string{},
			historyIndex:      -1,
			pageSize:          queryLogPageSize(),
		},
		browse: browseState{
			table:    newResultsTable(),
			pageSize: browsePageSizeDefault(),
		},
		structure: structureState{
			table:       newResultsTable(),
			indexes:     newResultsTable(),
			foreignKeys: newResultsTable(),
		},
		overlay: overlayState{
			formMode: &formModeController{},
		},
		chat:        newChatModel(),
		keybindings: DefaultKeybindings(),
		vimMode:     vimModeEnabled(),
	}
	model.ReadOnly = readOnly || appConfig.ReadOnly
	if appConfig.ReadOnly {
		// Pre-check the per-connection toggle so fresh forms keep the
		// configured default; the user can still opt a connection back.
		model.connection.form.values.readOnly = true
	}
	model.overlay.commandPalette = newCommandPalette(model)
	model.connection.recent.SetShowTitle(false)
	// The profiles pane renders its own persistent filter input (like the
	// schema sidebar); the list's built-in filter bar and keybinding are
	// unused.
	model.connection.recent.SetShowFilter(false)
	model.connection.recent.KeyMap.Filter = key.NewBinding(key.WithDisabled())
	model.queryLog.table.SetColumns(tableColumns([]string{"Time", "Status", "Statement", "Duration", "Message"}, nil))
	model.queryLog.table.Blur()
	model.focusActiveTable()
	model.queryLog.path, _ = queryLogPath()
	model.queryLog.entries = loadQueryLog(model.queryLog.path, "")
	model.notifications.path, _ = notificationPath()
	model.renderQueryLog()
	model.connection.recentPath, _ = recentConnectionsPath()
	model.configPath = ConfigPath()
	var migrated bool
	model.connection.recentConnections, migrated = loadRecentConnections(model.connection.recentPath)
	if migrated {
		// Best-effort: persist the assigned legacy profile IDs immediately.
		_ = saveRecentConnections(model.connection.recentPath, model.connection.recentConnections)
	}
	// Route every logged event into the notification popup pipeline.
	log.SetNotifier(enqueueLogNotification)
	_ = model.connection.recent.SetItems(recentListItems(model.connection.recentConnections))
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
	m.schema.anim = nil
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
	for _, object := range m.schema.objects {
		if object.Type == "schema" && object.Database == database {
			return true
		}
	}
	return false
}

func (m *Model) Service() sharedsql.Service { return m.Database }

func (m *Model) SetKeybindings(b Keybindings) {
	m.keybindings = b
	m.overlay.commandPalette = newCommandPalette(*m)
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
	m.notifications.reset()
	m.State = stateConnection
	m.reconnectPending = false
	// The accordion animation is dropped before relayout, matching the
	// original transition order; schema.reset() below re-clears it.
	m.schema.anim = nil
	m.openTag++ // supersede any open still in flight
	m.applyLayout(m.layout.width, m.layout.height)
	m.Database = nil
	m.SelectedTable = ""
	m.BrowsePage = 0
	m.setStatus("")
	m.queryLog.reset()
	m.schema.reset()
	m.databaseInfo = sharedsql.DatabaseInfo{}
	m.connectionID = ""
	m.structure.reset()
	m.browse.reset()
	m.connection.reset()
	m.overlay.reset()
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

// reset clears query workspace state: the editor, result table, raw
// result, completion data, validation tag, and query-log history.
func (s *queryState) reset() {
	s.entries = nil
	s.page = 0
	s.history = nil
	s.historyIndex = -1
	s.table.SetRows(nil)
	s.editor.setValue("")
	s.editorValidity = sqlValidityPending
	s.validationTag++
	s.completionColumns = map[string][]string{}
	s.completionTable = ""
	s.resultsRaw = nil
	s.results.SetRows(nil)
}

// reset clears the schema sidebar: loaded objects, expansion, the tree
// list, and the filter input.
func (s *schemaState) reset() {
	s.anim = nil
	s.objects = nil
	s.expandedDatabases = map[string]bool{}
	s.expandedSchemas = map[string]bool{}
	s.list.SetItems(nil)
	s.list.ResetFilter()
	s.filter.SetValue("")
	s.filter.Blur()
}

// reset clears the structure, index, and foreign-key tables.
func (s *structureState) reset() {
	s.table.SetRows(nil)
	s.indexes.SetRows(nil)
	s.foreignKeys.SetRows(nil)
}

// reset clears the browse result table and result data.
func (s *browseState) reset() {
	s.table.SetRows(nil)
	s.result = sharedsql.Result{}
}

// reset reloads the persisted profile list and clears the profile list
// and filter inputs.
func (s *connectionState) reset() {
	s.recentPath, _ = recentConnectionsPath()
	s.recentConnections, _ = loadRecentConnections(s.recentPath)
	_ = s.recent.SetItems(recentListItems(s.recentConnections))
	s.recent.ResetFilter()
	s.recentFilter.SetValue("")
	s.recentFilter.Blur()
}

// reset closes the notification history database and clears popup,
// detail, and history state.
func (s *notificationState) reset() {
	if s.database != nil {
		_ = s.database.Close()
		s.database = nil
	}
	s.entries = nil
	s.popup = nil
	s.detail = nil
	s.history = nil
}

// reset clears connection-scoped overlays. The current disconnect
// transition leaves transient overlays (palette, menus, dialogs) alone;
// this is the hook that owns them when a later step needs to.
func (s *overlayState) reset() {
}
func (m Model) Init() tea.Cmd {
	if m.State == core.StateOpening {
		return m.openTarget(m.Target)
	}
	if m.State == core.StateConnection {
		return m.connection.form.form.Init()
	}
	return nil
}

func (m *Model) setSchemaObjects(objects []sharedsql.SchemaObject) tea.Cmd {
	m.schema.objects = objects
	m.schema.anim = nil // the tree changed wholesale; no accordion to continue
	if m.schema.expandedDatabases == nil {
		m.schema.expandedDatabases = map[string]bool{}
	}
	if m.schema.expandedSchemas == nil {
		m.schema.expandedSchemas = map[string]bool{}
	}
	// Default expansion mirrors the toggle rule: server products open
	// exactly one database root (the connected one, else the first) and
	// PostgreSQL exactly one schema, so a fresh tree never shows every
	// database's or schema's children at once. Single-root products
	// (SQLite, MongoDB) have nothing to collapse.
	m.schema.expandedDatabases = m.initialDatabaseExpansion(objects)
	m.schema.expandedSchemas = m.initialSchemaExpansion(objects)
	cmd := m.rebuildSchemaTree()
	// Keep the cursor on the connected root: switching databases rebuilds
	// the tree, and with roots in stable alphabetical order the selection
	// must land where the user picked, not on the first item.
	if m.databaseInfo.Product == "PostgreSQL" {
		if connected := m.connectedDatabase(); connected != "" {
			for index, item := range m.schema.list.Items() {
				if root, ok := item.(schemaItem); ok && root.root && root.database == connected {
					m.schema.list.Select(index)
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
	expanding := !m.schema.expandedDatabases[database]
	total := m.schemaChildRowCount(database, "", expanding)
	if m.schema.expandedDatabases[database] {
		m.schema.expandedDatabases[database] = false
	} else {
		for db := range m.schema.expandedDatabases {
			m.schema.expandedDatabases[db] = false
		}
		m.schema.expandedDatabases[database] = true
	}
	return m.startTreeAnim(database, "", expanding, total)
}

// toggleSchema expands schema when collapsed and collapses it when
// expanded; expanding one schema collapses every other, so at most one
// schema shows its tables at a time. It returns the accordion tick command.
func (m *Model) toggleSchema(database, schema string) tea.Cmd {
	key := m.schemaExpansionKey(database, schema)
	expanding := !m.schema.expandedSchemas[key]
	total := m.schemaChildRowCount(database, schema, expanding)
	if m.schema.expandedSchemas[key] {
		m.schema.expandedSchemas[key] = false
	} else {
		for k := range m.schema.expandedSchemas {
			m.schema.expandedSchemas[k] = false
		}
		m.schema.expandedSchemas[key] = true
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
		if object.Type == "schema" && m.schema.expandedDatabases[object.Database] {
			expanded[m.schemaExpansionKey(object.Database, object.Name)] = true
			break
		}
	}
	return expanded
}

func (m *Model) rebuildSchemaTree() tea.Cmd {
	items := make([]list.Item, 0, len(m.schema.objects))
	schemaCounts, databaseCounts := m.schemaChildCounts()
	animDatabase, animSchema, revealBudget := m.schemaReveal()
	revealUsed := 0
	for _, object := range m.schema.objects {
		switch object.Type {
		case "database":
			description := ""
			if !m.schema.expandedDatabases[object.Database] {
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
			if !m.schema.expandedDatabases[object.Database] {
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
			if !m.schema.expandedSchemas[m.schemaExpansionKey(object.Database, object.Name)] {
				description = "collapsed"
			}
			item := schemaItem{title: object.Name, description: description, database: object.Database, schema: object.Name, kind: "schema", count: schemaCounts[m.schemaExpansionKey(object.Database, object.Name)]}
			item.open = m.schemaOpenPath(item)
			items = append(items, item)
		default: // table or view
			if !m.schema.expandedDatabases[object.Database] {
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
				if !m.schema.expandedSchemas[m.schemaExpansionKey(object.Database, schema)] {
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
	return m.schema.list.SetItems(items)
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
	for _, object := range m.schema.objects {
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
	if query := strings.TrimSpace(m.schema.filter.Value()); query != "" {
		m.schema.list.SetFilterText(query)
		return
	}
	m.schema.list.ResetFilter()
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
