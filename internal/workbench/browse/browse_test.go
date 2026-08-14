package browse

import (
	"charm.land/bubbles/v2/table"
	"github.com/charmbracelet/x/ansi"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

func TestValidateLimit(t *testing.T) {
	for _, valid := range []string{"1", "25", "500"} {
		if err := ValidateLimit(valid); err != nil {
			t.Fatalf("ValidateLimit(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{"", "0", "-1", "abc", "1.5"} {
		if err := ValidateLimit(invalid); err == nil {
			t.Fatalf("ValidateLimit(%q) = nil, want error", invalid)
		}
	}
	if err := ValidateLimit("501"); err == nil {
		t.Fatal("ValidateLimit above MaxRows = nil, want error")
	}
}

func TestSettings_PageSize(t *testing.T) {
	if got := (Settings{}).PageSize(25); got != 25 {
		t.Fatalf("empty settings PageSize = %d, want fallback 25", got)
	}
	if got := (Settings{Limit: 50}).PageSize(25); got != 50 {
		t.Fatalf("settings PageSize = %d, want 50", got)
	}
}

func TestCycleSort(t *testing.T) {
	m := New()
	m.Result = sharedsql.Result{Columns: []string{"id", "name"}}
	m.SelectedColumn = 1

	if !m.CycleSort() {
		t.Fatal("first sort did not change settings")
	}
	if want := []Sort{{Column: "name"}}; !reflect.DeepEqual(m.Settings.Sorts, want) {
		t.Fatalf("sorts = %#v, want %#v", m.Settings.Sorts, want)
	}
	if !m.CycleSort() {
		t.Fatal("second sort did not change settings")
	}
	if want := []Sort{{Column: "name", Desc: true}}; !reflect.DeepEqual(m.Settings.Sorts, want) {
		t.Fatalf("sorts = %#v, want %#v", m.Settings.Sorts, want)
	}
	if !m.CycleSort() {
		t.Fatal("third sort did not drop the sort")
	}
	if len(m.Settings.Sorts) != 0 {
		t.Fatalf("sorts = %#v, want empty after the cycle", m.Settings.Sorts)
	}
	m.SelectedColumn = -1
	if m.CycleSort() {
		t.Fatal("sort with negative column changed settings")
	}
}

func TestResetFilters(t *testing.T) {
	m := New()
	m.Settings.Filters = []sharedsql.BrowseFilter{{Column: "id", Operator: sharedsql.BrowseFilterGreater, Value: "1"}}
	m.ResetFilters()
	if len(m.Settings.Filters) != 0 {
		t.Fatalf("filters = %#v, want none after reset", m.Settings.Filters)
	}
}

func TestRowValuePreview(t *testing.T) {
	cases := []struct {
		value sharedsql.Value
		want  string
	}{
		{sharedsql.Value{Kind: sharedsql.ValueDefault}, "DEFAULT"},
		{sharedsql.Value{Kind: sharedsql.ValueNull}, "NULL"},
		{sharedsql.Value{Kind: sharedsql.ValueString, String: "a'b"}, `"a'b"`},
	}
	for _, test := range cases {
		if got := RowValuePreview(test.value); got != test.want {
			t.Fatalf("RowValuePreview(%#v) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestColumnType(t *testing.T) {
	cases := map[string]string{
		"INTEGER":                  "INTEGER",
		"VARCHAR(255)":             "VARCHAR",
		"TIMESTAMP WITH TIME ZONE": "TIMESTAMP",
		"  text  ":                 "TEXT",
	}
	for declaration, want := range cases {
		if got := ColumnType(declaration); got != want {
			t.Fatalf("ColumnType(%q) = %q, want %q", declaration, got, want)
		}
	}
}

func TestFilterForm_gridEditAndApply(t *testing.T) {
	columns := []sharedsql.ColumnInfo{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
	}
	form := NewFilterForm(columns, Settings{}, 25, 100, 8)
	keys := testKeybindings()

	// id: operator index 5 (after None, =, !=, <, <=) is >; value 1.
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	for range 5 {
		form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, keys)
	}
	form.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, keys)
	form.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	form.Update(tea.KeyPressMsg{Code: '1', Text: "1"}, keys)
	form.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, keys)

	// name: LIKE with value %second%.
	form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, keys)
	form.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, keys)
	form.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	for _, r := range []rune{'%', 's', 'e', 'c', 'o', 'n', 'd', '%'} {
		form.Update(tea.KeyPressMsg{Code: r, Text: string(r)}, keys)
	}
	form.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, keys)

	settings, err := form.Apply()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	want := []sharedsql.BrowseFilter{
		{Column: "id", Operator: sharedsql.BrowseFilterGreater, Value: "1"},
		{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%second%"},
	}
	if !reflect.DeepEqual(settings.Filters, want) {
		t.Fatalf("filters = %#v, want %#v", settings.Filters, want)
	}
}

func TestFilterForm_escapePreservesInlineValue(t *testing.T) {
	form := NewFilterForm([]sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER"}}, Settings{}, 25, 100, 8)
	keys := testKeybindings()

	form.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, keys)
	form.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, keys)
	if form.Fields[0].Value != "x" || form.Editing {
		t.Fatalf("form = %#v, want preserved inline value", form)
	}

	form.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, keys)
	form.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, keys)
	if form.Fields[0].Operator != sharedsql.BrowseFilterEqual || form.Editing {
		t.Fatalf("form = %#v, want preserved operator selection", form)
	}
}

