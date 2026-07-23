package workbench

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/go-sql-driver/mysql"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

func TestConnectionForm_buildsMySQLDSNFromSeparateFields(t *testing.T) {
	// Given
	form := newConnectionForm()
	form.values.driver, form.values.host, form.values.port = driverMySQL, "2001:db8::1", "3307"
	form.values.user, form.values.pass, form.values.target = "alice", "secret", "app"

	// When
	dsn, err := mysql.ParseDSN(form.targetValue())

	// Then
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.User != "alice" || dsn.Passwd != "secret" || dsn.Addr != "[2001:db8::1]:3307" || dsn.DBName != "app" {
		t.Fatalf("MySQL DSN = %#v, want separate field values", dsn)
	}
}

func TestConnectionForm_rendersDriverSpecificRequiredFields(t *testing.T) {
	for _, test := range []struct {
		name, present, absent string
		driver                connectionDriver
	}{
		{name: "SQLite", driver: driverSQLite, present: "Target*", absent: "Host*"},
		{name: "MySQL", driver: driverMySQL, present: "Database*", absent: "Target*"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			form := newConnectionForm()
			form.values.driver = test.driver
			form.rebuildForm()
			_ = form.form.Init()

			// When
			view := form.View()

			// Then
			if !strings.Contains(view, test.present) || strings.Contains(view, test.absent) {
				t.Fatalf("connection view = %q, want %q and not %q", view, test.present, test.absent)
			}
		})
	}
}

func TestConnectionForm_validatesRequiredDriverFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		driver connectionDriver
		set    func(*connectionFormValues)
	}{
		{name: "SQLite target", driver: driverSQLite},
		{name: "MySQL host", driver: driverMySQL, set: func(values *connectionFormValues) { values.target, values.port, values.user = "app", "3306", "alice" }},
		{name: "MySQL port", driver: driverMySQL, set: func(values *connectionFormValues) {
			values.target, values.host, values.port, values.user = "app", "localhost", "", "alice"
		}},
		{name: "MySQL username", driver: driverMySQL, set: func(values *connectionFormValues) {
			values.target, values.host, values.port = "app", "localhost", "3306"
		}},
		{name: "MySQL database", driver: driverMySQL, set: func(values *connectionFormValues) {
			values.host, values.port, values.user = "localhost", "3306", "alice"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			form := newConnectionForm()
			form.values.driver = test.driver
			if test.set != nil {
				test.set(form.values)
			}

			// When
			err := form.validate()

			// Then
			if err == nil {
				t.Fatal("connection validation succeeded for a missing required field")
			}
		})
	}
}

func TestConnectionForm_editsFieldsOnlyInInsertMode(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.connection.focus = connectionFocusForm
	_ = model.connection.form.NextField()

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)

	// Then
	if model.connection.values.name != "" {
		t.Fatalf("name in normal mode = %q, want empty", model.connection.values.name)
	}

	// When
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	model = updated.(Model)

	// Then
	if model.connection.values.name != "a" {
		t.Fatalf("name after insert and escape = %q, want a", model.connection.values.name)
	}
}

func TestConnectionForm_connectRequiresConfirmationAfterValidation(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.connection.focus = connectionFocusForm
	model.connection.values.target = ":memory:"

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	// Then
	if model.connection.confirmation == nil {
		t.Fatal("valid connection did not enter confirmation")
	}
	if model.formMode.mode != formModeConfirm {
		t.Fatalf("form mode = %v, want confirmation", model.formMode.mode)
	}
}

func TestConnectionForm_rejectsInvalidConnectionWithoutClearingValues(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.connection.focus = connectionFocusForm
	model.connection.values.driver, model.connection.values.host = driverMySQL, ""
	model.connection.values.port, model.connection.values.user, model.connection.values.target = "3306", "alice", "app"

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	model = resolveConnectionCommand(model, command)

	// Then
	if model.connection.confirmation != nil || model.State != stateConnection {
		t.Fatal("invalid connection advanced to an action")
	}
	if model.connection.values.target != "app" || model.connection.values.user != "alice" {
		t.Fatal("invalid connection cleared populated fields")
	}
}

func TestConnectionForm_testsSQLiteConnection(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.connection.values.name, model.connection.values.target = "Scratch", ":memory:"

	// When
	message := model.testConnection()()
	updated, _ := model.Update(message)
	model = updated.(Model)

	// Then
	if model.Status != "connection test succeeded: Scratch" {
		t.Fatalf("connection status = %q, want successful test", model.Status)
	}
}

