package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/core"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// stubWorkspaceViewService is a fake service answering custom view
// requests from canned results; requests, delays, and failures are
// configurable so tests can prove lazy loading, staleness, and error
// rendering.
type stubWorkspaceViewService struct {
	*stubService
	results map[string]sharedsql.Result
	fail    map[string]error
	delay   time.Duration
	calls   *[]sharedsql.WorkspaceViewRequest
}

func (s *stubWorkspaceViewService) WorkspaceView(ctx context.Context, request sharedsql.WorkspaceViewRequest) (sharedsql.Result, error) {
	if s.calls != nil {
		*s.calls = append(*s.calls, request)
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return sharedsql.Result{}, ctx.Err()
		}
	}
	if err := s.fail[request.ViewID]; err != nil {
		return sharedsql.Result{}, err
	}
	return s.results[request.ViewID], nil
}

var _ sharedsql.WorkspaceViewProvider = (*stubWorkspaceViewService)(nil)

func keysResult(rows ...string) sharedsql.Result {
	result := sharedsql.Result{
		Columns:     []string{"key", "ttl"},
		ColumnTypes: []string{"string", "integer"},
		Duration:    time.Millisecond,
	}
	for _, row := range rows {
		value := row
		result.Rows = append(result.Rows, []*string{&value, nil})
		result.UntruncatedRows = append(result.UntruncatedRows, []*string{&value, nil})
	}
	return result
}

// workspaceViewModel opens a connection whose driver advertises the
// given workspace capability and serves views through service.
func workspaceViewModel(t *testing.T, service sharedsql.Service, workspace *sharedsql.WorkspaceCapability) Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("pluginkv:svc", context.Background(), func(_ context.Context, _ string, _ string) (sharedsql.Opened, error) {
		return sharedsql.Opened{
			Target:        "pluginkv:svc",
			Service:       service,
			Info:          sharedsql.DatabaseInfo{Product: "PluginKV", Version: "1"},
			Objects:       []sharedsql.SchemaObject{{Database: "pluginkv", Type: "database", Name: "pluginkv"}, {Database: "pluginkv", Type: "table", Name: "widgets"}},
			QueryLanguage: sharedsql.SQLQueryLanguage,
			Workspace:     workspace,
		}, nil
	}, false)
	model.queryLog.path = t.TempDir() + "/data.db"
	model.queryLog.component.Entries = nil
	updated, _ := model.Update(model.Init()())
	return resizeModel(updated.(Model), 100, 26)
}

// TestWorkspaceTabs_absentMetadataKeepsLegacyPolicy: a connection whose
// driver advertises no workspace metadata renders the legacy per-product
// tab policy exactly, including an unknown product with no scope tabs
// beyond Query.
func TestWorkspaceTabs_absentMetadataKeepsLegacyPolicy(t *testing.T) {
	model := workspaceViewModel(t, &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}, nil)
	model.SelectTable("widgets")
	labels, _ := model.workspaceTabMeta(model.workspaceTabs())
	if got := strings.Join(labels, ","); got != "SQL,Browse,Columns,Indexes,Foreign Keys" {
		t.Fatalf("legacy table tab labels = %q, want the full table set", got)
	}
	model.SelectDatabase("pluginkv")
	labels, _ = model.workspaceTabMeta(model.workspaceTabs())
	if got := strings.Join(labels, ","); got != "SQL" {
		t.Fatalf("legacy database tab labels = %q, want SQL for an unknown product", got)
	}
}