func TestFilterForm_rRestoresOpenedState(t *testing.T) {
	settings := Settings{
		Filters: []sharedsql.BrowseFilter{{Column: "name", Operator: sharedsql.BrowseFilterLike, Value: "%first%"}},
		Limit:   1,
	}
	form := NewFilterForm([]sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER"}, {Name: "name", Type: "TEXT"}}, settings, 25, 100, 8)
	keys := testKeybindings()

	form.Fields[1].Operator, form.Fields[1].Value, form.Limit = sharedsql.BrowseFilterEqual, "second", "2"
	form.Update(tea.KeyPressMsg{Code: 'r', Text: "r"}, keys)
	if form.Fields[1].Operator != sharedsql.BrowseFilterLike || form.Fields[1].Value != "%first%" || form.Limit != "1" {
		t.Fatalf("reset form = %#v, want opened filter settings", form)
	}
}

func TestFilterForm_applyValidatesLimit(t *testing.T) {
	form := NewFilterForm([]sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER"}}, Settings{}, 25, 100, 8)
	form.Limit = "0"
	if _, err := form.Apply(); err == nil {
		t.Fatal("apply with limit 0 = nil, want error")
	}
	form.Limit = "50"
	settings, err := form.Apply()
	if err != nil {
		t.Fatalf("apply with limit 50: %v", err)
	}
	if settings.Limit != 50 {
		t.Fatalf("settings.Limit = %d, want 50", settings.Limit)
	}
}

func TestFilterForm_operatorOptionsByColumnType(t *testing.T) {
	form := NewFilterForm([]sharedsql.ColumnInfo{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
		{Name: "flag", Type: "BOOL"},
	}, Settings{}, 25, 100, 8)
	keys := testKeybindings()

	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	if len(form.OperatorOptions()) != 9 {
		t.Fatalf("numeric options = %v, want 9", form.OperatorOptions())
	}
	form.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, keys)

	form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	if options := form.OperatorOptions(); len(options) != 9 || options[3] != sharedsql.BrowseFilterPattern {
		t.Fatalf("text options = %v, want pattern at index 3", options)
	}
	form.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, keys)

	form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, keys)
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	if len(form.OperatorOptions()) != 5 {
		t.Fatalf("plain options = %v, want 5", form.OperatorOptions())
	}
}

