package workbench

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// TestNonVim_singleClickEntersInsertOnEditor: without vim mode a single
// click on the SQL editor enters insert mode, so typing works with no mode
// switch (vim mode needs a double-click).
func TestNonVim_singleClickEntersInsertOnEditor(t *testing.T) {
	model := readyModel(t)
	model.vimMode = false
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model = resizeModel(model, 100, 24)

	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 4, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.formMode.editing() {
		t.Fatal("single click on SQL editor did not enter insert mode")
	}
	for _, ch := range "select 1" {
		updated, _ := model.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		model = updated.(Model)
	}
	if got := model.editor.value; got != "select 1" {
		t.Fatalf("editor value = %q, want typed text", got)
	}
}

// TestNonVim_singleClickEntersInsertOnFormField: without vim mode a single
// click on a form field enters insert mode (vim mode needs a double-click).
func TestNonVim_singleClickEntersInsertOnFormField(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model.vimMode = false
	model = resizeModel(model, 100, 24)

	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatal("single click on form field did not enter insert mode")
	}
	for _, ch := range "renamed" {
		model = updateColumn(model, tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	if got := model.columnForm.values.name; !strings.Contains(got, "renamed") {
		t.Fatalf("name = %q, want typed text", got)
	}
}

func TestFormFieldIndexAt(t *testing.T) {
	view := "┃ Name*\n┃ > value\n\n┃ Type*\n┃ > TEXT\n┃   INTEGER\n\n┃ Nullable\n┃\n┃   Yes     No\n\nenter next"
	titles := []string{"Name*", "Type*", "Nullable"}
	cases := []struct {
		line int
		want int
	}{
		{0, 0}, {1, 0}, {2, 0}, // name block (title, value, blank)
		{3, 1}, {4, 1}, {5, 1}, // type block (title, value, option)
		{6, 1},
		{7, 2}, {8, 2}, {9, 2},
		{10, 2}, {11, 2}, // help footer still maps to the last field
	}
	for _, c := range cases {
		if got := formFieldIndexAt(view, 0, c.line, titles); got != c.want {
			t.Fatalf("line %d = %d, want %d", c.line, got, c.want)
		}
	}
	if got := formFieldIndexAt(view, 0, 12, titles); got != -1 {
		t.Fatalf("past end = %d, want -1", got)
	}
	if got := formFieldIndexAt(view, 0, -1, titles); got != -1 {
		t.Fatalf("negative view line = %d, want -1", got)
	}
	if got := formFieldIndexAt(view, -1, 0, titles); got != -1 {
		t.Fatalf("negative scroll offset = %d, want -1", got)
	}
	if got := formFieldIndexAt(view, 2, 1, titles); got != 1 {
		t.Fatalf("scrolled viewport line = %d, want 1 (view line 3)", got)
	}
	if got := formFieldIndexAt("saving row changes", 0, 0, titles); got != -1 {
		t.Fatalf("status-text layout = %d, want -1", got)
	}
	if got := formFieldIndexAt(view, 0, 0, nil); got != -1 {
		t.Fatalf("no titles = %d, want -1", got)
	}
}

func TestFormFieldIndexAt_windowedViewMapsVisibleFields(t *testing.T) {
	// A focus-scrolled window (huh group viewport): the first field's title
	// (Name*) is scrolled out above, and the visible window starts mid-way
	// through the Type block. Clicks on the scrolled-in tail must map to the
	// field whose block is visible, not to -1.
	view := "┃ > TEXT\n┃   INTEGER\n\n┃ Nullable\n┃\n┃   Yes     No\n\nenter next"
	titles := []string{"Name*", "Type*", "Nullable"}
	cases := []struct {
		line int
		want int
	}{
		{0, 1}, {1, 1}, {2, 1}, // Type block tail + gap, title scrolled out
		{3, 2}, {4, 2}, {5, 2}, {6, 2}, {7, 2}, // Nullable + help footer
	}
	for _, c := range cases {
		if got := formFieldIndexAt(view, 0, c.line, titles); got != c.want {
			t.Fatalf("line %d = %d, want %d", c.line, got, c.want)
		}
	}
}

func TestFormLineIsTitle(t *testing.T) {
	cases := []struct {
		line, title string
		want        bool
	}{
		{"┃ Name*", "Name*", true},
		{"┃ Name*", "Name", true},
		{"┃ > name", "Name*", false}, // value lines never match
		{"Action", "Action", true},   // custom fields render without a gutter
		{" Test connection   Connect", "Action", false},
		{"┃   MySQL", "MySQL", true}, // option lines can match later titles
	}
	for _, c := range cases {
		if got := formLineIsTitle(c.line, c.title); got != c.want {
			t.Fatalf("formLineIsTitle(%q, %q) = %t, want %t", c.line, c.title, got, c.want)
		}
	}
}

func TestBrowseForm_singleClickFocusesFieldInNormalMode(t *testing.T) {
	model := openBrowseRow(t, 0)
	model = resizeModel(model, 100, 26)
	// Form starts at screen y=4 (header 1 + pane border 1 + tabs 1 + blank 1);
	// the name field's title line is view line 3.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.browseForm.form.GetFocusedField().GetKey(); got != "value-1" {
		t.Fatalf("focused field = %q, want value-1", got)
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal", model.formMode.mode)
	}
}