// TestWorkspaceTabs_standardTabFiltering: a present advertisement
// filters the target-kind standard tabs by the explicit standard-tab
// capability — solving non-relational backends that do not support
// foreign keys — and the product name never gates Diagram.
func TestWorkspaceTabs_standardTabFiltering(t *testing.T) {
	for _, test := range []struct {
		name     string
		target   func(Model) Model
		standard []sharedsql.StandardWorkspaceTab
		want     string
	}{
		{
			name:     "non-relational table",
			target:   func(m Model) Model { m.SelectTable("widgets"); return m },
			standard: []sharedsql.StandardWorkspaceTab{sharedsql.StandardWorkspaceTabColumns, sharedsql.StandardWorkspaceTabIndexes},
			want:     "SQL,Browse,Columns,Indexes",
		},
		{
			name:     "no standard tabs",
			target:   func(m Model) Model { m.SelectTable("widgets"); return m },
			standard: nil,
			want:     "SQL,Browse",
		},
		{
			name:     "unknown product database with diagram",
			target:   func(m Model) Model { m.SelectDatabase("pluginkv"); return m },
			standard: []sharedsql.StandardWorkspaceTab{sharedsql.StandardWorkspaceTabDiagram},
			want:     "SQL,Browse,Diagram",
		},
		{
			name:     "schema scope with diagram",
			target:   func(m Model) Model { m.SelectSchema("pluginkv", "public"); return m },
			standard: []sharedsql.StandardWorkspaceTab{sharedsql.StandardWorkspaceTabDiagram},
			want:     "SQL,Browse,Diagram",
		},
		{
			name:     "advertised columns do not leak into the database scope",
			target:   func(m Model) Model { m.SelectDatabase("pluginkv"); return m },
			standard: []sharedsql.StandardWorkspaceTab{sharedsql.StandardWorkspaceTabColumns},
			want:     "SQL,Browse",
		},
		{
			name:     "empty advertisement filters everything",
			target:   func(m Model) Model { m.SelectTable("widgets"); return m },
			standard: []sharedsql.StandardWorkspaceTab{},
			want:     "SQL,Browse",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := &sharedsql.WorkspaceCapability{StandardTabs: test.standard}
			model := workspaceViewModel(t, &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}, workspace)
			model = test.target(model)
			labels, _ := model.workspaceTabMeta(model.workspaceTabs())
			if got := strings.Join(labels, ","); got != test.want {
				t.Fatalf("tab labels = %q, want %q", got, test.want)
			}
		})
	}
}

// TestWorkspaceTabs_customViewScopeMatching: custom views appear after
// the standard tabs in advertised order, filtered by their scopes
// against the active target kind; a view that does not serve the scope
// is hidden, and multiple views serve the same scope.
func TestWorkspaceTabs_customViewScopeMatching(t *testing.T) {
	workspace := &sharedsql.WorkspaceCapability{
		StandardTabs: []sharedsql.StandardWorkspaceTab{
			sharedsql.StandardWorkspaceTabColumns, sharedsql.StandardWorkspaceTabIndexes,
			sharedsql.StandardWorkspaceTabForeignKeys, sharedsql.StandardWorkspaceTabDiagram,
		},
		CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "db-overview", Label: "Overview", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewDatabase}},
			{ID: "key-info", Label: "Key Info", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
			{ID: "wide", Label: "Wide", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewDatabase, sharedsql.WorkspaceViewSchema}},
		},
	}
	model := workspaceViewModel(t, &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}, workspace)

	model.SelectTable("widgets")
	labels, _ := model.workspaceTabMeta(model.workspaceTabs())
	if got := strings.Join(labels, ","); got != "SQL,Browse,Columns,Indexes,Foreign Keys,Key Info" {
		t.Fatalf("table tab labels = %q, want only the table-scoped view", got)
	}

	model.SelectDatabase("pluginkv")
	labels, _ = model.workspaceTabMeta(model.workspaceTabs())
	if got := strings.Join(labels, ","); got != "SQL,Browse,Diagram,Overview,Wide" {
		t.Fatalf("database tab labels = %q, want the database-scoped views in order", got)
	}

	model.SelectSchema("pluginkv", "public")
	labels, _ = model.workspaceTabMeta(model.workspaceTabs())
	if got := strings.Join(labels, ","); got != "SQL,Browse,Diagram,Wide" {
		t.Fatalf("schema tab labels = %q, want only the schema-scoped view", got)
	}
}

