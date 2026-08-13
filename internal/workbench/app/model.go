package app

import (
	"context"
	"net/url"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/core"
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/browse"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
	"github.com/l3aro/perk-workbench/internal/workbench/connection"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
	"github.com/l3aro/perk-workbench/internal/workbench/querylog"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
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
	tabDiagram     = core.TabDiagram
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
	notifications    notificationState
	overlay          overlayState
	layout           layoutState
	chat             chatState
}

// chatState owns the assistant pane's root half: the chat component
// (input, viewport, runs, tool rounds, rendering) plus the root-side
// write-confirmation overlay.
type chatState struct {
	component chat.Model
	// writeConfirmation is the root-owned dialog for an assistant
	// sql_write call awaiting approval; writePending carries the request
	// until the dialog resolves.
	writeConfirmation *confirmationDialog
	writePending      *chat.WriteRequest
}

// connectionState owns the connection screen's root half: the database
// file picker. The profile form, recent list/filter, and persisted
// profiles live in the connection component; root dispatches its messages
// and performs the database flows.
type connectionState struct {
	component connection.Model
	picker    list.Model
	pickerDir string
}

// schemaState owns the schema sidebar and the structure/index/foreign-key
// tabs: the tree list and filter, the loaded schema objects, database/schema
// expansion, the accordion animation, the structure tables and forms, and
// the relationship diagram all live in the schema component; root keeps the
// query lifecycle, the confirmations, and the dispatch.
type schemaState struct {
	component schema.Model
	// foreignKeysAll and indexesAll cache the whole-schema foreign-key and
	// index listings for the current connection. The relationship and index
	// diagrams read them through the schema snapshot for focus rings beyond
	// the selected table. Loaded on connect, refreshed whenever DDL mutates
	// the schema.
	foreignKeysAll map[string][]sharedsql.ForeignKeyInfo
	indexesAll     map[string][]sharedsql.IndexInfo
	// foreignKeysRev and indexesRev order same-connection refreshes: each
	// load stamps its revision and a stale result (an older snapshot whose
	// message arrives late) is dropped.
	foreignKeysRev uint64
	indexesRev     uint64
}

// queryState owns the SQL workspace: the editor, result table and raw
// result, completion, validation, editor history, and the query-log
// component (pane, paging, detail).
type queryState struct {
	component             querylog.Model
	path                  string
	store                 *querylog.Store
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

// browseState owns the browse tab's root half: the browse component
// (result table, editors, settings, rendering) and the cached database
// adapter. Root keeps the query lifecycle and the overlays; the component
// owns the browse state and interactions.
type browseState struct {
	component browse.Model
	backend   browse.Backend
}

// notificationState owns the notification pipeline's root half: history
// persistence through the lazy store, the resolved data path, and the
// status-popup suppression flags. The popup/detail/history state, queue,
// and rendering live in the notification component; root routes messages
// and applies its events.
type notificationState struct {
	store                   *notification.Store
	component               notification.Model
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
	deletePendingConnection *profile.Profile
	formMode                *formModeController
}

// layoutState owns terminal geometry and pointer tracking: window and
// pane dimensions, table offsets/columns, and click debounce state.
type layoutState struct {
	width, height, schemaWidth, editorWidth, chatWidth int
	workspaceHeight, queryLogHeight                    int
	editorHeight, resultsHeight, tableViewportWidth    int
	structureOffset, browseOffset, resultsOffset       int
	indexesOffset, foreignKeysOffset                   int
	structureColumn, browseColumn, resultsColumn       int
	indexesColumn, foreignKeysColumn                   int
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
			component: connection.New(),
			picker:    newList("Choose database", true),
		},
		schema: schemaState{
			component: schema.New(),
		},
		queryLog: queryState{
			component:         querylog.New(queryLogPageSize()),
			results:           newResultsTable(),
			editor:            newEditor(),
			completionColumns: map[string][]string{},
			historyIndex:      -1,
		},
		browse: browseState{
			component: browse.New(),
		},
		overlay: overlayState{
			formMode: &formModeController{},
		},
		chat: chatState{
			component: chat.New(),
		},
		keybindings: DefaultKeybindings(),
		vimMode:     vimModeEnabled(),
	}
	model.ReadOnly = readOnly || appConfig.ReadOnly
	model.browse.component.PageSize = browsePageSizeDefault()
	if appConfig.ReadOnly {
		// Pre-check the per-connection toggle so fresh forms keep the
		// configured default; the user can still opt a connection back.
		model.connection.component.Form.Values.ReadOnly = true
	}
	model.overlay.commandPalette = newCommandPalette(model)
	model.chat.component.SetContext(ctx)
	model.focusActiveTable()
	model.queryLog.path, _ = queryLogPath()
	model.notifications.path, _ = notificationPath()
	model.connection.component.Path, _ = profile.Path()
	model.configPath = ConfigPath()
	var migrated bool
	profiles, migrated := profile.Load(model.connection.component.Path)
	model.connection.component.SetProfiles(profiles)
	if migrated {
		// Best-effort: persist the assigned legacy profile IDs immediately.
		model.connection.component.Save()
	}
	// Route every logged event into the notification popup pipeline.
	log.SetNotifier(notification.EnqueueLogEntry)
	notification.SetNerdFont(appConfig.NerdFont == nil || *appConfig.NerdFont)
	schema.SetNerdFont(appConfig.NerdFont == nil || *appConfig.NerdFont)
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
	m.schema.component.Anim = nil
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
	for _, object := range m.schema.component.Objects {
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
	// original transition order; the component reset below re-clears it.
	m.schema.component.Anim = nil
	m.openTag++ // supersede any open still in flight
	m.applyLayout(m.layout.width, m.layout.height)
	m.Database = nil
	m.SelectedTable = ""
	m.WorkspaceTarget = core.WorkspaceTarget{}
	m.BrowsePage = 0
	m.setStatus("")
	m.refreshBrowseBackend()
	m.queryLog.reset()
	m.schema.component.Reset()
	m.schema.foreignKeysAll = nil
	m.schema.indexesAll = nil
	m.schema.foreignKeysRev = 0
	m.schema.indexesRev = 0
	m.databaseInfo = sharedsql.DatabaseInfo{}
	m.connectionID = ""
	m.browse.reset()
	m.connection.reset()
	m.overlay.reset()
	m.chat.component.Reset()
	m.chat.writeConfirmation = nil
	m.chat.writePending = nil
}

