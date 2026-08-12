package workbench

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/sqlite"
	"github.com/l3aro/perk-workbench/internal/workbench/browse"
	"github.com/l3aro/perk-workbench/internal/workbench/connection"
	"github.com/l3aro/perk-workbench/internal/workbench/schema"
)

func TestHuhForms_renderDeterministicallyWithinNarrowAndWideWidths(t *testing.T) {
	forms := []struct {
		name string
		view func(int) string
	}{
		{name: "column", view: func(width int) string {
			form := schema.NewColumnForm(sqlite.ColumnInfo{Name: "name", Type: "TEXT", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
			form.SetWidth(width)
			return form.View()
		}},
		{name: "browse", view: func(width int) string {
			form, err := browse.NewForm([]string{"id", "name"}, []*string{stringPointer("1"), stringPointer("Ada")}, []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}, {Name: "name"}})
			if err != nil {
				t.Fatal(err)
			}
			form.SetWidth(width)
			return form.View()
		}},
		{name: "index", view: func(width int) string {
			form := schema.NewIndexForm(nil)
			form.SetKeys(DefaultKeybindings())
			form.SetWidth(width)
			return form.View()
		}},
		{name: "foreign key", view: func(width int) string {
			form := schema.NewForeignKeyForm(nil)
			form.SetKeys(DefaultKeybindings())
			form.SetWidth(width)
			return form.View()
		}},
		{name: "connection", view: func(width int) string {
			form := connection.NewForm()
			form.SetWidth(width)
			return form.View()
		}},
	}

	for _, form := range forms {
		for _, width := range []int{24, 72} {
			t.Run(form.name+"/width", func(t *testing.T) {
				// Given
				first := ansi.Strip(form.view(width))

				// When
				second := ansi.Strip(form.view(width))

				// Then
				if first == "" || first != second {
					t.Fatalf("Huh form render = %q / %q", first, second)
				}
				for _, line := range strings.Split(first, "\n") {
					if got := ansi.StringWidth(line); got > width {
						t.Fatalf("Huh form line width = %d, want at most %d: %q", got, width, line)
					}
				}
			})
		}
	}
}

func TestConnectionForm_rendersActionsAsButtonGroup(t *testing.T) {
	// Given
	form := connection.NewForm()
	view := ansi.Strip(form.View())

	// When
	testIndex := strings.Index(view, connectionActionTest)
	connectIndex := strings.Index(view, connectionActionConnect)

	// Then
	if testIndex < 0 || connectIndex < 0 || strings.Contains(view[testIndex:connectIndex], "\n") {
		t.Fatalf("connection action group = %q", view)
	}
}

func TestHuhForms_openAfterResizeUsesCurrentLayoutWidth(t *testing.T) {
	for _, width := range []int{24, 72} {
		t.Run("width", func(t *testing.T) {
			// Given
			model := resizeModel(readyModel(t), width, 24)
			model.SelectedTable = "items"
			model.schema.component.Structure.Columns = []sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}}
			model.browse.component.Result = sqlite.Result{Columns: []string{"id"}, Rows: [][]*string{{stringPointer("1")}}}
			model.browse.component.Table.SetCursor(0)

			// When
			model.Tab = tabStructure
			_ = model.openColumnForm()
			columnWidth := model.schema.component.Structure.ColumnForm.Width
			columnView := model.schema.component.Structure.ColumnForm.View()
			_ = model.openBrowseForm()
			browseWidth := model.browse.component.Form.Width
			browseView := model.browse.component.Form.View()
			_ = model.openIndexForm(nil)
			indexWidth := model.schema.component.Structure.IndexForm.Width
			indexView := model.schema.component.Structure.IndexForm.View()
			_ = model.openForeignKeyForm(nil)
			foreignKeyWidth := model.schema.component.Structure.ForeignKeyForm.Width
			foreignKeyView := model.schema.component.Structure.ForeignKeyForm.View()
			model.State = stateConnection
			connectionLayoutWidth := model.connection.component.Form.Width
			_ = model.newConnection()
			connectionWidth := model.connection.component.Form.Width
			connectionView := model.connection.component.Form.View()

			// Then
			for _, form := range []struct {
				name  string
				width int
				view  string
			}{
				{name: "column", width: columnWidth, view: columnView},
				{name: "browse", width: browseWidth, view: browseView},
				{name: "index", width: indexWidth, view: indexView},
				{name: "foreign key", width: foreignKeyWidth, view: foreignKeyView},
			} {
				if form.width != model.layout.tableViewportWidth {
					t.Fatalf("%s form width = %d, want viewport width %d", form.name, form.width, model.layout.tableViewportWidth)
				}
				for _, line := range strings.Split(ansi.Strip(form.view), "\n") {
					if got := ansi.StringWidth(line); got > model.layout.tableViewportWidth {
						t.Fatalf("%s form line width = %d, want at most %d: %q", form.name, got, model.layout.tableViewportWidth, line)
					}
				}
			}
			if connectionWidth != connectionLayoutWidth {
				t.Fatalf("connection form width = %d, want preserved layout width %d", connectionWidth, connectionLayoutWidth)
			}
			for _, line := range strings.Split(ansi.Strip(connectionView), "\n") {
				if got := ansi.StringWidth(line); got > connectionWidth {
					t.Fatalf("connection form line width = %d, want at most %d: %q", got, connectionWidth, line)
				}
			}
		})
	}
}
