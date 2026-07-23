package workbench

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	sharedsql "github.com/l3aro/perk/internal/sql"
	"github.com/l3aro/perk/internal/sqlite"
)

func TestHuhForms_renderDeterministicallyWithinNarrowAndWideWidths(t *testing.T) {
	forms := []struct {
		name string
		view func(int) string
	}{
		{name: "column", view: func(width int) string {
			form := newColumnForm(sqlite.ColumnInfo{Name: "name", Type: "TEXT", Nullable: true}, sharedsql.ColumnTypes(sharedsql.DatabaseInfo{Product: "SQLite"}))
			form.setWidth(width)
			return form.View()
		}},
		{name: "browse", view: func(width int) string {
			form, err := newBrowseForm([]string{"id", "name"}, []*string{stringPointer("1"), stringPointer("Ada")}, []sharedsql.ColumnInfo{{Name: "id", PrimaryKey: 1}, {Name: "name"}})
			if err != nil {
				t.Fatal(err)
			}
			form.setWidth(width)
			return form.View()
		}},
		{name: "index", view: func(width int) string {
			form := newIndexForm(nil)
			form.setWidth(width)
			return form.View()
		}},
		{name: "foreign key", view: func(width int) string {
			form := newForeignKeyForm(nil)
			form.setWidth(width)
			return form.View()
		}},
		{name: "connection", view: func(width int) string {
			form := newConnectionForm()
			form.setWidth(width)
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
	form := newConnectionForm()
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
			model.structureColumns = []sharedsql.ColumnInfo{{Name: "id", Type: "INTEGER", PrimaryKey: 1}}
			model.browseResult = sqlite.Result{Columns: []string{"id"}, Rows: [][]*string{{stringPointer("1")}}}
			model.browse.SetCursor(0)

			// When
			model.Tab = tabStructure
			_ = model.openColumnForm()
			columnWidth := model.columnForm.width
			columnView := model.columnForm.View()
			_ = model.openBrowseForm()
			browseWidth := model.browseForm.width
			browseView := model.browseForm.View()
			_ = model.openIndexForm(nil)
			indexWidth := model.indexForm.width
			indexView := model.indexForm.View()
			_ = model.openForeignKeyForm(nil)
			foreignKeyWidth := model.foreignKeyForm.width
			foreignKeyView := model.foreignKeyForm.View()
			model.State = stateConnection
			connectionLayoutWidth := model.connection.width
			_ = model.newConnection()
			connectionWidth := model.connection.width
			connectionView := model.connection.View()

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
				if form.width != model.tableViewportWidth {
					t.Fatalf("%s form width = %d, want viewport width %d", form.name, form.width, model.tableViewportWidth)
				}
				for _, line := range strings.Split(ansi.Strip(form.view), "\n") {
					if got := ansi.StringWidth(line); got > model.tableViewportWidth {
						t.Fatalf("%s form line width = %d, want at most %d: %q", form.name, got, model.tableViewportWidth, line)
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