// reset clears query workspace state: the editor, result table, raw
// result, completion data, validation tag, editor history, and the
// query-log component, and closes the query-log store. The component is
// cleared in place so its sized table survives the disconnect layout
// pass.
func (s *queryState) reset() {
	if s.store != nil {
		_ = s.store.Close()
		s.store = nil
	}
	s.component.Reset()
	s.history = nil
	s.historyIndex = -1
	s.editor.setValue("")
	s.editorValidity = sqlValidityPending
	s.validationTag++
	s.completionColumns = map[string][]string{}
	s.completionTable = ""
	s.resultsRaw = nil
	s.results.SetRows(nil)
}

// reset clears the browse result table and result data.
func (s *browseState) reset() {
	s.component.Reset()
}

// reset reloads the persisted profile list and clears the profile list
// and filter inputs.
func (s *connectionState) reset() {
	s.component.Path, _ = profile.Path()
	profiles, _ := profile.Load(s.component.Path)
	s.component.SetProfiles(profiles)
	s.component.Recent.ResetFilter()
	s.component.RecentFilter.SetValue("")
	s.component.RecentFilter.Blur()
}

// reset closes the notification history store and clears the component's
// captured entries, popup, detail, and history state.
func (s *notificationState) reset() {
	if s.store != nil {
		_ = s.store.Close()
		s.store = nil
	}
	s.component.Reset()
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
		return m.connection.component.Form.Huh.Init()
	}
	return nil
}

func (m *Model) setSchemaObjects(objects []sharedsql.SchemaObject) tea.Cmd {
	return m.schema.component.SetObjects(objects, m.schemaSnapshot())
}

// rebuildSchemaTree rebuilds the schema sidebar from the loaded objects.
func (m *Model) rebuildSchemaTree() tea.Cmd {
	return m.schema.component.RebuildTree(m.schemaSnapshot())
}

// schemaExpansionKey identifies a schema under a database root; the
// separator cannot appear in either name.
func (m Model) schemaExpansionKey(database, schema string) string {
	return database + "\x00" + schema
}

// schemaTable returns the qualified name of a schema item for the active
// product (MySQL qualifies with the database).
func (m Model) schemaTable(item schema.Item) string {
	return m.schema.component.TableName(item, m.schemaSnapshot())
}

// schemaSnapshot builds the session snapshot root hands to the schema
// component for one update or render.
func (m Model) schemaSnapshot() schema.Snapshot {
	return schema.Snapshot{
		SelectedTable:  m.SelectedTable,
		Database:       m.databaseInfo,
		Target:         m.Target,
		ReadOnly:       m.ReadOnly,
		ForeignKeysAll: m.schema.foreignKeysAll,
		IndexesAll:     m.schema.indexesAll,
	}
}