// TestWorkspaceTabs_customViewHLCycling: H/L cycle through the rendered
// row including custom views, wrapping at both ends.
func TestWorkspaceTabs_customViewHLCycling(t *testing.T) {
	workspace := &sharedsql.WorkspaceCapability{
		StandardTabs: []sharedsql.StandardWorkspaceTab{
			sharedsql.StandardWorkspaceTabColumns, sharedsql.StandardWorkspaceTabIndexes,
			sharedsql.StandardWorkspaceTabForeignKeys,
		},
		CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "one", Label: "One", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
			{ID: "two", Label: "Two", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		},
	}
	service := &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}
	model := workspaceViewModel(t, service, workspace)
	model.SelectTable("widgets")
	model.Focus = focusWorkspace
	tabs := model.workspaceTabs()
	for index, tab := range tabs {
		if tab.custom.ID != "" {
			model.Tab, model.workspace.active = tabCustom, tab.custom.ID
		} else {
			model.Tab, model.workspace.active = tab.standard, ""
		}
		updated, _ := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
		if !updated.(Model).workspaceTabActive(tabs[(index+1)%len(tabs)]) {
			t.Fatalf("L from %v did not wrap to %v", tab, tabs[(index+1)%len(tabs)])
		}
		if tab.custom.ID != "" {
			model.Tab, model.workspace.active = tabCustom, tab.custom.ID
		} else {
			model.Tab, model.workspace.active = tab.standard, ""
		}
		updated, _ = model.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
		if !updated.(Model).workspaceTabActive(tabs[(index+len(tabs)-1)%len(tabs)]) {
			t.Fatalf("H from %v did not wrap to %v", tab, tabs[(index+len(tabs)-1)%len(tabs)])
		}
	}
}

// openCustomViewFromFK presses L from the last standard tab of the
// rendered row, landing on the first custom view regardless of the
// advertised standard-tab filter.
func openCustomViewFromFK(model Model) (tea.Model, tea.Cmd) {
	start := workspaceTabItem{}
	for _, tab := range model.workspaceTabs() {
		if tab.custom.ID == "" {
			start = tab
		}
	}
	model.Tab, model.workspace.active = start.standard, ""
	return model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
}

// TestWorkspaceView_lazyLoadAndRender: clicking a custom tab loads it
// lazily with the active structured target — the loading state renders
// first, then the table with its status line. The request carries the
// view id and the active table target.
func TestWorkspaceView_lazyLoadAndRender(t *testing.T) {
	var calls []sharedsql.WorkspaceViewRequest
	service := &stubWorkspaceViewService{
		results: map[string]sharedsql.Result{"key-info": keysResult("user:2", "user:3")},
		calls:   &calls,
	}
	workspace := &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
		{ID: "key-info", Label: "Key Info", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
	}}
	model := workspaceViewModel(t, service, workspace)
	model.SelectTable("widgets")
	model.Focus = focusWorkspace
	model = resizeModel(model, 80, 24)
	if !model.layout.compact {
		t.Fatal("test setup did not produce the compact layout")
	}
	if len(calls) != 0 {
		t.Fatalf("views loaded before the tab was opened: %d calls", len(calls))
	}

	tabs := model.workspaceTabs()
	var item workspaceTabItem
	widths := []int{}
	for _, tab := range tabs {
		if tab.custom.ID == "key-info" {
			item = tab
		}
		widths = append(widths, lipgloss.Width(statusStyle.Render(model.workspaceTabLabel(tab))))
	}
	if item.custom.ID == "" {
		t.Fatal("custom tab missing from the tab row")
	}
	tabY := renderedRowY(t, model, "Key Info")
	cx := 2 // pane left border (1) + left padding (1)
	for i, tab := range tabs {
		if sameWorkspaceTab(tab, item) {
			cx += widths[i] / 2
			break
		}
		cx += widths[i]
	}
	updated, command := model.Update(tea.MouseClickMsg{X: cx, Y: tabY, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.workspaceTabActive(item) {
		t.Fatalf("click did not activate the custom tab: tab %v active %q", model.Tab, model.workspace.active)
	}
	if !model.workspace.loading {
		t.Fatal("custom tab did not enter the loading state")
	}
	if view := ansi.Strip(model.workspaceView()); !strings.Contains(view, "loading…") {
		t.Fatalf("loading state = %q, want the loading indicator", view)
	}
	if command == nil {
		t.Fatal("custom tab click returned no load command")
	}

	model = runTableCommand(model, command)
	if model.workspace.loading {
		t.Fatal("view still loading after the reply")
	}
	if len(calls) != 1 {
		t.Fatalf("view requests = %#v, want exactly one", calls)
	}
	want := sharedsql.WorkspaceViewRequest{
		ViewID: "key-info",
		Target: sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewTable, Table: "widgets"},
	}
	if calls[0] != want {
		t.Fatalf("view request = %#v, want %#v", calls[0], want)
	}
	view := ansi.Strip(model.workspaceView())
	if !strings.Contains(view, "user:2") || !strings.Contains(view, "user:3") || !strings.Contains(view, "2 rows") {
		t.Fatalf("success state = %q, want the rows and the status line", view)
	}
}