func TestBrowseForm_singleClickFirstFieldStaysFocused(t *testing.T) {
	model := openBrowseRow(t, 0)
	model = resizeModel(model, 100, 24)
	// Click the id field's value line (view line 1, screen y=5).
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.browseForm.form.GetFocusedField().GetKey(); got != "value-0" {
		t.Fatalf("focused field = %q, want value-0", got)
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal", model.formMode.mode)
	}
}

func TestBrowseForm_doubleClickEntersInsertModeOnClickedField(t *testing.T) {
	model := openBrowseRow(t, 0)
	model = resizeModel(model, 100, 26)
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatalf("mode = %d, want insert", model.formMode.mode)
	}
	if got := model.browseForm.form.GetFocusedField().GetKey(); got != "value-1" {
		t.Fatalf("focused field = %q, want value-1", got)
	}
}

func TestBrowseForm_releaseAfterClickDoesNotEnterInsert(t *testing.T) {
	model := openBrowseRow(t, 0)
	model = resizeModel(model, 100, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseReleaseMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal after release", model.formMode.mode)
	}
}

func TestBrowseForm_doubleClickInInsertModeKeepsEditingField(t *testing.T) {
	model := openBrowseRow(t, 0)
	model = resizeModel(model, 100, 26)
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatalf("mode = %d, want insert", model.formMode.mode)
	}
}

func TestBrowseFilterForm_clickSelectsRowAndDoubleClickEdits(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabBrowse
	model.structureColumns = []sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}, {Name: "name", Type: "TEXT"}}
	model = resizeModel(model, 100, 26)
	_ = model.openBrowseFilterForm()
	// View: line 0 header, line 1 id row, line 2 name row, line 3 Rows row.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 6, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.browseFilterForm.row != 1 {
		t.Fatalf("filter row = %d, want 1", model.browseFilterForm.row)
	}
	if model.browseFilterForm.editing {
		t.Fatal("single click started editing")
	}
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 6, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.browseFilterForm.editing {
		t.Fatal("double click did not start editing")
	}
}

func TestColumnForm_clickFocusesClickedFieldInNormalMode(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = resizeModel(model, 100, 30)
	// Name block is view lines 0-2; the Type* title is at view line 3.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.columnForm.form.GetFocusedField().GetKey(); got != "type" {
		t.Fatalf("focused field = %q, want type", got)
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal", model.formMode.mode)
	}
}