// schemaLayout builds the layout snapshot root hands to the schema
// component for the sidebar: the pane's own width and the shared form
// viewport height.
func (m Model) schemaLayout() uikit.Layout {
	return uikit.Layout{
		Width:         m.layout.width,
		Height:        m.layout.height,
		ViewportWidth: m.layout.schemaWidth,
		PaneHeight:    m.formViewportHeight(),
	}
}

// workspaceLayout builds the layout snapshot root hands to the schema
// component for the workspace tabs: the table viewport width and the
// workspace body height.
func (m Model) workspaceLayout() uikit.Layout {
	return uikit.Layout{
		Width:         m.layout.width,
		Height:        m.layout.workspaceHeight,
		ViewportWidth: m.layout.tableViewportWidth,
		PaneHeight:    m.formViewportHeight(),
	}
}

// applySchemaEvent applies one schema component event: status transitions,
// clipboard copies, table selection, DDL execution, schema reloads,
// reconnects, delete confirmations, and context menus all stay root-owned.
func (m Model) applySchemaEvent(event schema.Event, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	switch e := event.(type) {
	case nil:
		return m, cmd
	case uikit.StatusChanged:
		m.setStatus(uikit.SafeText(e.Text))
		return m, cmd
	case uikit.ClipboardRequested:
		m.setStatus("copied to clipboard")
		if cmd == nil {
			return m, copyQueryLogStatement(e.Text)
		}
		return m, tea.Batch(cmd, copyQueryLogStatement(e.Text))
	case schema.TableSelected:
		cmd := m.selectSchemaTableBy(e.Table)
		return m, cmd
	case schema.DatabaseSelected:
		return m, tea.Batch(cmd, m.selectDatabaseTarget(e.Database))
	case schema.SchemaSelected:
		return m, tea.Batch(cmd, m.selectSchemaTarget(e.Database, e.Schema))
	case schema.QueryRequested:
		return m.startQueryStatement(e.Statement, e.ReadOnly)
	case schema.SchemaRequested:
		return m, tea.Batch(cmd, m.loadSchema())
	case schema.ReconnectRequested:
		return m.reconnectDatabase(e.Database)
	case schema.DeleteTableRequested:
		m.confirmTableDelete(e.Database, e.Table)
		return m, cmd
	case schema.ColumnDeleteRequested:
		m.overlay.deletePending = "column"
		m.overlay.deletePendingName = e.Name
		m.overlay.deleteConfirm = newConfirmationDialog("Delete column?", "", []confirmationOption{
			{Label: "Yes, delete", Action: "delete"},
			{Label: "Cancel", Action: "cancel"},
		})
		return m, cmd
	case schema.IndexDeleteRequested:
		m.overlay.deletePending = "index"
		m.overlay.deletePendingName = e.Name
		m.overlay.deleteConfirm = newConfirmationDialog("Delete index?", "", []confirmationOption{
			{Label: "Yes, delete", Action: "delete"},
			{Label: "Cancel", Action: "cancel"},
		})
		return m, cmd
	case schema.ForeignKeyDeleteRequested:
		m.overlay.deletePending = "foreign_key"
		m.overlay.deletePendingName = e.ID
		m.overlay.deleteConfirm = newConfirmationDialog("Delete foreign key?", "", []confirmationOption{
			{Label: "Yes, delete", Action: "delete"},
			{Label: "Cancel", Action: "cancel"},
		})
		return m, cmd
	case schema.ColumnFormRequested:
		cmd := m.openColumnForm()
		return m, cmd
	case schema.NewColumnFormRequested:
		cmd := m.openNewColumnForm()
		return m, cmd
	case schema.IndexFormRequested:
		if e.Selected {
			if index := m.schema.component.SelectedIndex(); index != nil {
				cmd := m.openIndexForm(index)
				return m, cmd
			}
			return m, nil
		}
		cmd := m.openIndexForm(nil)
		return m, cmd
	case schema.ForeignKeyFormRequested:
		if e.Selected {
			if foreignKey := m.schema.component.SelectedForeignKey(); foreignKey != nil {
				cmd := m.openForeignKeyForm(foreignKey)
				return m, cmd
			}
			return m, nil
		}
		cmd := m.openForeignKeyForm(nil)
		return m, cmd
	case schema.TableFormRequested:
		switch e.Kind {
		case schema.TableFormDatabase:
			cmd := m.openDatabaseForm(e.OriginalName)
			return m, cmd
		case schema.TableFormSchema:
			cmd := m.openSchemaForm(e.OriginalName)
			return m, cmd
		default:
			cmd := m.openTableForm(e.Database, e.Table)
			return m, cmd
		}
	case schema.ContextMenuRequested:
		m.openSchemaComponentMenu(e.Menu)
		return m, cmd
	}
	return m, cmd
}