// TestWorkspaceView_states: the loading, error, empty, and success
// states render distinctly, and a failed load keeps the view in the
// error state with no table.
func TestWorkspaceView_states(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		service := &stubWorkspaceViewService{
			results: map[string]sharedsql.Result{"broken": keysResult("x")},
			fail:    map[string]error{"broken": fmt.Errorf("view exploded")},
		}
		model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "broken", Label: "Broken", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		}})
		model.SelectTable("widgets")
		model.Focus = focusWorkspace
		updated, command := openCustomViewFromFK(model)
		model = updated.(Model)
		model = runTableCommand(model, command)
		view := ansi.Strip(model.workspaceView())
		if !strings.Contains(view, "view unavailable") || !strings.Contains(view, "view exploded") {
			t.Fatalf("error state = %q, want the error text", view)
		}
	})
	t.Run("empty", func(t *testing.T) {
		service := &stubWorkspaceViewService{
			results: map[string]sharedsql.Result{"empty": {Columns: []string{}}},
		}
		model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "empty", Label: "Empty", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		}})
		model.SelectTable("widgets")
		model.Focus = focusWorkspace
		updated, command := openCustomViewFromFK(model)
		model = updated.(Model)
		model = runTableCommand(model, command)
		if view := ansi.Strip(model.workspaceView()); !strings.Contains(view, "no data") {
			t.Fatalf("empty state = %q, want the no-data indicator", view)
		}
	})
	t.Run("zero rows is success", func(t *testing.T) {
		service := &stubWorkspaceViewService{
			results: map[string]sharedsql.Result{"none": {Columns: []string{"key"}, ColumnTypes: []string{"string"}}},
		}
		model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "none", Label: "None", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		}})
		model.SelectTable("widgets")
		model.Focus = focusWorkspace
		updated, command := openCustomViewFromFK(model)
		model = updated.(Model)
		model = runTableCommand(model, command)
		view := ansi.Strip(model.workspaceView())
		if !strings.Contains(view, "0 rows") {
			t.Fatalf("success state = %q, want the 0-rows status", view)
		}
	})
}