func TestColumnForm_doubleClickEntersInsertOnClickedField(t *testing.T) {
	model := openColumn(t, "name", "TEXT")
	model = resizeModel(model, 100, 30)
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatalf("mode = %d, want insert", model.formMode.mode)
	}
	if got := model.columnForm.form.GetFocusedField().GetKey(); got != "name" {
		t.Fatalf("focused field = %q, want name", got)
	}
}

func TestIndexForm_clickFocusesClickedField(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabIndexes
	model = resizeModel(model, 100, 26)
	_ = model.openIndexForm(nil)
	_ = model.indexForm.form.Init()
	// Columns* title is at view line 3 (name block 0-2).
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.indexForm.form.GetFocusedField().GetKey(); got != "columns" {
		t.Fatalf("focused field = %q, want columns", got)
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal", model.formMode.mode)
	}
}

func TestIndexForm_doubleClickEntersInsertOnClickedField(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabIndexes
	model = resizeModel(model, 100, 24)
	_ = model.openIndexForm(nil)
	_ = model.indexForm.form.Init()
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatalf("mode = %d, want insert", model.formMode.mode)
	}
	if got := model.indexForm.form.GetFocusedField().GetKey(); got != "name" {
		t.Fatalf("focused field = %q, want name", got)
	}
}

func TestForeignKeyForm_clickFocusesClickedField(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabForeignKeys
	model = resizeModel(model, 100, 30)
	_ = model.openForeignKeyForm(nil)
	_ = model.foreignKeyForm.form.Init()
	// Reference columns* title is at view line 6 (two 3-line blocks before it).
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.foreignKeyForm.form.GetFocusedField().GetKey(); got != "reference-columns" {
		t.Fatalf("focused field = %q, want reference-columns", got)
	}
}

func TestForeignKeyForm_doubleClickEntersInsertOnClickedField(t *testing.T) {
	model := readyModel(t)
	model.SelectedTable, model.Tab = "items", tabForeignKeys
	model = resizeModel(model, 100, 24)
	_ = model.openForeignKeyForm(nil)
	_ = model.foreignKeyForm.form.Init()
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatalf("mode = %d, want insert", model.formMode.mode)
	}
	if got := model.foreignKeyForm.form.GetFocusedField().GetKey(); got != "columns" {
		t.Fatalf("focused field = %q, want columns", got)
	}
}

func TestConnectionForm_clickSelectsDriverOption(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 100, 44)
	_ = model.newConnection()
	// SQLite layout: Driver title at view line 0, options at 1-3
	// (SQLite, MySQL, PostgreSQL). Pane content starts at screen y=2, so
	// the MySQL option row is at y=4.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 4, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.connection.values.driver != driverMySQL {
		t.Fatalf("driver = %q, want mysql", model.connection.values.driver)
	}
	// The rebuild resets focus to the first field, matching the keyboard
	// path, and the form now carries the MySQL TLS select.
	if got := model.connection.form.GetFocusedField().GetKey(); got != "driver" {
		t.Fatalf("focused field = %q, want driver", got)
	}
	if !strings.Contains(model.connection.View(), "TLS") {
		t.Fatal("MySQL form has no TLS field after driver click")
	}
}

func TestConnectionForm_clickSwapsPortOnDriverOption(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 100, 44)
	_ = model.newConnection()
	model.connection.values.port = "5432"
	// Click the MySQL option (view line 2, screen y=4): the port follows
	// the MySQL default, matching the keyboard driver change.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 4, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.connection.values.driver != driverMySQL || model.connection.values.port != "3306" {
		t.Fatalf("driver/port = %q/%q, want mysql/3306", model.connection.values.driver, model.connection.values.port)
	}
}