func TestFilterForm_patternWildcardHint(t *testing.T) {
	form := NewFilterForm([]sharedsql.ColumnInfo{{Name: "name", Type: "TEXT"}}, Settings{}, 25, 120, 8)
	keys := testKeybindings()
	form.Update(tea.KeyPressMsg{Code: 'i', Text: "i"}, keys)
	for range 3 {
		form.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, keys)
	}
	form.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, keys)
	if view := form.View(); !strings.Contains(view, "* any, ? one char") {
		t.Fatalf("filter form = %q, want the PATTERN wildcard hint", view)
	}
}

func TestFilterForm_applyKeyBindings(t *testing.T) {
	form := NewFilterForm([]sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER"}}, Settings{}, 25, 100, 8)
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyF5},
		{Code: 's', Mod: tea.ModCtrl, Text: "s"},
	} {
		if _, action := form.Update(key, testKeybindings()); action != FilterApply {
			t.Fatalf("key %v action = %d, want apply", key, action)
		}
	}
	if _, action := form.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, testKeybindings()); action != FilterDiscard {
		t.Fatal("escape action != discard")
	}
}

func TestForm_newAndValues(t *testing.T) {
	form, err := NewForm([]string{"id", "name"}, []*string{strPtr("1"), nil}, []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}, {Name: "name"}})
	if err != nil {
		t.Fatalf("new form: %v", err)
	}
	if !form.Active() || form.Inserting {
		t.Fatal("edit form should be active and not inserting")
	}
	if !form.Values.Nulls[1] {
		t.Fatal("name should start NULL")
	}
	if form.Values.Nulls[0] {
		t.Fatal("id should not start NULL")
	}
	// Name stays NULL: no change → no row values.
	if values := form.RowValues(); len(values) != 0 {
		t.Fatalf("rowValues = %#v, want none", values)
	}
	// Mark name dirty by typing into it.
	form.Values.Nulls[1] = false
	form.Values.Fields[1] = "second"
	wantValues := []sharedsql.RowValue{{Name: "name", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "second"}}}
	if values := form.RowValues(); !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("rowValues = %#v, want %#v", values, wantValues)
	}
	wantKey := []sharedsql.RowValue{{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "1"}}}
	if key, err := form.KeyValues(); err != nil || !reflect.DeepEqual(key, wantKey) {
		t.Fatalf("keyValues = %#v, %v; want %#v", key, err, wantKey)
	}
	if got, want := form.Preview(), "Table: \nKey:\n  id = \"1\"\nChanges:\n  name = \"second\""; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
}

func TestForm_insertDefaultsAndNull(t *testing.T) {
	form, err := NewInsertForm([]string{"id", "name"})
	if err != nil {
		t.Fatalf("new insert form: %v", err)
	}
	for index := range form.Values.Defaults {
		if !form.Values.Defaults[index] || form.Values.Nulls[index] || form.Values.Fields[index] != "" {
			t.Fatalf("field %d = defaults:%t nulls:%t value:%q, want default", index, form.Values.Defaults[index], form.Values.Nulls[index], form.Values.Fields[index])
		}
	}
	// All DEFAULT → no row values (engine defaults apply).
	if values := form.RowValues(); len(values) != 0 {
		t.Fatalf("rowValues = %#v, want none", values)
	}
	// Typing selects VALUE.
	form.Values.Defaults[0] = false
	form.Values.Fields[0] = "x"
	want := []sharedsql.RowValue{{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "x"}}}
	if values := form.RowValues(); !reflect.DeepEqual(values, want) {
		t.Fatalf("rowValues = %#v, want %#v", values, want)
	}
	// NULL.
	form.Values.Nulls[0] = true
	want = []sharedsql.RowValue{{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueNull}}}
	if values := form.RowValues(); !reflect.DeepEqual(values, want) {
		t.Fatalf("rowValues = %#v, want %#v", values, want)
	}
	// Back to DEFAULT → omitted again.
	form.Values.Nulls[0] = false
	form.Values.Defaults[0] = true
	form.Values.Fields[0] = ""
	if values := form.RowValues(); len(values) != 0 {
		t.Fatalf("rowValues = %#v, want none after returning to DEFAULT", values)
	}
}