// TestWorkspaceView_navigationAndReload: j/k move the row cursor, h/l
// move the column cursor and pan the viewport, and r reloads the view
// with a fresh request.
func TestWorkspaceView_navigationAndReload(t *testing.T) {
	var calls []sharedsql.WorkspaceViewRequest
	service := &stubWorkspaceViewService{
		results: map[string]sharedsql.Result{"keys": keysResult("a", "b", "c")},
		calls:   &calls,
	}
	model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
		{ID: "keys", Label: "Keys", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
	}})
	model.SelectTable("widgets")
	model.Focus = focusWorkspace
	updated, command := openCustomViewFromFK(model)
	model = updated.(Model)
	model = runTableCommand(model, command)

	// j/k move the row cursor like every other result table.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updated.(Model)
	if got := model.workspace.table.Cursor(); got != 1 {
		t.Fatalf("cursor after j = %d, want 1", got)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	model = updated.(Model)
	if got := model.workspace.table.Cursor(); got != 0 {
		t.Fatalf("cursor after k = %d, want 0", got)
	}
	// l moves the selected column and reveals it; h moves back.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	model = updated.(Model)
	if got := model.workspace.selectedColumn; got != 1 {
		t.Fatalf("selected column after l = %d, want 1", got)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	model = updated.(Model)
	if got := model.workspace.selectedColumn; got != 0 {
		t.Fatalf("selected column after h = %d, want 0", got)
	}
	// r reloads: a fresh request for the same view and target.
	before := len(calls)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	model = updated.(Model)
	if command == nil || len(calls) != before {
		t.Fatalf("reload did not start a new request: calls %d -> %d", before, len(calls))
	}
	model = runTableCommand(model, command)
	if len(calls) != before+1 {
		t.Fatalf("reload requests = %d, want %d", len(calls), before+1)
	}
}

// TestWorkspaceView_staleRepliesDropped: a reply from a superseded load
// is dropped when the selection changed, the target changed, or the
// connection changed while the request was in flight.
func TestWorkspaceView_staleRepliesDropped(t *testing.T) {
	t.Run("view switch", func(t *testing.T) {
		service := &stubWorkspaceViewService{
			results: map[string]sharedsql.Result{"one": keysResult("1"), "two": keysResult("2")},
			delay:   50 * time.Millisecond,
		}
		model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "one", Label: "One", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
			{ID: "two", Label: "Two", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		}})
		model.SelectTable("widgets")
		model.Focus = focusWorkspace
		// Open "one"; its request is still in flight.
		updated, command := openCustomViewFromFK(model)
		model = updated.(Model)
		// Switch to "two" before the first reply lands.
		updated, commandTwo := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
		model = updated.(Model)
		// The first reply arrives late and must be dropped.
		model = runTableCommand(model, command)
		if model.workspace.result.Rows != nil {
			t.Fatalf("stale reply for view %q applied: %#v", model.workspace.active, model.workspace.result.Rows)
		}
		model = runTableCommand(model, commandTwo)
		if len(model.workspace.result.Rows) != 1 {
			t.Fatalf("current view rows = %#v, want the second view's data", model.workspace.result.Rows)
		}
	})
	t.Run("target change", func(t *testing.T) {
		service := &stubWorkspaceViewService{
			results: map[string]sharedsql.Result{"keys": keysResult("stale")},
			delay:   50 * time.Millisecond,
		}
		model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "keys", Label: "Keys", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable, sharedsql.WorkspaceViewDatabase}},
		}})
		model.SelectTable("widgets")
		model.Focus = focusWorkspace
		updated, command := openCustomViewFromFK(model)
		model = updated.(Model)
		// The database scope changes the target while the load is in
		// flight; the table-scoped reply must be dropped.
		_ = model.selectDatabaseTarget("pluginkv")
		if model.workspace.active != "" || model.Tab != tabBrowse {
			t.Fatalf("target change left the custom selection: active %q tab %v", model.workspace.active, model.Tab)
		}
		model = runTableCommand(model, command)
		if len(model.workspace.result.Rows) != 0 {
			t.Fatalf("stale table reply applied after the target change: %#v", model.workspace.result.Rows)
		}
	})
	t.Run("connection change", func(t *testing.T) {
		service := &stubWorkspaceViewService{
			results: map[string]sharedsql.Result{"keys": keysResult("stale")},
			delay:   50 * time.Millisecond,
		}
		model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
			{ID: "keys", Label: "Keys", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		}})
		model.SelectTable("widgets")
		model.Focus = focusWorkspace
		updated, command := openCustomViewFromFK(model)
		model = updated.(Model)
		// A fresh connection (new open tag, no advertisement) replaces
		// the driver while the request is in flight.
		model.openTag++
		updated, _ = model.Update(databaseOpenedMsg{
			target:        "other:db",
			service:       &stubService{},
			info:          sharedsql.DatabaseInfo{Product: "Other", Version: "1"},
			queryLanguage: sharedsql.SQLQueryLanguage,
			openTag:       model.openTag,
		})
		model = updated.(Model)
		if model.workspace.advertised != nil {
			t.Fatal("connection change kept the old advertisement")
		}
		model = runTableCommand(model, command)
		if len(model.workspace.result.Rows) != 0 {
			t.Fatalf("stale connection reply applied: %#v", model.workspace.result.Rows)
		}
	})
}

// TestWorkspaceView_safeOutput: driver-provided cells are sanitized —
// the driver's ANSI escapes never reach the rendered view — while the
// visible cell text survives. Labels are validation-bounded control-free
// strings, so only cell data can carry escapes.
func TestWorkspaceView_safeOutput(t *testing.T) {
	row := "\x1b[31mred\x1b[0m"
	service := &stubWorkspaceViewService{
		results: map[string]sharedsql.Result{"keys": {Columns: []string{"key"}, ColumnTypes: []string{"string"}, Rows: [][]*string{{&row}}}},
	}
	workspace := &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
		{ID: "keys", Label: "Keys", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
	}}
	model := workspaceViewModel(t, service, workspace)
	model.SelectTable("widgets")
	model.Focus = focusWorkspace
	updated, command := openCustomViewFromFK(model)
	model = updated.(Model)
	model = runTableCommand(model, command)
	view := model.workspaceView()
	if strings.Contains(view, "\x1b[31m") {
		t.Fatalf("rendered view carries the driver's raw ANSI: %q", view)
	}
	stripped := ansi.Strip(view)
	if !strings.Contains(stripped, "red") {
		t.Fatalf("sanitized view = %q, want the visible cell text", stripped)
	}
}