func TestConnectionForm_clickSelectsTLSOption(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 100, 44)
	_ = model.newConnection()
	model.connection.values.driver = driverMySQL
	_ = model.connection.rebuildForm()
	// MySQL layout: TLS title at view line 23, options at 24-26. Pane
	// content starts at screen y=2, so "Verify certificate" is at y=26.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 26, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.connection.values.mysqlTLS != mysqlTLSVerify {
		t.Fatalf("mysqlTLS = %q, want %q", model.connection.values.mysqlTLS, mysqlTLSVerify)
	}
	// Focus returns to the TLS field so the pane stays scrolled to it.
	if got := model.connection.form.GetFocusedField().GetKey(); got != "tls" {
		t.Fatalf("focused field = %q, want tls", got)
	}
}

func TestConnectionForm_clickOnSelectTitleOnlyFocuses(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 100, 44)
	_ = model.newConnection()
	// The Driver title (view line 0, screen y=2) is not an option row: the
	// click focuses the field without changing the value.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 2, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.connection.values.driver != driverSQLite {
		t.Fatalf("driver = %q, want sqlite unchanged", model.connection.values.driver)
	}
	if got := model.connection.form.GetFocusedField().GetKey(); got != "driver" {
		t.Fatalf("focused field = %q, want driver", got)
	}
	// A click on the blank line under the options (view line 4, screen y=6)
	// also leaves the value alone.
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 6, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.connection.values.driver != driverSQLite {
		t.Fatalf("driver = %q, want sqlite unchanged", model.connection.values.driver)
	}
}

func TestConnectionForm_clickSelectsWrappedTLSOption(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	// Compact pane 40 wide: the form renders at 36 (width-4), so huh wraps
	// "Encrypt, don't verify certificate" across two option rows.
	model = resizeModel(model, 40, 44)
	_ = model.newConnection()
	model.connection.values.driver = driverMySQL
	_ = model.connection.rebuildForm()
	// Click the wrapped continuation row, then the option after the wrapped
	// one; each click re-locates its row in the freshly rendered view (the
	// rebuild scrolls the form viewport to the refocused TLS field).
	for _, test := range []struct {
		label string
		want  mysqlTLSMode
	}{
		{label: "certificate", want: mysqlTLSSkipVerify},
		{label: "Don't encrypt", want: mysqlTLSDisabled},
	} {
		view := model.connection.View()
		viewLine := -1
		for index, line := range strings.Split(ansi.Strip(view), "\n") {
			clean := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "┃"))
			clean = strings.TrimSpace(strings.TrimPrefix(clean, ">"))
			if clean == test.label {
				viewLine = index
				break
			}
		}
		if viewLine < 0 {
			t.Fatalf("rendered form does not contain option row %q", test.label)
		}
		updated, _ := model.Update(tea.MouseClickMsg{X: 20, Y: viewLine + 2, Button: tea.MouseLeft})
		model = updated.(Model)
		if model.connection.values.mysqlTLS != test.want {
			t.Fatalf("mysqlTLS = %q, want %q after clicking %q", model.connection.values.mysqlTLS, test.want, test.label)
		}
	}
}

func TestConnectionForm_clickValueReadingLikeOptionFocusesField(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 100, 44)
	_ = model.newConnection()
	// A Name value that reads like a driver label must not change the
	// driver: the click focuses the input field instead. Name title at
	// view line 5, value at 6; pane content starts at screen y=2.
	model.connection.values.name = "MySQL"
	_ = model.connection.rebuildForm()
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 8, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.connection.values.driver != driverSQLite {
		t.Fatalf("driver = %q, want sqlite unchanged", model.connection.values.driver)
	}
	if got := model.connection.form.GetFocusedField().GetKey(); got != "name" {
		t.Fatalf("focused field = %q, want name", got)
	}
	// A Database value that reads like a wrapped TLS fragment must not
	// change the TLS mode. Database title at view line 20, value at 21
	// (screen y=23).
	model.connection.values.driver = driverMySQL
	model.connection.values.target = "certificate"
	_ = model.connection.rebuildForm()
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 23, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.connection.values.mysqlTLS != mysqlTLSDisabled {
		t.Fatalf("mysqlTLS = %q, want disabled unchanged", model.connection.values.mysqlTLS)
	}
	if got := model.connection.form.GetFocusedField().GetKey(); got != "database" {
		t.Fatalf("focused field = %q, want database", got)
	}
}