// openSchemaComponentMenu maps a component-built schema menu onto the root's
// context-menu overlay.
func (m *Model) openSchemaComponentMenu(menu schema.Menu) {
	options := make([]menuOption, 0, len(menu.Options))
	for _, option := range menu.Options {
		options = append(options, menuOption{label: option.Label, action: option.Action, keys: option.Keys})
	}
	m.overlay.contextMenu = &contextMenuModel{
		options:  options,
		selected: 0,
		visible:  true,
		x:        menu.X,
		y:        menu.Y,
		database: menu.Database,
		schema:   menu.Schema,
		table:    menu.Table,
	}
}

// selectSchemaTableBy opens the given qualified table in the workflow and
// loads its structure, index, and foreign-key data.
func (m *Model) selectSchemaTableBy(table string) tea.Cmd {
	m.SelectTable(table)
	// The landing tab is configurable; SelectTable defaults to the
	// Structure (columns) tab.
	m.Tab = tableOpenTargetTab()
	m.browse.component.Settings = browse.Settings{}
	m.schema.component.Structure.Columns = nil
	m.browse.component.Structure = nil
	m.browse.component.Page = 0
	m.schema.component.Structure.ForeignKeyInfo = nil
	m.schema.component.Structure.ReferencingForeignKeyInfo = nil
	m.schema.component.Structure.RelationshipDiagram = false
	m.browse.component.Pending = true
	m.focusActiveTable()
	return tea.Batch(m.rebuildSchemaTree(), m.loadTableInfo(), m.loadIndexes(), m.loadForeignKeys(), m.loadReferencingForeignKeys(), m.loadPendingBrowse())
}

// selectSchemaTable opens the table of the given sidebar item.
func (m *Model) selectSchemaTable(item schema.Item) tea.Cmd {
	return m.selectSchemaTableBy(m.schemaTable(item))
}

// selectDatabaseTarget opens a database scope in the workflow: table-owned
// browse/structure state is cleared, the tree rebuilds without the old
// open-table path, and the workspace lands on the Browse tab. Loading is
// deferred until the active tab needs it.
func (m *Model) selectDatabaseTarget(database string) tea.Cmd {
	m.SelectDatabase(database)
	m.clearTableWorkspace()
	m.focusActiveTable()
	return m.rebuildSchemaTree()
}

// selectSchemaTarget opens a PostgreSQL schema scope, mirroring
// selectDatabaseTarget for schema targets.
func (m *Model) selectSchemaTarget(database, schema string) tea.Cmd {
	m.SelectSchema(database, schema)
	m.clearTableWorkspace()
	m.focusActiveTable()
	return m.rebuildSchemaTree()
}

// clearTableWorkspace drops table-owned browse/structure/form state so a
// database/schema scope starts from an empty object view: no row fetch is
// pending, the table tabs hold no stale structure data, and an open table
// form (sidebar selection can arrive mid-edit) closes with its input mode.
func (m *Model) clearTableWorkspace() {
	m.browse.component.Reset()
	m.browse.component.Settings = browse.Settings{}
	m.browse.component.Structure = nil
	m.browse.component.Pending = false
	m.browse.component.Form = browse.Form{}
	m.browse.component.FilterForm = nil
	m.browse.component.CellEditor = nil
	m.browse.component.DocumentEditor = nil
	m.browse.component.CellViewer = nil
	m.schema.component.Structure.Columns = nil
	m.schema.component.Structure.ForeignKeyInfo = nil
	m.schema.component.Structure.ReferencingForeignKeyInfo = nil
	m.schema.component.Structure.RelationshipDiagram = false
	m.schema.component.Structure.IndexDiagram = false
	m.schema.component.Structure.ColumnForm = schema.ColumnForm{}
	m.schema.component.Structure.IndexForm = schema.IndexForm{}
	m.schema.component.Structure.ForeignKeyForm = schema.ForeignKeyForm{}
	m.schema.component.Structure.TableForm = schema.TableForm{}
	m.schema.component.Structure.TableFormRunning = false
	m.overlay.formMode.Mode = formModeNormal
	m.overlay.formMode.ButtonsFocused = false
	m.overlay.formMode.ButtonChoice = 0
}
