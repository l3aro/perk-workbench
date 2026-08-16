// Package schema owns the schema sidebar and the structure/index/foreign-key
// tabs: the loaded schema tree with its filter and accordion animation, the
// structure tables and their filters, the column/index/foreign-key/table
// forms, and the relationship diagram. The root shell keeps the query
// lifecycle (loadSchema, selectSchemaTable, DDL execution, column/index/FK
// CRUD execution) and the confirmation/delete overlays; it routes pane-local
// messages into the component and applies the component's typed events.
package schema

import (
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// nerdFont mirrors the root's config.json "nerd_font" setting: Nerd Font
// icons are the default, geometric symbols fall back for terminals without
// a Nerd Font. The root applies it once at startup, like the notification
// component's glyph setting.
var nerdFont = true

// SetNerdFont records whether the session renders Nerd Font glyphs.
func SetNerdFont(enabled bool) {
	nerdFont = enabled
}

// Event is the component's update result: a typed request to the root
// shell. It is an alias for any so the component can emit the shared uikit
// events (uikit.StatusChanged, uikit.ClipboardRequested) alongside its own
// schema-local events; the root type-switches the concrete type.
type Event = any

// TableSelected asks the root to open the given qualified table: select it
// in the workflow and load its structure, index, and foreign-key data.
// Database and Schema are the selected item's structured identifiers,
// preserved so table-scoped workspace views receive the full structured
// target.
type TableSelected struct {
	Table    string
	Database string
	Schema   string
}

// DatabaseSelected asks the root to open the database scope of the sidebar
// root: clear the table workspace and target the database. SQLite roots
// never emit it (their workspace stays SQL-only until a table opens),
// PostgreSQL roots only when they are the connected database, and
// unknown/non-built-in products only when the driver advertises workspace
// metadata.
type DatabaseSelected struct{ Database string }

// SchemaSelected asks the root to open the PostgreSQL schema scope of the
// sidebar: clear the table workspace and target the schema.
type SchemaSelected struct {
	Database string
	Schema   string
}

// QueryRequested asks the root to run a statement through its query
// lifecycle; ReadOnly marks a schema mutation whose success refreshes the
// sidebar.
type QueryRequested struct {
	Statement string
	ReadOnly  bool
}

// SchemaRequested asks the root to reload the schema sidebar.
type SchemaRequested struct{}

// ReconnectRequested asks the root to switch the PostgreSQL session to
// another database on the same server.
type ReconnectRequested struct{ Database string }

// DeleteTableRequested asks the root to confirm dropping the given table.
type DeleteTableRequested struct {
	Database string
	Table    string
}

// ColumnDeleteRequested asks the root to confirm deleting the structure
// column at the table cursor.
type ColumnDeleteRequested struct{ Name string }

// IndexDeleteRequested asks the root to confirm deleting the index at the
// indexes table cursor.
type IndexDeleteRequested struct{ Name string }

// ForeignKeyDeleteRequested asks the root to confirm deleting the foreign
// key at the foreign-keys table cursor.
type ForeignKeyDeleteRequested struct{ ID string }

// ColumnFormRequested asks the root to open the edit form for the selected
// structure column through its form-mode wrapper.
type ColumnFormRequested struct{}

// NewColumnFormRequested asks the root to open the add-column form through
// its form-mode wrapper.
type NewColumnFormRequested struct{}

// IndexFormRequested asks the root to open the index form through its
// form-mode wrapper: Selected edits the index at the table cursor, an
// unset flag creates a new one.
type IndexFormRequested struct{ Selected bool }

// ForeignKeyFormRequested asks the root to open the foreign-key form
// through its form-mode wrapper: Selected edits the foreign key at the
// table cursor, an unset flag creates a new one.
type ForeignKeyFormRequested struct{ Selected bool }

// TableFormRequested asks the root to open the create/rename popup for a
// table, database, or schema through its form-mode wrapper.
type TableFormRequested struct {
	Kind         TableFormObjectKind
	Database     string // table create target qualifier; database for table renames
	Table        string // qualified old name for table renames
	OriginalName string // database/schema rename target
}

// MenuOption is one selectable action of a schema context menu; the root
// renders the menu and dispatches the action strings.
type MenuOption struct {
	Label  string
	Action string
	Keys   string
}

// Menu is a schema context menu the component builds; the root owns the
// overlay model and dispatch.
type Menu struct {
	Options  []MenuOption
	X, Y     int
	Database string
	Schema   string
	Table    string
}

// ContextMenuRequested asks the root to open a schema context menu.
type ContextMenuRequested struct{ Menu Menu }

// Snapshot is the root-owned session state the component reads for one
// update or render: the open table, the connected database info and target
// (for PostgreSQL/MySQL product rules), the workspace scope target, and
// the read-only flag.
type Snapshot struct {
	SelectedTable string
	Database      sharedsql.DatabaseInfo
	Target        string
	ReadOnly      bool
	// WorkspaceTarget is the workspace scope the tabs serve: none, a
	// database, a PostgreSQL schema, or a table. The scope diagram reads
	// it to filter its cards.
	WorkspaceTarget core.WorkspaceTarget
	// DatabaseScopeCapable reports that the active driver advertises
	// explicit workspace metadata, so unknown/non-built-in products may
	// serve a generic database scope. Host-owned: the root sets it from
	// its workspace advertisement, never from the product name.
	DatabaseScopeCapable bool
	// ForeignKeysAll and IndexesAll are the connection-level schema caches:
	// every foreign key and index keyed by table, loaded by the root on
	// connect and refreshed on DDL. Nil when not loaded yet; the diagrams
	// then degrade to the selected table's own data.
	ForeignKeysAll map[string][]sharedsql.ForeignKeyInfo
	IndexesAll     map[string][]sharedsql.IndexInfo
}

// MaxDiagramDepth caps the focus-ring depth of the relationship and index
// diagrams: each extra ring widens the layout, and the fit fallback covers
// the rest.
const MaxDiagramDepth = 5

// Structure owns the structure/index/foreign-key tabs: their tables, row
// metadata, filters, forms, the relationship-diagram flag, and the table
// filter input.
type Structure struct {
	Table                     table.Model
	Rows                      []table.Row
	Columns                   []sharedsql.ColumnInfo
	Filter                    string
	Indexes                   table.Model
	IndexRows                 []table.Row
	IndexInfo                 []sharedsql.IndexInfo
	IndexesFilter             string
	IndexForm                 IndexForm
	ForeignKeys               table.Model
	ForeignKeyRows            []table.Row
	ForeignKeyInfo            []sharedsql.ForeignKeyInfo
	ReferencingForeignKeyInfo []sharedsql.ReferencingForeignKeyInfo
	ForeignKeysFilter         string
	ForeignKeyForm            ForeignKeyForm
	ColumnForm                ColumnForm
	TableForm                 TableForm
	TableFormRunning          bool
	RelationshipDiagram       bool
	// IndexDiagram switches the indexes tab into its diagram mode.
	IndexDiagram bool
	// DiagramDepth is the focus-ring depth of both diagrams: 1 shows only
	// the selected table's foreign-key neighbors, each extra level widens
	// the ring one hop.
	DiagramDepth     int
	TableFiltering   bool
	TableFilterInput textinput.Model
	TableFilterTab   core.Tab
}

// Model is the schema feature component: the sidebar list and filter, the
// loaded schema objects, database/schema expansion and the accordion
// animation, plus the structure/index/foreign-key state. The root reads and
// writes the exported state fields and mirrors the workflow's selected
// table and tab through the Snapshot.
type Model struct {
	List              list.Model
	Filter            textinput.Model
	Objects           []sharedsql.SchemaObject
	ExpandedDatabases map[string]bool
	ExpandedSchemas   map[string]bool
	Anim              *Anim
	Structure         Structure
}

// New builds the schema component with a fresh tree list and structure
// tables.
func New() Model {
	return Model{
		List:              newSchemaList(),
		Filter:            uikit.NewFilterInput(),
		ExpandedDatabases: map[string]bool{},
		ExpandedSchemas:   map[string]bool{},
		Structure: Structure{
			Table:        uikit.NewResultsTable(),
			Indexes:      uikit.NewResultsTable(),
			ForeignKeys:  uikit.NewResultsTable(),
			DiagramDepth: 1,
		},
	}
}

// Reset clears the schema sidebar (loaded objects, expansion, the tree
// list, and the filter input) and the structure, index, and foreign-key
// tables.
func (m *Model) Reset() {
	m.Anim = nil
	m.Objects = nil
	m.ExpandedDatabases = map[string]bool{}
	m.ExpandedSchemas = map[string]bool{}
	m.List.SetItems(nil)
	m.List.ResetFilter()
	m.Filter.SetValue("")
	m.Filter.Blur()
	m.Structure.Table.SetRows(nil)
	m.Structure.Indexes.SetRows(nil)
	m.Structure.ForeignKeys.SetRows(nil)
	m.Structure.RelationshipDiagram = false
	m.Structure.IndexDiagram = false
	m.Structure.DiagramDepth = 1
}

// Item is one row of the schema sidebar tree: a database root, a PostgreSQL
// schema, or a table/view. Positional fields feed the list filter.
type Item struct {
	Name, Detail    string
	Database, Table string
	Schema          string
	Kind            string
	Root            bool
	Open            bool   // on the path from the root to the open table
	Count           int    // child tables/views; -1 = unknown (not rendered)
	RowCount        *int64 // estimated rows for tables; nil = unknown
}

func (i Item) FilterValue() string {
	// Positional fields joined with a unit separator: database, name,
	// schema, kind. The schema sidebar filter glob-matches the name,
	// schema, and kind fields (skipping the containing database); fuzzy
	// matching replaces the separator with a space to keep the historical
	// behavior.
	return strings.Join([]string{i.Database, i.Name, i.Schema, i.Kind}, "\x00")
}

func (i Item) Title() string       { return i.Name }
func (i Item) Description() string { return i.Detail }

// schemaFilterSeparator joins the name fields of Item.FilterValue. It
// cannot be typed into the filter input, so glob matching can rely on it to
// recover the individual database/schema/table/kind names.
const schemaFilterSeparator = "\x00"

// ListFilter is the schema sidebar's list filter; exported for the root
// tests.
func ListFilter(term string, targets []string) []list.Rank { return schemaListFilter(term, targets) }

// schemaListFilter is the schema sidebar's list filter. A term containing
// a * or ? wildcard is treated as a shell-style glob: * matches any run of
// characters, ? matches exactly one character, and everything else — % and
// _ included — is literal. Matching is case-insensitive and anchored: the
// pattern must match one of the item's own name fields (title, schema, or
// kind). The containing database is not matched, so a table under office
// is found by its own name, not by its database. Any other term keeps the
// list's default fuzzy matching unchanged.
func schemaListFilter(term string, targets []string) []list.Rank {
	if !strings.ContainsAny(term, "*?") {
		plain := make([]string, len(targets))
		for index, target := range targets {
			plain[index] = strings.TrimSpace(strings.ReplaceAll(target, schemaFilterSeparator, " "))
		}
		return list.DefaultFilter(term, plain)
	}
	pattern := regexp.MustCompile("(?i)" + sharedsql.GlobToRegex(term))
	ranks := make([]list.Rank, 0, len(targets))
	for index, target := range targets {
		fields := strings.Split(target, schemaFilterSeparator)
		for _, part := range fields[1:] { // fields[0] is the containing database
			if pattern.MatchString(part) {
				ranks = append(ranks, list.Rank{Index: index})
				break
			}
		}
	}
	return ranks
}

// SetObjects records the loaded schema objects, computes the load-time
// expansion, rebuilds the tree, and keeps the cursor on the connected
// root. It returns the tree rebuild command.
func (m *Model) SetObjects(objects []sharedsql.SchemaObject, snapshot Snapshot) tea.Cmd {
	m.Objects = objects
	m.Anim = nil // the tree changed wholesale; no accordion to continue
	if m.ExpandedDatabases == nil {
		m.ExpandedDatabases = map[string]bool{}
	}
	if m.ExpandedSchemas == nil {
		m.ExpandedSchemas = map[string]bool{}
	}
	// Default expansion mirrors the toggle rule: server products open
	// exactly one database root (the connected one, else the first) and
	// PostgreSQL exactly one schema, so a fresh tree never shows every
	// database's or schema's children at once. Single-root products
	// (SQLite, MongoDB) have nothing to collapse.
	m.ExpandedDatabases = m.initialDatabaseExpansion(objects, snapshot)
	m.ExpandedSchemas = m.initialSchemaExpansion(objects, snapshot)
	cmd := m.RebuildTree(snapshot)
	// Keep the cursor on the connected root: switching databases rebuilds
	// the tree, and with roots in stable alphabetical order the selection
	// must land where the user picked, not on the first item.
	if snapshot.Database.Product == "PostgreSQL" {
		if connected := m.connectedDatabase(snapshot); connected != "" {
			for index, item := range m.List.Items() {
				if root, ok := item.(Item); ok && root.Root && root.Database == connected {
					m.List.Select(index)
					break
				}
			}
		}
	}
	return cmd
}

// RebuildTree rebuilds the sidebar list from the loaded objects, applying
// the expansion state and the accordion reveal budget.
func (m *Model) RebuildTree(snapshot Snapshot) tea.Cmd {
	items := make([]list.Item, 0, len(m.Objects))
	schemaCounts, databaseCounts := m.schemaChildCounts(snapshot)
	animDatabase, animSchema, revealBudget := m.SchemaReveal()
	revealUsed := 0
	for _, object := range m.Objects {
		switch object.Type {
		case "database":
			description := ""
			if !m.ExpandedDatabases[object.Database] {
				description = "collapsed"
			}
			// PostgreSQL objects outside the connected database are not
			// introspected, so only its root gets a child count.
			count := -1
			if snapshot.Database.Product != "PostgreSQL" || m.databaseRootConnected(object.Database, snapshot) {
				count = databaseCounts[object.Database]
			}
			item := Item{Name: object.Name, Detail: description, Database: object.Database, Root: true, Count: count}
			item.Open = m.schemaOpenPath(item, snapshot)
			items = append(items, item)
		case "schema":
			// PostgreSQL only: schemas nest under the connected
			// database's root.
			if !m.ExpandedDatabases[object.Database] {
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
			if !m.ExpandedSchemas[m.schemaExpansionKey(object.Database, object.Name)] {
				description = "collapsed"
			}
			item := Item{Name: object.Name, Detail: description, Database: object.Database, Schema: object.Name, Kind: "schema", Count: schemaCounts[m.schemaExpansionKey(object.Database, object.Name)]}
			item.Open = m.schemaOpenPath(item, snapshot)
			items = append(items, item)
		default: // table or view
			if !m.ExpandedDatabases[object.Database] {
				if animDatabase != object.Database || animSchema != "" {
					continue
				}
			}
			table := object.Name
			schema := ""
			if snapshot.Database.Product == "PostgreSQL" {
				// The name carries schema.table; only the connected
				// database's tables are listed.
				var found bool
				schema, table, found = strings.Cut(object.Name, ".")
				if !found {
					continue
				}
				if !m.ExpandedSchemas[m.schemaExpansionKey(object.Database, schema)] {
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
			item := Item{Name: table, Detail: object.Type, Database: object.Database, Schema: schema, Table: object.Name, Kind: object.Type}
			if object.Type == "table" {
				item.RowCount = object.RowCount
			}
			item.Open = m.schemaOpenPath(item, snapshot)
			items = append(items, item)
		}
	}
	return m.List.SetItems(items)
}

// schemaExpansionKey identifies a schema under a database root; the
// separator cannot appear in either name.
func (m Model) schemaExpansionKey(database, schema string) string {
	return database + "\x00" + schema
}

// ToggleDatabase expands database when collapsed and collapses it when
// expanded; expanding one root collapses every other, so at most one
// database shows children at a time. It returns the accordion tick command.
func (m *Model) ToggleDatabase(database string, snapshot Snapshot) tea.Cmd {
	expanding := !m.ExpandedDatabases[database]
	total := m.schemaChildRowCount(database, "", expanding, snapshot)
	if m.ExpandedDatabases[database] {
		m.ExpandedDatabases[database] = false
	} else {
		for db := range m.ExpandedDatabases {
			m.ExpandedDatabases[db] = false
		}
		m.ExpandedDatabases[database] = true
	}
	return m.startTreeAnim(database, "", expanding, total)
}

// ToggleSchema expands schema when collapsed and collapses it when
// expanded; expanding one schema collapses every other, so at most one
// schema shows its tables at a time. It returns the accordion tick command.
func (m *Model) ToggleSchema(database, schema string, snapshot Snapshot) tea.Cmd {
	key := m.schemaExpansionKey(database, schema)
	expanding := !m.ExpandedSchemas[key]
	total := m.schemaChildRowCount(database, schema, expanding, snapshot)
	if m.ExpandedSchemas[key] {
		m.ExpandedSchemas[key] = false
	} else {
		for k := range m.ExpandedSchemas {
			m.ExpandedSchemas[k] = false
		}
		m.ExpandedSchemas[key] = true
	}
	return m.startTreeAnim(database, schema, expanding, total)
}

// initialDatabaseExpansion returns the load-time database expansion: the
// root holding the open table, else the connected database, else the
// first root. Server products expand exactly one root so a fresh session
// never shows every database's tables at once; single-root products
// expand everything (their only root).
func (m Model) initialDatabaseExpansion(objects []sharedsql.SchemaObject, snapshot Snapshot) map[string]bool {
	expanded := map[string]bool{}
	if snapshot.Database.Product != "MySQL" && snapshot.Database.Product != "PostgreSQL" {
		for _, object := range objects {
			if object.Type == "database" {
				expanded[object.Database] = true
			}
		}
		return expanded
	}
	preferred := m.preferredDatabase(snapshot)
	if snapshot.SelectedTable != "" {
		switch snapshot.Database.Product {
		case "MySQL":
			// MySQL qualifies tables as database.table.
			if database, _, ok := strings.Cut(snapshot.SelectedTable, "."); ok {
				preferred = database
			}
		case "PostgreSQL":
			if connected := m.connectedDatabase(snapshot); connected != "" {
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
func (m Model) preferredDatabase(snapshot Snapshot) string {
	switch snapshot.Database.Product {
	case "PostgreSQL":
		return m.connectedDatabase(snapshot)
	case "MySQL":
		return m.mysqlDatabase(snapshot)
	}
	return ""
}

// connectedDatabase returns the PostgreSQL database the session is
// connected to, taken from the target URL's (unescaped) path.
func (m Model) connectedDatabase(snapshot Snapshot) string {
	u, err := url.Parse(strings.TrimSpace(snapshot.Target))
	if err != nil || u.Scheme != "postgres" || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// mysqlDatabase extracts the database name from the MySQL DSN target.
func (m Model) mysqlDatabase(snapshot Snapshot) string {
	dsn, ok := strings.CutPrefix(snapshot.Target, "mysql:")
	if !ok {
		return ""
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return ""
	}
	return config.DBName
}

// databaseRootConnected reports whether database is the connected
// PostgreSQL database. The target URL's database name is authoritative;
// schema children are only a fallback when the target carries no path.
func (m Model) databaseRootConnected(database string, snapshot Snapshot) bool {
	if connected := m.connectedDatabase(snapshot); connected != "" {
		return connected == database
	}
	for _, object := range m.Objects {
		if object.Type == "schema" && object.Database == database {
			return true
		}
	}
	return false
}

// initialSchemaExpansion returns the load-time PostgreSQL schema
// expansion: the schema holding the open table, else the first schema of
// the expanded database. Exactly one schema is expanded so a fresh
// session never shows every schema's tables at once.
func (m Model) initialSchemaExpansion(objects []sharedsql.SchemaObject, snapshot Snapshot) map[string]bool {
	expanded := map[string]bool{}
	preferred := ""
	if snapshot.SelectedTable != "" {
		// PostgreSQL qualifies tables as schema.table.
		preferred, _, _ = strings.Cut(snapshot.SelectedTable, ".")
	}
	for _, object := range objects {
		if object.Type == "schema" && object.Name == preferred {
			expanded[m.schemaExpansionKey(object.Database, object.Name)] = true
			return expanded
		}
	}
	for _, object := range objects {
		if object.Type == "schema" && m.ExpandedDatabases[object.Database] {
			expanded[m.schemaExpansionKey(object.Database, object.Name)] = true
			break
		}
	}
	return expanded
}

// schemaOpenPath reports whether item lies on the path from its root down to
// the currently open table, which is the sidebar's "opened" state.
func (m Model) schemaOpenPath(item Item, snapshot Snapshot) bool {
	if snapshot.SelectedTable == "" {
		return false
	}
	switch snapshot.Database.Product {
	case "PostgreSQL":
		if !m.databaseRootConnected(item.Database, snapshot) {
			return false
		}
		schema, _, _ := strings.Cut(snapshot.SelectedTable, ".")
		switch {
		case item.Root:
			return true
		case item.Kind == "schema":
			return item.Schema == schema
		case item.Table != "":
			return item.Table == snapshot.SelectedTable
		}
	case "MySQL":
		database, table, _ := strings.Cut(snapshot.SelectedTable, ".")
		switch {
		case item.Root:
			return item.Database == database
		case item.Table != "":
			return item.Database == database && item.Table == table
		}
	default: // SQLite: a single root per file, tables are bare names.
		switch {
		case item.Root:
			return true
		case item.Table != "":
			return item.Table == snapshot.SelectedTable
		}
	}
	return false
}

// schemaChildCounts tallies the table/view objects under each database and
// schema so expander rows can show child counts from data already loaded.
func (m Model) schemaChildCounts(snapshot Snapshot) (schemaCounts, databaseCounts map[string]int) {
	schemaCounts = map[string]int{}
	databaseCounts = map[string]int{}
	for _, object := range m.Objects {
		switch object.Type {
		case "table", "view", "collection":
			databaseCounts[object.Database]++
			if snapshot.Database.Product == "PostgreSQL" {
				if schema, _, ok := strings.Cut(object.Name, "."); ok {
					schemaCounts[m.schemaExpansionKey(object.Database, schema)]++
				}
			}
		}
	}
	return schemaCounts, databaseCounts
}

// ApplyFilter pushes the visible filter input's value into the schema
// list, which filters its items and reports the committed state the status
// line mirrors.
func (m *Model) ApplyFilter() {
	if query := strings.TrimSpace(m.Filter.Value()); query != "" {
		m.List.SetFilterText(query)
		return
	}
	m.List.ResetFilter()
}

// TableName returns the qualified name of a schema item: database.table
// for MySQL (whose sidebar tables are bare names under database roots),
// the item's own table name otherwise (SQLite bare names, PostgreSQL
// schema.table).
func (m Model) TableName(item Item, snapshot Snapshot) string {
	table := item.Table
	if table == "" {
		table = item.Name
	}
	if snapshot.Database.Product == "MySQL" {
		return item.Database + "." + table
	}
	return table
}

// SupportsCreateDatabase reports whether the connected product can create
// databases: MySQL and PostgreSQL only.
func (m Model) SupportsCreateDatabase(snapshot Snapshot) bool {
	return snapshot.Database.Product == "MySQL" || snapshot.Database.Product == "PostgreSQL"
}

// SupportsSchemas reports whether the connected product nests tables under
// schemas (PostgreSQL only).
func (m Model) SupportsSchemas(snapshot Snapshot) bool {
	return snapshot.Database.Product == "PostgreSQL"
}

// AddTarget returns the qualifier for a new table next to the selected
// item: the database for SQLite/MySQL, the schema for PostgreSQL (whose
// sidebar groups tables under database roots).
func (m Model) AddTarget(item Item, snapshot Snapshot) (string, bool) {
	if snapshot.Database.Product == "PostgreSQL" {
		switch item.Kind {
		case "schema":
			return item.Schema, true
		case "table":
			schema, _, found := strings.Cut(item.Table, ".")
			return schema, found
		}
		return "", false
	}
	if item.Root || item.Kind == "table" {
		return item.Database, true
	}
	return "", false
}

// FilterShown reports whether the pane is wide enough for the filter box
// (3 rows: top border, input, bottom border).
func (m Model) FilterShown(layout uikit.Layout) bool {
	return layout.ViewportWidth >= 7
}

// ItemOffset returns the content Y of the first schema item line: the
// pane title row plus the 3-row filter box (when shown). The list's status
// bar is hidden, so nothing else precedes the items.
func (m Model) ItemOffset(layout uikit.Layout) int {
	if m.FilterShown(layout) {
		return 4
	}
	return 1
}

// RowY returns the screen Y of the schema list item at the given index,
// clamped to the visible window.
func (m Model) RowY(index int, layout uikit.Layout) int {
	itemOffset := m.ItemOffset(layout)
	items := m.List.VisibleItems()
	start, _ := m.List.Paginator.GetSliceBounds(len(items))
	row := itemOffset + (index - start)
	return max(row, itemOffset)
}

// ItemAt maps a schema-pane Y coordinate to its item, using the same
// visible/filter/pagination mapping as the rendered sidebar, and selects
// it.
func (m *Model) ItemAt(contentY int, layout uikit.Layout) (Item, bool) {
	// contentY = terminal Y - 1 (after header).
	itemOffset := m.ItemOffset(layout)
	itemY := contentY - itemOffset
	if itemY < 0 {
		return Item{}, false
	}
	items := m.List.VisibleItems()
	if len(items) == 0 {
		return Item{}, false
	}
	start, end := m.List.Paginator.GetSliceBounds(len(items))
	if itemY >= end-start {
		return Item{}, false
	}
	m.List.Select(start + itemY)
	item, ok := m.List.SelectedItem().(Item)
	return item, ok
}

// ItemMenu builds the schema context menu for the given item: each tree
// level offers its sibling, child, edit, and delete operations. SQLite
// keeps the table-only menu; views expose no menu. ok is false when the
// item has no menu.
func (m Model) ItemMenu(item Item, x, y int, snapshot Snapshot) (Menu, bool) {
	switch {
	case item.Kind == "schema":
		// database carries the schema-qualified Add table target (the
		// table form uses it as the PostgreSQL schema); schema carries
		// the same value for the schema-level actions.
		return Menu{
			Options: []MenuOption{
				{Label: "Add new schema", Action: "create_schema", Keys: "A"},
				{Label: "Add new table", Action: "add_table", Keys: "a"},
				{Label: "Edit schema", Action: "rename_schema", Keys: "r"},
				{Label: "Delete schema", Action: "delete_schema", Keys: "d"},
			},
			X: x, Y: y, Database: item.Schema, Schema: item.Schema,
		}, true
	case item.Root:
		switch {
		case snapshot.Database.Product == "PostgreSQL":
			// The connected database cannot be renamed or dropped in
			// place; a root that is not the connected database offers
			// Connect to switch to it and full database operations.
			options := []MenuOption{
				{Label: "Add new database", Action: "create_database", Keys: "A"},
				{Label: "Add new schema", Action: "create_schema", Keys: "a"},
			}
			if !m.databaseRootConnected(item.Database, snapshot) {
				options = []MenuOption{
					{Label: "Connect", Action: "connect_database", Keys: "enter"},
					{Label: "Add new database", Action: "create_database", Keys: "A"},
					{Label: "Edit database", Action: "rename_database", Keys: "r"},
					{Label: "Delete database", Action: "delete_database", Keys: "d"},
				}
			}
			return Menu{Options: options, X: x, Y: y, Database: item.Database}, true
		case m.SupportsCreateDatabase(snapshot):
			// MySQL treats database and schema as one level: sibling
			// database actions plus the table child. Database rename
			// has no safe DDL, so Edit database is not offered.
			return Menu{
				Options: []MenuOption{
					{Label: "Add new database", Action: "create_database", Keys: "A"},
					{Label: "Add new table", Action: "add_table", Keys: "a"},
					{Label: "Delete database", Action: "delete_database", Keys: "d"},
				},
				X: x, Y: y, Database: item.Database,
			}, true
		default:
			return Menu{
				Options:  []MenuOption{{Label: "Add table", Action: "add_table", Keys: "a"}},
				X:        x,
				Y:        y,
				Database: item.Database,
			}, true
		}
	case item.Kind == "table":
		// Server products add the same-level Add new table; SQLite keeps
		// its table-only menu.
		options := []MenuOption{
			{Label: "Rename table", Action: "rename_table", Keys: "r"},
			{Label: "Delete table", Action: "delete_table", Keys: "d"},
		}
		menuDatabase := item.Database
		if snapshot.Database.Product == "MySQL" || snapshot.Database.Product == "PostgreSQL" {
			options = []MenuOption{
				{Label: "Add new table", Action: "add_table", Keys: "a"},
				{Label: "Edit table", Action: "rename_table", Keys: "r"},
				{Label: "Delete table", Action: "delete_table", Keys: "d"},
			}
			if target, ok := m.AddTarget(item, snapshot); ok {
				// PostgreSQL creates tables inside the table's schema,
				// so the Add new table target is the schema.
				menuDatabase = target
			}
		}
		return Menu{Options: options, X: x, Y: y, Database: menuDatabase, Table: item.Table}, true
	}
	return Menu{}, false
}

// BlankMenu is the blank-sidebar context menu: creating a database is
// valid on any server product and needs no selection.
func (m Model) BlankMenu(x, y int) Menu {
	return Menu{
		Options: []MenuOption{{Label: "Add new database", Action: "create_database", Keys: "A"}},
		X:       x,
		Y:       y,
	}
}

// HandleSchemaRightClick maps a right-click on the schema sidebar to its
// context menu: the clicked item is selected first so the menu actions act
// on it, blank space on a server product offers creating a database.
func (m Model) HandleSchemaRightClick(absX, absY int, layout uikit.Layout, snapshot Snapshot) (Model, Menu, bool) {
	contentY := absY - 1
	if contentY < 0 {
		return m, Menu{}, false
	}
	if m.FilterShown(layout) && contentY >= 1 && contentY <= 3 {
		// The filter box is not blank sidebar space.
		return m, Menu{}, false
	}
	item, ok := m.ItemAt(contentY, layout)
	if !ok {
		// Blank sidebar space on server products offers creating a
		// database; the keyboard path shares this helper.
		if m.SupportsCreateDatabase(snapshot) {
			return m, m.BlankMenu(absX, absY+1), true
		}
		item, ok = m.List.SelectedItem().(Item)
		if !ok || item.Database == "" {
			return m, Menu{}, false
		}
		item = Item{Database: item.Database, Root: true}
	}
	menu, ok := m.ItemMenu(item, absX, absY+1, snapshot)
	return m, menu, ok
}

// schemaItemDelegate renders each tree level with its own marker (database,
// schema, table/view); state is conveyed by color: muted when idle,
// secondary on the open table's path, primary when selected.
// ItemDelegate renders the schema tree rows; exported for the root tests.
type ItemDelegate = schemaItemDelegate

type schemaItemDelegate struct{}

func (schemaItemDelegate) Height() int                         { return 1 }
func (schemaItemDelegate) Spacing() int                        { return 0 }
func (schemaItemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// treeMarkers returns the node marker for each tree level: database, schema,
// table/view. Nerd Font icons are the default; the root's config.json
// "nerd_font": false falls back to geometric symbols for terminals without
// a Nerd Font.
func treeMarkers() (database, schema, table string) {
	if nerdFont {
		return "\uf1c0", "\uf07b", "\uf0ce" // nf-fa-database, nf-fa-folder, nf-fa-table
	}
	return "▣", "▤", "▪"
}

func (schemaItemDelegate) Render(writer io.Writer, model list.Model, index int, item list.Item) {
	schema, ok := item.(Item)
	if !ok {
		return
	}
	dbMarker, schemaMarker, tableMarker := treeMarkers()
	marker, indent := tableMarker, ""
	switch {
	case schema.Root:
		marker = dbMarker
	case schema.Kind == "schema":
		marker, indent = schemaMarker, "  "
	case schema.Kind == "view", schema.Table != "":
		if schema.Schema != "" {
			indent = "    "
		} else {
			indent = "  "
		}
	}
	label := schema.Title()
	if schema.Root || schema.Kind == "schema" {
		if schema.Count >= 0 {
			label += fmt.Sprintf(" (%d)", schema.Count)
		}
	}
	if schema.Kind == "view" {
		label += " (view)"
	}
	// Estimated row counts (PostgreSQL reltuples, MySQL table_rows) show as
	// a badge.
	if schema.RowCount != nil {
		label += " (" + AbbreviateCount(*schema.RowCount) + ")"
	}
	color := uikit.ColorMuted // idle
	if schema.Open {
		color = uikit.ColorSecondary
	}
	if index == model.Index() {
		color = uikit.ColorPrimary
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	// The marker renders bold so the icon reads larger than the regular
	// label; terminal cells are fixed size, so weight is the only scaling
	// that keeps the layout intact.
	prefix := indent + style.Bold(true).Render(marker+" ")
	// Parenthetical badges (row counts, view marker) pin to the right edge
	// of the sidebar; the name truncates with an ellipsis so a long name
	// never wraps or overlaps the badge.
	name, badge, hasBadge := strings.Cut(label, " (")
	if hasBadge {
		badge = " (" + badge
		if lipgloss.Width(badge) >= model.Width()-lipgloss.Width(prefix) {
			badge, hasBadge = "", false // sidebar too narrow for the badge
		}
	}
	if limit := model.Width() - lipgloss.Width(prefix) - lipgloss.Width(badge); lipgloss.Width(name) > limit {
		name = ansi.Truncate(name, max(limit, 0), "…")
	}
	line := prefix + style.Render(name)
	if hasBadge {
		line += strings.Repeat(" ", model.Width()-lipgloss.Width(line)-lipgloss.Width(badge)) + style.Render(badge)
	}
	fmt.Fprint(writer, line)
}

// NewSchemaList builds a fresh schema sidebar list.
func NewSchemaList() list.Model { return newSchemaList() }

func newSchemaList() list.Model {
	model := newList("", true)
	model.SetShowTitle(false)
	model.SetShowFilter(false)
	model.SetShowStatusBar(false)
	// The sidebar renders its own persistent filter input; the list's
	// built-in filter bar and keybinding are unused.
	model.KeyMap.Filter = key.NewBinding(key.WithDisabled())
	model.Filter = schemaListFilter
	model.SetDelegate(schemaItemDelegate{})
	return model
}

func newList(title string, filtering bool) list.Model {
	delegate := newListDelegate()
	model := list.New([]list.Item{}, delegate, 0, 0)
	applyListTheme(&model)
	model.Title = title
	model.SetFilteringEnabled(filtering)
	model.SetShowPagination(false)
	model.SetShowHelp(false)
	model.DisableQuitKeybindings()
	return model
}

func newListDelegate() list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(lipgloss.Color(uikit.ColorInk))
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(lipgloss.Color(uikit.ColorMuted))
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color(uikit.ColorPrimary))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color(uikit.ColorPrimary))
	return delegate
}

func applyListTheme(model *list.Model) {
	model.Styles.Title = uikit.HeaderStyle
	model.Styles.NoItems = uikit.StatusStyle
}

// abbreviateCount renders a compact human-readable count: 10k, 490k,
// 1.23M; up to two decimals with trailing zeros trimmed, raw below 1k.
func AbbreviateCount(n int64) string {
	trim := func(s string) string { return strings.TrimRight(strings.TrimRight(s, "0"), ".") }
	switch {
	case n >= 1_000_000_000_000:
		return trim(fmt.Sprintf("%.2f", float64(n)/1_000_000_000_000)) + "T"
	case n >= 1_000_000_000:
		return trim(fmt.Sprintf("%.2f", float64(n)/1_000_000_000)) + "B"
	case n >= 1_000_000:
		return trim(fmt.Sprintf("%.2f", float64(n)/1_000_000)) + "M"
	case n >= 1_000:
		return trim(fmt.Sprintf("%.2f", float64(n)/1_000)) + "k"
	default:
		return strconv.FormatInt(n, 10)
	}
}