func TestConnectionForm_clickFocusesClickedField(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 100, 30)
	_ = model.newConnection()
	// SQLite layout: Driver block 0-4, Name* title at view line 5. The pane
	// content starts at screen y=2, so the Name title is at y=7.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 8, Button: tea.MouseLeft})
	model = updated.(Model)
	if got := model.connection.form.GetFocusedField().GetKey(); got != "name" {
		t.Fatalf("focused field = %q, want name", got)
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal", model.formMode.mode)
	}
}

func TestConnectionForm_doubleClickEntersInsertOnClickedField(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 100, 30)
	_ = model.newConnection()
	// Target* title is at view line 8 (Name block 5-7); screen y = 2+9 = 11.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 11, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 11, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert {
		t.Fatalf("mode = %d, want insert", model.formMode.mode)
	}
	if got := model.connection.form.GetFocusedField().GetKey(); got != "target" {
		t.Fatalf("focused field = %q, want target", got)
	}
}

func TestConnectionForm_clickExecutesActionButtons(t *testing.T) {
	for _, test := range []struct {
		name   string
		action string
	}{
		{name: "test", action: connectionActionTest},
		{name: "connect", action: connectionActionConnect},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := readyModel(t)
			model.State = stateConnection
			model = resizeModel(model, 100, 30)
			_ = model.newConnection()
			model.connection.values.target = ":memory:"
			lines := strings.Split(ansi.Strip(model.connection.View()), "\n")
			buttonLine, x := -1, -1
			for line, text := range lines {
				if start := strings.Index(text, test.action); start >= 0 {
					buttonLine, x = line, start
					break
				}
			}
			if buttonLine < 0 || x < 0 {
				t.Fatalf("buttons row not found in connection form view")
			}

			// When
			updated, command := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 1 + x, Y: buttonLine + 2, Button: tea.MouseLeft})
			model = updated.(Model)

			// Then
			if test.action == connectionActionTest {
				if command == nil {
					t.Fatal("Test click returned no command")
				}
				if _, ok := command().(connectionTestMsg); !ok {
					t.Fatalf("Test click message = %T, want connection test", command())
				}
			} else if model.State != stateOpening {
				t.Fatalf("connection state = %v, want opening", model.State)
			}
		})
	}
}

// TestConnectionForm_compactClickExecutesActionButtons guards the compact
// single-pane layout: the connection form renders full-width there, and
// clicks on its action buttons must execute like in the wide layout
// instead of being ignored.
func TestConnectionForm_compactClickExecutesActionButtons(t *testing.T) {
	model := readyModel(t)
	model.State = stateConnection
	model = resizeModel(model, 60, 20) // height < 24 forces compact
	if !model.compact {
		t.Fatal("model not compact after small resize")
	}
	_ = model.newConnection()
	model.connection.values.target = ":memory:"
	for range 4 {
		updated, _ := model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		model = updated.(Model)
	}
	if got := model.connection.form.GetFocusedField().GetKey(); got != "action" {
		t.Fatalf("focused field = %q, want action", got)
	}
	lines := strings.Split(ansi.Strip(model.connection.View()), "\n")
	buttonLine, x := -1, -1
	for line, text := range lines {
		if start := strings.Index(text, connectionActionTest); start >= 0 {
			buttonLine, x = line, start
			break
		}
	}
	if buttonLine < 0 || x < 0 {
		t.Fatalf("buttons row not visible in compact connection form view")
	}

	updated, command := model.Update(tea.MouseClickMsg{X: 1 + x, Y: buttonLine + 2, Button: tea.MouseLeft})
	model = updated.(Model)
	if command == nil {
		t.Fatal("Test click returned no command")
	}
	if _, ok := command().(connectionTestMsg); !ok {
		t.Fatalf("Test click message = %T, want connection test", command())
	}
}