func TestForm_rejectsRowsWithoutPrimaryKey(t *testing.T) {
	if _, err := NewForm([]string{"name"}, []*string{strPtr("first")}, []sharedsql.ColumnInfo{{Name: "name"}}); err == nil || !strings.Contains(err.Error(), "primary key") {
		t.Fatalf("error = %v, want primary-key rejection", err)
	}
}

func TestForm_confirmingFlow(t *testing.T) {
	form, err := NewForm([]string{"id", "name"}, []*string{strPtr("1"), strPtr("first")}, []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}, {Name: "name"}})
	if err != nil {
		t.Fatalf("new form: %v", err)
	}
	form.Values.Fields[1] = "edited"
	if !form.HasChanges() {
		t.Fatal("form with an edited field should have changes")
	}
	form.BeginConfirmation(true)
	if !form.Confirming() || form.Confirmation == nil {
		t.Fatal("save did not open the confirmation")
	}
	form.BeginConfirmation(false)
	if form.ConfirmationSave {
		t.Fatal("discard confirmation did not record save=false")
	}
}

func TestForm_updateRouting(t *testing.T) {
	form, err := NewForm([]string{"id", "name"}, []*string{strPtr("1"), strPtr("first")}, []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}, {Name: "name"}})
	if err != nil {
		t.Fatalf("new form: %v", err)
	}
	form.Keybindings = testKeybindings()
	form.Table = "items"
	controller := &uikit.FormModeController{}

	// Save opens the confirmation.
	if _, action := form.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl, Text: "s"}, controller); action != FormNoAction {
		t.Fatalf("save action = %d, want no-action (confirmation opens)", action)
	}
	if !form.Confirming() {
		t.Fatal("save did not open the confirmation")
	}
	// Decline keeps the form.
	completed, action := form.Confirmation.Update(tea.KeyPressMsg{Code: 'n', Text: "n"}, form.Width, form.Height)
	if !completed || action != "cancel" {
		t.Fatalf("decline = completed:%t action:%q, want completed cancel", completed, action)
	}
	// Discard without changes closes directly.
	form.Confirmation = nil
	form.Values.Fields[1] = "first" // unchanged again
	controller.Mode = uikit.FormModeNormal
	if _, action := form.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, controller); action != FormDiscard {
		t.Fatalf("unchanged discard action = %d, want discard", action)
	}
}

func TestCellEditor_isLongTextType(t *testing.T) {
	for _, textType := range []string{"TEXT", "MEDIUMTEXT", "LONGTEXT", "TINYTEXT", "CLOB", "JSON", "text"} {
		if !IsLongTextType(textType) {
			t.Fatalf("IsLongTextType(%q) = false, want true", textType)
		}
	}
	if IsLongTextType("INTEGER") || IsLongTextType("VARCHAR") {
		t.Fatal("short types classified as long text")
	}
}