// TestWorkspaceView_multipleCustomTabs: opening one custom tab does not
// preload another; each open starts exactly one request for its own view
// id, and the tabs are independent.
func TestWorkspaceView_multipleCustomTabs(t *testing.T) {
	var calls []sharedsql.WorkspaceViewRequest
	service := &stubWorkspaceViewService{
		results: map[string]sharedsql.Result{
			"one": keysResult("1"),
			"two": keysResult("2"),
		},
		calls: &calls,
	}
	model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
		{ID: "one", Label: "One", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
		{ID: "two", Label: "Two", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
	}})
	model.SelectTable("widgets")
	model.Focus = focusWorkspace

	updated, command := openCustomViewFromFK(model)
	model = updated.(Model)
	model = runTableCommand(model, command)
	if len(calls) != 1 || calls[0].ViewID != "one" {
		t.Fatalf("first view requests = %#v, want exactly one for %q", calls, "one")
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if len(calls) != 2 || calls[1].ViewID != "two" {
		t.Fatalf("second view requests = %#v, want exactly one for %q", calls, "two")
	}
	// Re-opening the first view loads it again for the active context.
	updated, command = model.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	if len(calls) != 3 || calls[2].ViewID != "one" {
		t.Fatalf("re-open requests = %#v, want exactly one for %q", calls, "one")
	}
}

// TestWorkspaceView_legacyConnectionHasNoCustomTabs: a connection with
// no workspace advertisement renders no custom tabs, and the standard
// tab row is unchanged.
func TestWorkspaceView_legacyConnectionHasNoCustomTabs(t *testing.T) {
	model := workspaceViewModel(t, &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}, nil)
	model.SelectTable("widgets")
	tabs := model.workspaceTabs()
	for _, tab := range tabs {
		if tab.custom.ID != "" {
			t.Fatalf("legacy connection rendered custom tab %q", tab.custom.ID)
		}
	}
}

// TestWorkspaceView_targetKindCarried: database and schema targets load
// with their structured identifiers.
func TestWorkspaceView_targetKindCarried(t *testing.T) {
	var calls []sharedsql.WorkspaceViewRequest
	service := &stubWorkspaceViewService{
		results: map[string]sharedsql.Result{"overview": {Columns: []string{"x"}, ColumnTypes: []string{"string"}}},
		calls:   &calls,
	}
	workspace := &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
		{ID: "overview", Label: "Overview", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewDatabase, sharedsql.WorkspaceViewSchema, sharedsql.WorkspaceViewTable}},
	}}
	model := workspaceViewModel(t, service, workspace)
	model.Focus = focusWorkspace

	_ = model.selectDatabaseTarget("pluginkv")
	updated, command := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	want := sharedsql.WorkspaceViewRequest{ViewID: "overview", Target: sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewDatabase, Database: "pluginkv"}}
	if calls[0] != want {
		t.Fatalf("database request = %#v, want %#v", calls[0], want)
	}

	_ = model.selectSchemaTarget("pluginkv", "public")
	model.Tab = tabBrowse
	updated, command = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	want = sharedsql.WorkspaceViewRequest{ViewID: "overview", Target: sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewSchema, Database: "pluginkv", Schema: "public"}}
	if calls[1] != want {
		t.Fatalf("schema request = %#v, want %#v", calls[1], want)
	}

	// A table selected through the sidebar path keeps its structured
	// identifiers: database and schema travel on the table-scoped view
	// request.
	_ = model.selectSchemaTableBy("public.users", "pluginkv", "public")
	model.Tab = tabBrowse
	updated, command = model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	model = updated.(Model)
	model = runTableCommand(model, command)
	want = sharedsql.WorkspaceViewRequest{ViewID: "overview", Target: sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewTable, Database: "pluginkv", Schema: "public", Table: "public.users"}}
	if calls[2] != want {
		t.Fatalf("table request = %#v, want %#v", calls[2], want)
	}
}