func TestSQLTab_doubleClickEditorEntersInsertMode(t *testing.T) {
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model = resizeModel(model, 100, 24)
	// Editor box occupies the first editorHeight lines of the pane (y=4..).
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.formMode.editing() {
		t.Fatal("editor double click did not enter insert mode")
	}
}

func TestSQLTab_singleClickEditorStaysNormal(t *testing.T) {
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model = resizeModel(model, 100, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal", model.formMode.mode)
	}
}

func TestSQLTab_singleClickFocusesEditor(t *testing.T) {
	model := readyModel(t)
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model = resizeModel(model, 100, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.editor.text.Focused() {
		t.Fatal("editor not focused after single click")
	}
	if model.formMode.mode != formModeNormal {
		t.Fatalf("mode = %d, want normal", model.formMode.mode)
	}
}

func TestBrowseForm_singleClickKeepsNullFlagDoubleClickClearsIt(t *testing.T) {
	model := readyBrowseModel(t)
	model.browse.SetCursor(0)
	model.browseResult.Rows[0][1] = nil // name is NULL
	model = updateBrowseForm(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = resizeModel(model, 100, 26)
	if !model.browseForm.values.nulls[1] {
		t.Fatal("fixture: name should start as NULL")
	}
	// Single click on the name field only focuses it; the NULL flag survives.
	updated, _ := model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeNormal || !model.browseForm.values.nulls[1] {
		t.Fatalf("single click mode/nulls = %d/%t, want normal/true", model.formMode.mode, model.browseForm.values.nulls[1])
	}
	// Double click enters insert mode and clears the NULL flag for typing.
	updated, _ = model.Update(tea.MouseClickMsg{X: model.schemaWidth + 10, Y: 7, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.formMode.mode != formModeInsert || model.browseForm.values.nulls[1] {
		t.Fatalf("double click mode/nulls = %d/%t, want insert/false", model.formMode.mode, model.browseForm.values.nulls[1])
	}
}

func TestChat_singleClickStaysNormalMode(t *testing.T) {
	model := readyModel(t)
	model.chat.visible = true
	model = resizeModel(model, 140, 32)
	// Chat pane starts at x = schemaWidth + editorWidth = 102.
	updated, _ := model.Update(tea.MouseClickMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.Focus != focusChat {
		t.Fatalf("focus = %v, want focusChat", model.Focus)
	}
	if model.chat.chatMode != formModeNormal {
		t.Fatalf("chat mode = %d, want normal", model.chat.chatMode)
	}
}

func TestChat_doubleClickEntersInsertMode(t *testing.T) {
	model := readyModel(t)
	model.chat.visible = true
	model = resizeModel(model, 140, 32)
	updated, _ := model.Update(tea.MouseClickMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.chat.chatMode != formModeInsert {
		t.Fatalf("chat mode = %d, want insert", model.chat.chatMode)
	}
}

func TestChat_doubleClickInsertSurvivesRelease(t *testing.T) {
	model := readyModel(t)
	model.chat.visible = true
	model = resizeModel(model, 140, 32)
	updated, _ := model.Update(tea.MouseClickMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseReleaseMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.chat.chatMode != formModeInsert {
		t.Fatalf("chat mode = %d, want insert after release", model.chat.chatMode)
	}
}

func TestChat_singleClickAfterDoubleClickExitsInsert(t *testing.T) {
	model := readyModel(t)
	model.chat.visible = true
	model = resizeModel(model, 140, 32)
	updated, _ := model.Update(tea.MouseClickMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseClickMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	updated, _ = model.Update(tea.MouseReleaseMsg{X: 110, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	// A later single click at a different spot exits insert mode.
	updated, _ = model.Update(tea.MouseClickMsg{X: 120, Y: 12, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.chat.chatMode != formModeNormal {
		t.Fatalf("chat mode = %d, want normal", model.chat.chatMode)
	}
}