func TestCellEditor_buildAndValues(t *testing.T) {
	m := New()
	m.Table.SetColumns([]table.Column{{Title: "id", Width: 4}, {Title: "name", Width: 8}})
	m.Table.SetRows([]table.Row{{"1", "first"}})
	m.Table.SetCursor(0)
	m.Result = sharedsql.Result{
		Columns: []string{"id", "name"},
		Rows:    [][]*string{{strPtr("1"), strPtr("first")}},
	}
	m.Structure = []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}, {Name: "name", Type: "TEXT"}}
	m.SelectedColumn = 1

	editor, _, err := m.BuildCellEditor("items", 60)
	if err != nil {
		t.Fatalf("build cell editor: %v", err)
	}
	if editor == nil {
		t.Fatal("cell editor = nil")
	}
	if editor.ColumnName != "name" || editor.EditedVal != "first" || editor.Width != 60 {
		t.Fatalf("editor = %#v, want name/first/60", editor)
	}
	wantKey := []sharedsql.RowValue{{Name: "id", Value: sharedsql.Value{Kind: sharedsql.ValueString, String: "1"}}}
	if key, err := editor.KeyValues(); err != nil || !reflect.DeepEqual(key, wantKey) {
		t.Fatalf("keyValues = %#v, %v; want %#v", key, err, wantKey)
	}
	if got, want := editor.Preview(), "Table: items\nKey:\n  id = \"1\"\nChanges:\n  name = \"first\""; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}
	if !editor.Active() {
		t.Fatal("editor should be active")
	}
	editor.BeginConfirmation()
	if !editor.Confirming || editor.Confirm == nil {
		t.Fatal("begin confirmation did not open the dialog")
	}
}

func TestCellEditor_noPrimaryKey(t *testing.T) {
	m := New()
	m.Table.SetColumns([]table.Column{{Title: "name", Width: 8}})
	m.Table.SetRows([]table.Row{{"first"}})
	m.Table.SetCursor(0)
	m.Result = sharedsql.Result{Columns: []string{"name"}, Rows: [][]*string{{strPtr("first")}}}
	m.Structure = []sharedsql.ColumnInfo{{Name: "name"}}
	m.SelectedColumn = 0
	if _, _, err := m.BuildCellEditor("items", 60); err == nil || !strings.Contains(err.Error(), "primary key") {
		t.Fatalf("error = %v, want primary-key rejection", err)
	}
}

func TestSetObjects_staleRowsDoNotPanicOnColumnChange(t *testing.T) {
	// Given — a browse pane showing a wide table (4 result columns) with a
	// sized viewport, as after browsing any real table.
	m := New()
	m.Table.SetColumns([]table.Column{
		{Title: "id", Width: 4},
		{Title: "name", Width: 8},
		{Title: "state", Width: 8},
		{Title: "city", Width: 8},
	})
	m.Table.SetRows([]table.Row{{"1", "first", "active", "Berlin"}})
	m.Table.SetCursor(0)
	uikit.ResizeResultsTable(&m.Table, 40, 3)

	// When — selecting another table routes through SetObjects(nil): the
	// stale 4-cell rows must be dropped before the column change, because
	// bubbles renders columns indexed by row cell count and a wider stale
	// row panics with index out of range otherwise.
	m.SetObjects(nil)

	// Then — back to table-row mode with no rows left behind.
	if m.ObjectListMode() {
		t.Fatal("pane is in object-list mode after SetObjects(nil)")
	}
	if got := m.Table.Rows(); len(got) != 0 {
		t.Fatalf("table rows after SetObjects(nil) = %#v, want none", got)
	}

	// And — the object-list transition is equally safe: the 4-cell state
	// above is replaced by 3-cell scope-object rows.
	m.SetObjects([]sharedsql.SchemaObject{{Database: "office", Type: "table", Name: "orders"}})
	rows := m.Table.Rows()
	if len(rows) != 1 || len(rows[0]) != 3 || rows[0][0] != "orders" || rows[0][1] != "table" {
		t.Fatalf("table rows = %#v, want one 3-cell orders/table row", rows)
	}
	if got, ok := m.SelectedObject(); !ok || got.Name != "orders" {
		t.Fatalf("selected object = %#v, %v; want orders selected", got, ok)
	}
}