// TestWorkspaceView_advertisedWithoutProviderFails: a driver that
// advertises custom views but whose session does not implement the
// provider fails deterministically into the view's error state.
func TestWorkspaceView_advertisedWithoutProviderFails(t *testing.T) {
	service := &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}
	model := workspaceViewModel(t, service, &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
		{ID: "ghost", Label: "Ghost", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewTable}},
	}})
	// Strip the provider: the wrapped service must lose the interface.
	model.Database = &stubService{}
	model.SelectTable("widgets")
	model.Focus = focusWorkspace
	updated, _ := openCustomViewFromFK(model)
	model = updated.(Model)
	if view := ansi.Strip(model.workspaceView()); !strings.Contains(view, "advertised but not implemented") {
		t.Fatalf("view state = %q, want the deterministic advertisement-mismatch error", view)
	}
	if model.workspace.loading {
		t.Fatal("view must not stay loading after the synchronous mismatch failure")
	}
}

// pluginDatabaseScopeObjects is a plugin-like fixture: the synthesized
// database-root shape of flat perk/v1 responses across two databases with
// mixed object kinds.
func pluginDatabaseScopeObjects() []sharedsql.SchemaObject {
	return []sharedsql.SchemaObject{
		{Database: "pluginkv", Type: "database", Name: "pluginkv"},
		{Database: "pluginkv", Type: "table", Name: "widgets", RowCount: int64Ptr(42)},
		{Database: "pluginkv", Type: "hash", Name: "users"},
		{Database: "cache", Type: "database", Name: "cache"},
		{Database: "cache", Type: "table", Name: "sessions"},
	}
}

// pluginDatabaseWorkspace is the advertisement of a plugin-like product
// with one database-scoped custom view.
func pluginDatabaseWorkspace() *sharedsql.WorkspaceCapability {
	return &sharedsql.WorkspaceCapability{CustomViews: []sharedsql.CustomWorkspaceView{
		{ID: "db-overview", Label: "Overview", Scopes: []sharedsql.WorkspaceViewKind{sharedsql.WorkspaceViewDatabase}},
	}}
}

// TestPluginDatabaseScope_keyboardSelectsRoot drives Enter on a database
// root of a plugin-like product with explicit workspace metadata: the
// workspace targets the database, Browse lists the database's non-root
// objects in sidebar order, the database-scoped custom tab appears, and
// loading it issues a request carrying the structured database target.
func TestPluginDatabaseScope_keyboardSelectsRoot(t *testing.T) {
	var calls []sharedsql.WorkspaceViewRequest
	service := &stubWorkspaceViewService{
		results: map[string]sharedsql.Result{"db-overview": keysResult("k1", "k2")},
		calls:   &calls,
	}
	model := workspaceViewModel(t, service, pluginDatabaseWorkspace())
	_ = model.setSchemaObjects(pluginDatabaseScopeObjects())
	model.schema.component.List.Select(0) // the pluginkv database root

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.WorkspaceTarget.Kind != core.WorkspaceDatabase || model.WorkspaceTarget.Database != "pluginkv" {
		t.Fatalf("workspace target = %+v, want the pluginkv database scope", model.WorkspaceTarget)
	}
	want := []sharedsql.SchemaObject{
		{Database: "pluginkv", Type: "table", Name: "widgets", RowCount: int64Ptr(42)},
		{Database: "pluginkv", Type: "hash", Name: "users"},
	}
	assertScopeObjects(t, model, want)
	view := ansi.Strip(model.workspaceView())
	for _, present := range []string{"widgets", "users", "2 objects", "Overview"} {
		if !strings.Contains(view, present) {
			t.Fatalf("workspace view misses %q: %q", present, view)
		}
	}
	for _, absent := range []string{"cache", "sessions"} {
		if strings.Contains(view, absent) {
			t.Fatalf("workspace view leaks the other database %q: %q", absent, view)
		}
	}

	// The database-scoped custom tab is exposed after the standard tabs
	// and loads lazily with the active database target.
	labels, _ := model.workspaceTabMeta(model.workspaceTabs())
	if got := strings.Join(labels, ","); got != "SQL,Browse,Overview" {
		t.Fatalf("database tab labels = %q, want the database-scoped view", got)
	}
	if len(calls) != 0 {
		t.Fatalf("views loaded before the tab was opened: %d calls", len(calls))
	}
	updated, command := openCustomViewFromFK(model)
	model = updated.(Model)
	model = runTableCommand(model, command)
	if len(calls) != 1 {
		t.Fatalf("view requests = %#v, want exactly one", calls)
	}
	wantRequest := sharedsql.WorkspaceViewRequest{
		ViewID: "db-overview",
		Target: sharedsql.WorkspaceViewTarget{Kind: sharedsql.WorkspaceViewDatabase, Database: "pluginkv"},
	}
	if calls[0] != wantRequest {
		t.Fatalf("view request = %#v, want %#v", calls[0], wantRequest)
	}
}

