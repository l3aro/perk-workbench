// Package browse owns the browse tab feature: the result table, page and
// sort/filter state, the row/document editors, the cell editor and viewer,
// the pager, and the pane rendering. The root shell keeps the query
// lifecycle (loadBrowse and the browse write flows), the confirmations and
// context menus, and the message dispatch; it routes pane-local messages
// into the component and applies the component's typed events.
package browse

import (
	"strconv"

	"charm.land/bubbles/v2/table"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// uikitBrowsePageSizeDefault is the built-in browse page size used before
// the root applies the configured default.
const uikitBrowsePageSizeDefault = 25

// Backend is the narrow database adapter the root builds from its
// capability helpers and hands to the component for one update. Optional
// interfaces are nil when the open service does not support them; the
// component preserves the current capability behavior (row writer enables
// row CRUD, a text document capability enables document insertion/editing,
// unsupported stores keep copy-only actions).
type Backend struct {
	Service       sharedsql.Service
	Capabilities  sharedsql.WriteCapabilities
	RowWriter     sharedsql.RowWriter
	DocumentRead  sharedsql.DocumentReader
	DocumentWrite sharedsql.DocumentWriter
}

// Event is the component's update result: a typed request to the root
// shell. It is an alias for any so the component can emit the shared uikit
// events (uikit.StatusChanged, uikit.ClipboardRequested) alongside its own
// browse-local events; the root type-switches the concrete type.
type Event = any

// DataChanged asks the root to reload the browse page from page 0 (the
// sort or filter settings changed).
type DataChanged struct{}

// SchemaRequested asks the root to reload the schema sidebar.
type SchemaRequested struct{}

// PageRequested asks the root to advance the browse page by Delta through
// its debounced paging command.
type PageRequested struct{ Delta int }

// Sort is one active browse sort: the column title and direction.
type Sort struct {
	Column string
	Desc   bool
}

// Settings is the browse filter/sort/limit state carried into every
// BrowseTable call.
type Settings struct {
	Filters []sharedsql.BrowseFilter
	Sorts   []Sort
	Limit   int
}

// PageSize returns the configured row limit, falling back to the given
// default when the settings carry no explicit limit.
func (s Settings) PageSize(fallback int) int {
	if s.Limit < 1 {
		return fallback
	}
	return s.Limit
}

// Model is the browse feature component: the result table, the loaded
// result and its status summary, the numeric-column alignment mask, the
// filter/sort settings, the row/document/cell editors and the cell
// viewer, and the paging state. The root reads and writes the exported
// state fields and mirrors the global page (core.Workflow.BrowsePage) in
// Page for the pager rendering.
type Model struct {
	Table          table.Model
	Result         sharedsql.Result
	Status         string
	NumericColumns []bool
	Settings       Settings
	PageSize       int
	Form           Form
	FilterForm     *FilterForm
	CellEditor     *CellEditor
	DocumentEditor *DocumentEditor
	CellViewer     *uikit.CellViewer
	Loading        bool
	Pending        bool
	PageTag        uint64
	// Page mirrors the root's global browse page for rendering (the pager
	// button enablement); the root keeps it current with BrowsePage.
	Page int
	// SelectedColumn and Offset are the selected column and horizontal
	// scroll offset of the browse table, owned here so the component can
	// move the selection and render the alignment.
	SelectedColumn int
	Offset         int
	// Structure mirrors the root's loaded table structure; the root keeps
	// it current so the component can build forms and locate primary keys.
	Structure []sharedsql.ColumnInfo
}

// New builds the browse component with a fresh results table.
func New() Model {
	return Model{
		Table:    uikit.NewResultsTable(),
		PageSize: uikitBrowsePageSizeDefault,
	}
}

// Reset clears the browse result table and result data.
func (m *Model) Reset() {
	m.Table.SetRows(nil)
	m.Result = sharedsql.Result{}
	m.Page = 0
}

// CycleSort toggles the sort on the selected column, cycling exactly like
// the s keybinding: first press sorts ascending, the second descending,
// the third drops the sort. Returns whether the settings changed (the
// caller reloads the browse page only then).
func (m *Model) CycleSort() bool {
	if m.SelectedColumn < 0 || m.SelectedColumn >= len(m.Result.Columns) {
		return false
	}
	column := m.Result.Columns[m.SelectedColumn]
	for index, sort := range m.Settings.Sorts {
		if sort.Column != column {
			continue
		}
		if !sort.Desc {
			m.Settings.Sorts[index].Desc = true
		} else {
			m.Settings.Sorts = append(m.Settings.Sorts[:index], m.Settings.Sorts[index+1:]...)
		}
		return true
	}
	m.Settings.Sorts = append(m.Settings.Sorts, Sort{Column: column})
	return true
}

// ResetFilters clears the active browse filters.
func (m *Model) ResetFilters() {
	m.Settings.Filters = nil
}

// RowValuePreview renders one tagged value for confirmation and query-log
// text: DEFAULT, NULL, or a Go-quoted scalar.
func RowValuePreview(value sharedsql.Value) string {
	switch value.Kind {
	case sharedsql.ValueDefault:
		return "DEFAULT"
	case sharedsql.ValueNull:
		return "NULL"
	default:
		return strconv.Quote(value.String)
	}
}