func TestDocumentEditor_buildAndValidate(t *testing.T) {
	capability := sharedsql.DocumentWriteCapability{Text: true, Format: sharedsql.DocumentFormatMongoExtendedJSON}
	editor := NewDocumentEditor("things", true, capability, nil, "{}", 60)
	if editor.Title != "Insert document" || editor.Edited != "{}" {
		t.Fatalf("editor = %#v, want insert title and {} text", editor)
	}
	if _, err := editor.BeginConfirmation(); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
	editor.Edited = "{oops"
	if _, err := editor.BeginConfirmation(); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("invalid JSON error = %v, want invalid-JSON rejection", err)
	}
	// Raw formats skip validation.
	raw := NewDocumentEditor("things", true, sharedsql.DocumentWriteCapability{Text: true, Format: "raw"}, nil, "", 60)
	raw.Edited = "anything"
	if _, err := raw.BeginConfirmation(); err != nil {
		t.Fatalf("raw format rejected: %v", err)
	}
}

func TestDocumentEditor_preview(t *testing.T) {
	capability := sharedsql.DocumentWriteCapability{Text: true, Format: sharedsql.DocumentFormatMongoExtendedJSON}
	editor := NewDocumentEditor("things", false, capability, &sharedsql.DocumentPayload{Data: []byte(`{"_id": 1}`)}, `{"name": "x"}`, 60)
	editor.Collection = "things"
	got := editor.Preview()
	for _, want := range []string{"Table: things", "Key:\n  _id = {\"_id\": 1}", "Changes:", `{"name": "x"}`} {
		if !strings.Contains(got, want) {
			t.Fatalf("preview = %q, want %q", got, want)
		}
	}
}

func TestPager(t *testing.T) {
	layout := uikit.Layout{ViewportWidth: 100}
	m := New()
	m.Result.HasMore = true
	pager := m.Pager(layout)
	if pager.PrevEnabled || !pager.NextEnabled {
		t.Fatalf("enabled = %t/%t, want only Next on page 0", pager.PrevEnabled, pager.NextEnabled)
	}
	if pager.Line == "" {
		t.Fatal("pager line must always render")
	}
	m.Page = 1
	m.Result.HasMore = false
	pager = m.Pager(layout)
	if !pager.PrevEnabled || pager.NextEnabled {
		t.Fatalf("enabled = %t/%t, want only Prev on the last page", pager.PrevEnabled, pager.NextEnabled)
	}
	row := ansi.Strip(pager.Line)
	if !strings.Contains(row, "Prev") || !strings.Contains(row, "Next") {
		t.Fatalf("pager line = %q, want both buttons rendered", row)
	}
}

func TestStatusSplitAndFooterRows(t *testing.T) {
	m := New()
	m.Status = "orderdetails | 2,996-3,020 | 7/25 | page 9"
	wide := uikit.Layout{ViewportWidth: 120}
	narrow := uikit.Layout{ViewportWidth: 60}
	if m.StatusSplit(wide) {
		t.Fatal("wide layout should not split the status line")
	}
	if !m.StatusSplit(narrow) {
		t.Fatal("narrow layout should split the status line")
	}
	if m.FooterRows(wide) != 8 || m.FooterRows(narrow) != 9 {
		t.Fatalf("footer rows = %d/%d, want 8/9", m.FooterRows(wide), m.FooterRows(narrow))
	}
	line := m.StatusLine(wide)
	if strings.Contains(line, "\n") {
		t.Fatalf("wide status line = %q, want a single line", line)
	}
	lines := strings.Split(m.StatusLine(narrow), "\n")
	if len(lines) != 2 {
		t.Fatalf("narrow status line = %q, want two lines", m.StatusLine(narrow))
	}
}

func TestView_rendersPagerLast(t *testing.T) {
	m := New()
	m.Table.SetColumns([]table.Column{{Title: "id", Width: 4}})
	m.Table.SetRows([]table.Row{{"1"}})
	m.Page = 1
	m.Result.HasMore = false
	layout := uikit.Layout{ViewportWidth: 80, Width: 100, Height: 24, PaneHeight: 10}
	lines := strings.Split(m.View(layout), "\n")
	last := strings.TrimSpace(ansi.Strip(lines[len(lines)-1]))
	if !strings.HasPrefix(last, "◀ Prev") || !strings.HasSuffix(last, "Next ▶") {
		t.Fatalf("last view line = %q, want the pinned pager row", last)
	}
}