// TestPluginDatabaseScope_clickSelectsRoot drives a left-click on the
// database root row of the schema sidebar: the same scope selection the
// keyboard path performs, with the same object list.
func TestPluginDatabaseScope_clickSelectsRoot(t *testing.T) {
	model := workspaceViewModel(t, &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}, pluginDatabaseWorkspace())
	_ = model.setSchemaObjects(pluginDatabaseScopeObjects())

	// The first root row is contentY 4 (pane title + 3-row filter box),
	// so terminal Y 5; any x inside the schema pane hits it.
	updated, _ := model.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)

	if model.WorkspaceTarget.Kind != core.WorkspaceDatabase || model.WorkspaceTarget.Database != "pluginkv" {
		t.Fatalf("workspace target = %+v, want the pluginkv database scope", model.WorkspaceTarget)
	}
	want := []sharedsql.SchemaObject{
		{Database: "pluginkv", Type: "table", Name: "widgets", RowCount: int64Ptr(42)},
		{Database: "pluginkv", Type: "hash", Name: "users"},
	}
	assertScopeObjects(t, model, want)
}

// TestPluginDatabaseScope_absentMetadataKeepsSQLOnlyRoot: without an
// explicit workspace advertisement an unknown product's roots behave
// exactly as before — Enter and clicks only toggle, never targeting a
// database scope.
func TestPluginDatabaseScope_absentMetadataKeepsSQLOnlyRoot(t *testing.T) {
	model := workspaceViewModel(t, &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}, nil)
	_ = model.setSchemaObjects(pluginDatabaseScopeObjects())
	model.schema.component.List.Select(0)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.WorkspaceTarget.Kind != core.WorkspaceNone {
		t.Fatalf("workspace target after Enter = %+v, want none (SQL-only root)", model.WorkspaceTarget)
	}
	if model.browse.component.ObjectListMode() {
		t.Fatal("browse pane entered object-list mode without metadata")
	}

	updated, _ = model.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.WorkspaceTarget.Kind != core.WorkspaceNone {
		t.Fatalf("workspace target after click = %+v, want none (SQL-only root)", model.WorkspaceTarget)
	}
}

// TestPluginDatabaseScope_connectionChangeDropsCapability: switching to a
// driver without workspace metadata removes the scope capability, so the
// new connection's database roots cannot be targeted — the pre-switch
// database target is left untouched.
func TestPluginDatabaseScope_connectionChangeDropsCapability(t *testing.T) {
	model := workspaceViewModel(t, &stubWorkspaceViewService{results: map[string]sharedsql.Result{}}, pluginDatabaseWorkspace())
	_ = model.setSchemaObjects(pluginDatabaseScopeObjects())
	model.schema.component.List.Select(0)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.WorkspaceTarget.Kind != core.WorkspaceDatabase {
		t.Fatalf("setup: workspace target = %+v, want the database scope", model.WorkspaceTarget)
	}

	// A fresh connection with objects but no advertisement replaces the
	// driver synchronously; the schema tree follows.
	model.openTag++
	updated, _ = model.Update(databaseOpenedMsg{
		target:        "other:db",
		service:       &stubService{},
		info:          sharedsql.DatabaseInfo{Product: "Other", Version: "1"},
		queryLanguage: sharedsql.SQLQueryLanguage,
		openTag:       model.openTag,
	})
	model = updated.(Model)
	if model.workspace.advertised != nil {
		t.Fatal("connection change kept the old advertisement")
	}
	_ = model.setSchemaObjects([]sharedsql.SchemaObject{
		{Database: "other", Type: "database", Name: "other"},
		{Database: "other", Type: "table", Name: "thing"},
	})
	model.schema.component.List.Select(0) // the other database root

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.WorkspaceTarget.Database != "pluginkv" {
		t.Fatalf("workspace target = %+v, want the pre-switch pluginkv target untouched (root not selectable)", model.WorkspaceTarget)
	}
}
