package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/drivers/sqlite"
)

func TestWideLayout_renders_query_log_in_its_own_pane(t *testing.T) {
	// Given
	model := readyModel(t)

	// When
	model = resizeModel(model, 160, 24)

	// Then
	if model.layout.queryLogHeight != queryLogPaneHeight {
		t.Fatalf("query log height = %d, want %d", model.layout.queryLogHeight, queryLogPaneHeight)
	}
	if got, want := model.layout.workspaceHeight+model.layout.queryLogHeight, model.layout.height-4; got != want {
		t.Fatalf("stacked right pane height = %d, want %d", got, want)
	}
	if strings.Contains(ansi.Strip(model.workspaceView()), "Duration/fetch") {
		t.Fatal("workspace view embeds the query log")
	}
	if !strings.Contains(ansi.Strip(model.queryLogPaneView()), "Message") {
		t.Fatal("query log pane does not render its table")
	}
}

func TestWideLayout_shows_completed_query_in_lower_pane(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 160, 24)
	requestID := model.StartQueryForTest(context.Background())

	// When
	updated, _ := model.Update(querySucceededMsg{requestID: requestID, statement: "SELECT 1", startedAt: time.Now(), result: sqlite.Result{Duration: time.Millisecond}})
	model = updated.(Model)

	// Then
	if !strings.Contains(ansi.Strip(model.queryLogPaneView()), "SELECT 1") {
		t.Fatal("lower query log pane does not show completed query")
	}
}

func TestWideLayout_sizes_browse_table_inside_upper_pane(t *testing.T) {
	// Given
	model := readyModel(t)
	model.SelectedTable = "projects"

	// When
	model = resizeModel(model, 160, 24)
	updated, _ := model.Update(browseTableMsg{table: "projects", result: sqlite.Result{Rows: [][]*string{{stringPointer("first")}}}})
	model = updated.(Model)

	// Then
	// SetHeight(workspaceHeight-8) minus the header row, clamped to one.
	if got, want := model.browse.component.Table.Height(), max(model.layout.workspaceHeight-9, 1); got != want {
		t.Fatalf("browse table height = %d, want %d within upper pane", got, want)
	}
}

func TestWideLayout_shows_two_recent_browse_entries(t *testing.T) {
	// Given
	model := resizeModel(readyModel(t), 160, 24)
	model.SelectedTable = "projects"

	// When
	updated, _ := model.Update(browseTableMsg{table: "projects", page: 0})
	model = updated.(Model)
	model.BrowsePage = 1
	updated, _ = model.Update(browseTableMsg{table: "projects", page: 1})
	model = updated.(Model)

	// Then
	view := ansi.Strip(model.queryLogPaneView())
	if got, want := len(model.queryLog.component.Entries), 2; got != want {
		t.Fatalf("query log entries = %d, want %d", got, want)
	}
	if !strings.Contains(view, cellText("SELECT * FROM \"projects\" LIMIT 25 OFFSET 25")) {
		t.Fatalf("query log pane = %q, want the newest browse statement", view)
	}
}

func TestQueryLog_focuses_with_3_and_navigates_with_jk_gG(t *testing.T) {
	// Given
	model := readyModel(t)
	for index := 0; index < 3; index++ {
		model.appendQueryLog(queryLogEntry{Statement: "SELECT " + string(rune('1'+index))})
	}

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model = updated.(Model)

	// Then
	if model.Focus != focusQueryLog || !model.queryLog.component.Table.Focused() {
		t.Fatal("3 did not focus the query log pane")
	}

	for _, test := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key: tea.KeyPressMsg{Code: 'j', Text: "j"}, want: 1},
		{key: tea.KeyPressMsg{Code: 'G', Text: "G"}, want: 2},
		{key: tea.KeyPressMsg{Code: 'g', Text: "g"}, want: 2},
		{key: tea.KeyPressMsg{Code: 'g', Text: "g"}, want: 0},
		{key: tea.KeyPressMsg{Code: 'k', Text: "k"}, want: 0},
	} {
		updated, _ = model.Update(test.key)
		model = updated.(Model)
		if got := model.queryLog.component.Table.Cursor(); got != test.want {
			t.Fatalf("query log cursor = %d, want %d after %s", got, test.want, test.key.String())
		}
	}
}

func TestQueryLog_n_and_p_change_pages(t *testing.T) {
	// Given
	model := readyModel(t)
	for index := range defaultQueryLogPageSize + 1 {
		model.appendQueryLog(queryLogEntry{Statement: fmt.Sprintf("SELECT %d", index)})
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model = updated.(Model)

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.component.Page, 1; got != want {
		t.Fatalf("query log page = %d, want %d", got, want)
	}
	if got, want := model.queryLog.component.Table.Rows()[0][2], "SELECT 0"; got != want {
		t.Fatalf("next page statement = %q, want %q", got, want)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	model = updated.(Model)

	// Then
	if got, want := model.queryLog.component.Page, 0; got != want {
		t.Fatalf("query log page = %d, want %d", got, want)
	}
	if got, want := model.queryLog.component.Table.Rows()[0][2], "SELECT 25"; got != want {
		t.Fatalf("previous page statement = %q, want %q", got, want)
	}
}
func TestQueryLog_y_copiesSelectedCellImmediately(t *testing.T) {
	// Given
	model := readyModel(t)
	message := strings.Repeat("query message ", 4)
	model.appendQueryLog(queryLogEntry{Statement: "SELECT 42", Message: message})
	updated, _ := model.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model = updated.(Model)
	model.queryLog.component.Column = 4

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	// Then
	if got, want := model.Status, "copied to clipboard"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if command == nil {
		t.Fatal("copy command = nil, want clipboard command")
	}
	if got, ok := model.queryLog.component.SelectedCellText(); !ok || got != message {
		t.Fatalf("copied cell value = %q, want full message %q", got, message)
	}
}

func resolveYankCommand(model Model, command tea.Cmd) Model {
	return driveCommand(model, command)
}