func TestUpdate_events(t *testing.T) {
	m := New()
	keys := testKeybindings()
	layout := uikit.Layout{ViewportWidth: 80}

	// Sort emits DataChanged only when the sort changes.
	m.Result = sharedsql.Result{Columns: []string{"id"}}
	model, event, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"}, layout, keys, Backend{})
	if _, ok := event.(DataChanged); !ok {
		t.Fatalf("sort event = %#v, want DataChanged", event)
	}
	// Reset emits DataChanged.
	model.Settings.Filters = []sharedsql.BrowseFilter{{Column: "id", Operator: sharedsql.BrowseFilterEqual, Value: "1"}}
	_, event, _ = model.Update(tea.KeyPressMsg{Code: 'r', Text: "r"}, layout, keys, Backend{})
	if _, ok := event.(DataChanged); !ok {
		t.Fatalf("reset event = %#v, want DataChanged", event)
	}
	// Paging bumps the tag and requests a page.
	m2 := New()
	m2.Result.HasMore = true
	_, event, _ = m2.Update(tea.KeyPressMsg{Code: 'n', Text: "n"}, layout, keys, Backend{})
	if request, ok := event.(PageRequested); !ok || request.Delta != 1 {
		t.Fatalf("next event = %#v, want PageRequested{1}", event)
	}
	// Yank emits ClipboardRequested with the selected cell text.
	m3 := New()
	m3.Table.SetColumns([]table.Column{{Title: "id", Width: 4}})
	m3.Table.SetRows([]table.Row{{"42"}})
	m3.Table.SetCursor(0)
	m3.Result = sharedsql.Result{Columns: []string{"id"}, Rows: [][]*string{{strPtr("42")}}, UntruncatedRows: [][]*string{{strPtr("42")}}}
	m3.SelectedColumn = 0
	_, event, _ = m3.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}, layout, keys, Backend{})
	if request, ok := event.(uikit.ClipboardRequested); !ok || request.Text != "42" {
		t.Fatalf("yank event = %#v, want ClipboardRequested{42}", event)
	}
}

func TestUpdate_tableNavigation(t *testing.T) {
	m := New()
	m.Table.SetRows([]table.Row{{"a"}, {"b"}})
	layout := uikit.Layout{ViewportWidth: 80}
	model, event, cmd := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}, layout, testKeybindings(), Backend{})
	if event != nil || cmd != nil {
		t.Fatalf("navigation event/cmd = %#v/%v, want nil/nil", event, cmd)
	}
	if got := model.Table.Cursor(); got != 1 {
		t.Fatalf("cursor = %d, want 1 after j", got)
	}
}

func strPtr(s string) *string { return &s }

func TestDocumentEditor_unknownFormatPassesExactBytes(t *testing.T) {
	// Raw/non-Mongo formats skip JSON validation and keep the exact text
	// through the preview and the save payload.
	raw := NewDocumentEditor("things", false, sharedsql.DocumentWriteCapability{Text: true, Format: ""}, &sharedsql.DocumentPayload{Data: []byte(`{"_id": 1}`)}, "line1\n\tline2  trailing  ", 60)
	raw.Collection = "things"
	raw.Edited = "line1\n\tline2  trailing  "
	if _, err := raw.BeginConfirmation(); err != nil {
		t.Fatalf("raw format rejected valid text: %v", err)
	}
	if !raw.Confirming || raw.Confirmation == nil {
		t.Fatal("raw save did not open the confirmation")
	}
	got := raw.Preview()
	if !strings.Contains(got, "line1\n\tline2  trailing  ") {
		t.Fatalf("preview = %q, want the exact untrimmed document text", got)
	}
}