func TestConnectionForm_opensSQLiteConnection(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.connection.values.name, model.connection.values.target = "Scratch", ":memory:"

	// When
	updated, command := model.openConnection()
	model = updated.(Model)
	if command == nil {
		t.Fatal("open connection command = nil")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)

	// Then
	if model.State != stateReady || model.Database == nil || model.Status != "ready: Scratch" {
		t.Fatalf("SQLite open = state %v, database %v, status %q", model.State, model.Database, model.Status)
	}
	t.Cleanup(func() {
		if err := model.Database.Close(); err != nil {
			t.Errorf("closing connection: %v", err)
		}
	})
}

func TestConnectionForm_opensMySQLConnection(t *testing.T) {
	// Given
	var openedTarget string
	model := New("", context.Background(), func(_ context.Context, target string) (sharedsql.Opened, error) {
		openedTarget = target
		return sharedsql.Opened{}, nil
	})
	model.connection.values.driver, model.connection.values.host = driverMySQL, "localhost"
	model.connection.values.port, model.connection.values.target = "3306", "app"
	model.connection.values.user = "alice"

	// When
	updated, command := model.openConnection()
	model = updated.(Model)
	if command != nil {
		_ = command()
	}

	// Then
	if model.State != stateOpening || command == nil || !strings.HasPrefix(openedTarget, "mysql:") {
		t.Fatalf("MySQL open = state %v, command %v, target %q", model.State, command, openedTarget)
	}
}

func TestConnectionForm_doesNotRecordMySQLConnections(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.recentConnections = nil
	model.connection.values.driver, model.connection.values.target = driverMySQL, "app"

	// When
	model.recordConnection()

	// Then
	if len(model.recentConnections) != 0 {
		t.Fatalf("recent MySQL connections = %#v, want none", model.recentConnections)
	}
}

func TestConnectionForm_driverSwitchInitializesRebuiltHuhForm(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.connection.focus = connectionFocusForm
	model = resolveConnectionCommand(model, model.connection.form.Init())
	updated, command := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	model = updated.(Model)
	model = resolveConnectionCommand(model, command)

	// When
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	message := command()
	value := reflect.ValueOf(message)
	if value.Kind() != reflect.Slice || value.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
		t.Fatalf("driver switch command message = %T, want rebuilt Huh form initialization sequence", message)
	}
	model = resolveConnectionMessage(model, message, 16)

	// Then
	if model.connection.values.driver != driverMySQL || !strings.Contains(model.connection.View(), "Host*") {
		t.Fatalf("driver switch form = driver %v, view %q", model.connection.values.driver, model.connection.View())
	}
}

func TestConnectionForm_f5ShowsDatabaseErrorWhenOnlyMySQLDatabaseIsBlank(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.connection.focus = connectionFocusForm
	model.connection.values.driver = driverMySQL
	model.connection.values.host, model.connection.values.port, model.connection.values.user = "localhost", "3306", "alice"
	_ = model.connection.rebuildForm()

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	model = resolveConnectionCommand(model, command)

	// Then
	field := model.connection.form.GetFocusedField()
	if field.GetKey() != "database" || field.Error() == nil {
		t.Fatalf("MySQL database validation = field %q, error %v, want database/error", field.GetKey(), field.Error())
	}
}

func TestConnectionForm_completionSequencesInitBeforeConnectionAction(t *testing.T) {
	// Given
	form := newConnectionForm()
	form.values.target = ":memory:"
	init := form.rebuildForm()

	// When
	command := sequenceConnectionAction(init, connectionActionTest)
	message := command()

	// Then
	value := reflect.ValueOf(message)
	if value.Kind() != reflect.Slice || value.Type().Elem() != reflect.TypeFor[tea.Cmd]() || value.Len() != 2 {
		t.Fatalf("completion command = %T, want init/action sequence", message)
	}
}

func TestConnectionForm_retainsRejectedSQLiteConnection(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen)
	model.connection.focus = connectionFocusForm
	model.connection.values.name = "Missing"
	model.connection.values.target = t.TempDir() + "/missing.db"

	// When
	updated, command := model.openConnection()
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)

	// Then
	if model.State != stateConnection || model.connection.values.name != "Missing" || model.connection.values.target == "" {
		t.Fatalf("rejected connection = state %v, name %q, target %q", model.State, model.connection.values.name, model.connection.values.target)
	}
}
