package app

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/go-sql-driver/mysql"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/connection"
)

func TestConnectionForm_buildsMySQLDSNFromSeparateFields(t *testing.T) {
	// Given
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverMySQL, "2001:db8::1", "3307"
	form.Values.User, form.Values.Pass, form.Values.Target = "alice", "secret", "app"

	// When
	dsn, err := mysql.ParseDSN(form.TargetValue())

	// Then
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.User != "alice" || dsn.Passwd != "secret" || dsn.Addr != "[2001:db8::1]:3307" || dsn.DBName != "app" {
		t.Fatalf("MySQL DSN = %#v, want separate field values", dsn)
	}
	if dsn.TLSConfig != "false" {
		t.Fatalf("MySQL TLS config = %q, want TLS disabled by default", dsn.TLSConfig)
	}
}

func TestConnectionForm_buildsMySQLDSNWithSelectedTLSMode(t *testing.T) {
	// Given
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverMySQL, "127.0.0.1", "3306"
	form.Values.User, form.Values.MySQLTLS = "root", mysqlTLSSkipVerify

	// When
	dsn, err := mysql.ParseDSN(form.TargetValue())

	// Then
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.TLSConfig != "skip-verify" {
		t.Fatalf("MySQL TLS config = %q, want skip-verify", dsn.TLSConfig)
	}
}

func TestConnectionForm_buildsPostgreSQLDSNWithSelectedTLSMode(t *testing.T) {
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverPostgreSQL, "127.0.0.1", "5432"
	form.Values.User, form.Values.Target, form.Values.PostgreSQLTLS = "alice", "app", postgresTLSEncrypt

	target, err := url.Parse(form.TargetValue())
	if err != nil {
		t.Fatalf("parsing PostgreSQL DSN: %v", err)
	}
	if target.Query().Get("sslmode") != "require" {
		t.Fatalf("PostgreSQL sslmode = %q, want require", target.Query().Get("sslmode"))
	}
}
func TestConnectionForm_blankHostAndPortUseDefaults(t *testing.T) {
	for _, test := range []struct {
		name        string
		driver      connectionDriver
		defaultPort string
	}{
		{name: "MySQL", driver: driverMySQL, defaultPort: "3306"},
		{name: "PostgreSQL", driver: driverPostgreSQL, defaultPort: "5432"},
	} {
		t.Run(test.name, func(t *testing.T) {
			form := connection.NewForm()
			form.Values.Driver = test.driver
			form.Values.User = "alice"

			if err := form.Validate(); err != nil {
				t.Fatalf("blank host and port rejected: %v", err)
			}

			want := net.JoinHostPort("localhost", test.defaultPort)
			if test.driver == driverMySQL {
				dsn, err := mysql.ParseDSN(form.TargetValue())
				if err != nil {
					t.Fatalf("parsing MySQL DSN: %v", err)
				}
				if dsn.Addr != want {
					t.Fatalf("MySQL addr = %q, want %q", dsn.Addr, want)
				}
				return
			}
			target, err := url.Parse(form.TargetValue())
			if err != nil {
				t.Fatalf("parsing PostgreSQL target: %v", err)
			}
			if target.Host != want {
				t.Fatalf("PostgreSQL host = %q, want %q", target.Host, want)
			}
		})
	}
}
func TestConnectionForm_rendersDriverSpecificRequiredFields(t *testing.T) {
	for _, test := range []struct {
		name, present, absent string
		driver                connectionDriver
	}{
		{name: "SQLite", driver: driverSQLite, present: "Target*", absent: "Host"},
		{name: "MySQL", driver: driverMySQL, present: "Database", absent: "Database*"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			form := connection.NewForm()
			form.Values.Driver = test.driver
			form.Rebuild()
			_ = form.Huh.Init()

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
		{name: "MySQL port", driver: driverMySQL, set: func(values *connectionFormValues) {
			values.Host, values.Port, values.User = "localhost", "not-a-port", "alice"
		}},
		{name: "MySQL username", driver: driverMySQL, set: func(values *connectionFormValues) {
			values.Host, values.Port = "localhost", "3306"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			form := connection.NewForm()
			form.Values.Driver = test.driver
			if test.set != nil {
				test.set(form.Values)
			}

			// When
			err := form.Validate()

			// Then
			if err == nil {
				t.Fatal("connection validation succeeded for a missing required field")
			}
		})
	}
}

func TestConnectionForm_editsFieldsOnlyInInsertMode(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	_ = model.connection.component.Form.Huh.NextField()

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(Model)

	// Then
	if model.connection.component.Form.Values.Name != "" {
		t.Fatalf("name in normal mode = %q, want empty", model.connection.component.Form.Values.Name)
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
	if model.connection.component.Form.Values.Name != "a" {
		t.Fatalf("name after insert and escape = %q, want a", model.connection.component.Form.Values.Name)
	}
}

func TestConnectionForm_connectRequiresConfirmationAfterValidation(t *testing.T) {
	for _, test := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "ctrl enter", key: tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}},
		{name: "ctrl s", key: tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}},
		{name: "f5", key: tea.KeyPressMsg{Code: tea.KeyF5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", context.Background(), testOpen, false)
			model.connection.component.Form.Focus = connectionFocusForm
			model.connection.component.Form.Values.Target = ":memory:"

			// When
			updated, _ := model.Update(test.key)
			model = updated.(Model)

			// Then
			if model.connection.component.Form.Confirmation == nil {
				t.Fatal("valid connection did not enter confirmation")
			}
			if model.overlay.formMode.Mode != formModeConfirm {
				t.Fatalf("form mode = %v, want confirmation", model.overlay.formMode.Mode)
			}
		})
	}
}

func TestConnectionForm_executeKeysWorkWhileEditing(t *testing.T) {
	for _, test := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "ctrl enter", key: tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}},
		{name: "ctrl s", key: tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}},
		{name: "f5", key: tea.KeyPressMsg{Code: tea.KeyF5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", context.Background(), testOpen, false)
			model.connection.component.Form.Focus = connectionFocusForm
			model.connection.component.Form.Values.Target = ":memory:"
			updated, _ := model.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
			model = updated.(Model)

			// When
			updated, _ = model.Update(test.key)
			model = updated.(Model)

			// Then
			if model.connection.component.Form.Confirmation == nil || model.overlay.formMode.Mode != formModeConfirm {
				t.Fatalf("execute key did not enter confirmation: confirmation=%t mode=%v", model.connection.component.Form.Confirmation != nil, model.overlay.formMode.Mode)
			}
		})
	}
}

func TestConnectionForm_actionButtonsExecuteFromNormalAndInsertModes(t *testing.T) {
	for _, test := range []struct {
		name    string
		action  string
		editing bool
	}{
		{name: "test normal", action: connectionActionTest},
		{name: "connect normal", action: connectionActionConnect},
		{name: "test insert", action: connectionActionTest, editing: true},
		{name: "connect insert", action: connectionActionConnect, editing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			model := New("", context.Background(), testOpen, false)
			model.connection.component.Form.Focus = connectionFocusForm
			model.connection.component.Form.Values.Target = ":memory:"
			for range 4 {
				_ = model.connection.component.Form.Huh.NextField()
			}
			if got := model.connection.component.Form.Huh.GetFocusedField().GetKey(); got != "action" {
				t.Fatalf("focused field = %q, want action", got)
			}
			if test.action == connectionActionConnect {
				updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
				model = updated.(Model)
			}
			model.connection.component.Form.Values.Target = ":memory:"
			if test.editing {
				model.overlay.formMode.BeginHuh(model.connection.component.Form.FocusForm())
			}

			// When
			updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			model = updated.(Model)
			if command == nil {
				t.Fatal("action button returned no command")
			}
			if test.action == connectionActionTest {
				message := command()
				if _, ok := message.(connectionTestMsg); !ok {
					t.Fatalf("action button message = %T, want connection test", message)
				}
			} else if model.State != stateOpening {
				t.Fatalf("connection state = %v, want opening", model.State)
			}

			// Then
			if model.overlay.formMode.Mode != formModeNormal {
				t.Fatalf("form mode = %v, want normal", model.overlay.formMode.Mode)
			}
			if model.connection.component.Form.Values.Action != test.action {
				t.Fatalf("selected action = %q, want %q", model.connection.component.Form.Values.Action, test.action)
			}
		})
	}
}

// TestConnectionForm_actionButtonsHighlightOnFocus guards the focus cue of
// the Action buttons: the selected button renders with the selection color
// (primary) while blurred and shifts to the focus color while the field is
// focused — the same convention huh applies to its own options and buttons.
// TestConnectionForm_actionButtonsSameWidth guards the equal-width Action
// buttons: both render at connectionActionWidth() in the form view, with the
// shorter "Connect" label centered into the wider "Test connection" button.
func TestConnectionForm_actionButtonsSameWidth(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	model.connection.component.Form.Values.Target = ":memory:"
	model = resolveConnectionCommand(model, model.connection.component.Form.Huh.Init())
	view := ansi.Strip(model.connection.component.Form.Huh.View())

	width := connectionActionWidth()
	connectPad := (width - lipgloss.Width(connectionActionStyle.Render(connectionActionConnect))) / 2

	found := false
	for _, line := range strings.Split(view, "\n") {
		testIdx := strings.Index(line, connectionActionTest)
		connectIdx := strings.LastIndex(line, connectionActionConnect)
		if testIdx < 0 || connectIdx < 0 {
			continue
		}
		found = true
		// Each button spans its label, the style padding (1 per side), and
		// for Connect the centering padding (connectPad per side).
		if testSpan := len(connectionActionTest) + 2; testSpan != width {
			t.Fatalf("Test button span = %d, want %d", testSpan, width)
		}
		if connectSpan := len(connectionActionConnect) + 2 + 2*connectPad; connectSpan != width {
			t.Fatalf("Connect button span = %d, want %d", connectSpan, width)
		}
	}
	if !found {
		t.Fatal("action buttons not found in connection form view")
	}
}

func TestConnectionForm_actionButtonsHighlightOnFocus(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	model.connection.component.Form.Values.Target = ":memory:"
	model = resolveConnectionCommand(model, model.connection.component.Form.Huh.Init())

	highlighted := func(view string, style lipgloss.Style) []string {
		var got []string
		if strings.Contains(view, connectionActionRender(style, connectionActionTest)) {
			got = append(got, connectionActionTest)
		}
		if strings.Contains(view, connectionActionRender(style, connectionActionConnect)) {
			got = append(got, connectionActionConnect)
		}
		return got
	}

	// Action field starts blurred (focus is on Driver): the selected button
	// shows the teal selection style, not the focus style.
	view := model.connection.component.Form.Huh.View()
	if got := highlighted(view, connectionActionSelectedStyle); !reflect.DeepEqual(got, []string{connectionActionTest}) {
		t.Fatalf("blurred action field highlighted %v, want %q", got, connectionActionTest)
	}
	if got := highlighted(view, connectionActionFocusedStyle); len(got) != 0 {
		t.Fatalf("blurred action field used focus style on %v, want none", got)
	}

	// Navigate onto the action field: the selection shifts to the focus color.
	for range 4 {
		_ = model.connection.component.Form.Huh.NextField()
	}
	view = model.connection.component.Form.Huh.View()
	if got := highlighted(view, connectionActionFocusedStyle); !reflect.DeepEqual(got, []string{connectionActionTest}) {
		t.Fatalf("focused action field highlighted %v, want %q", got, connectionActionTest)
	}
	if got := highlighted(view, connectionActionSelectedStyle); len(got) != 0 {
		t.Fatalf("focused action field kept selection style on %v, want none", got)
	}

	// Switch the selection: the focus highlight moves to Connect.
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	view = model.connection.component.Form.Huh.View()
	if got := highlighted(view, connectionActionFocusedStyle); !reflect.DeepEqual(got, []string{connectionActionConnect}) {
		t.Fatalf("after h/l highlighted %v, want %q", got, connectionActionConnect)
	}

	// Leave the field: the highlight returns to the selection color, keeping
	// the chosen action.
	_ = model.connection.component.Form.Huh.PrevField()
	view = model.connection.component.Form.Huh.View()
	if got := highlighted(view, connectionActionSelectedStyle); !reflect.DeepEqual(got, []string{connectionActionConnect}) {
		t.Fatalf("blurred action field highlighted %v, want %q", got, connectionActionConnect)
	}
}

// TestConnectionForm_successfulTestPersistsProfile guards the removed Save
// button: a successful Test records the form's current credentials, and the
// recorded SQLite target is the tested one — never a previously connected
// m.Target.
func TestConnectionForm_successfulTestPersistsProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	model.connection.component.Form.Values.Target = ":memory:"
	_ = model.connection.component.Form.Rebuild()
	_ = model.connection.component.Form.Huh.Init()
	model.Target = "/stale/old.db" // a previous connect must not leak in

	for range 4 {
		_ = model.connection.component.Form.Huh.NextField()
	}
	if got := model.connection.component.Form.Huh.GetFocusedField().GetKey(); got != "action" {
		t.Fatalf("focused field = %q, want action", got)
	}

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if command == nil {
		t.Fatal("Test action returned no command")
	}
	test, ok := command().(connectionTestMsg)
	if !ok || test.err != nil {
		t.Fatalf("test message = %#v, want successful connection test", command())
	}
	updated, _ = model.Update(test)
	model = updated.(Model)

	if len(model.connection.component.Profiles) != 1 {
		t.Fatalf("recent profiles = %#v, want the tested credentials recorded", model.connection.component.Profiles)
	}
	if got := model.connection.component.Profiles[0].Target; got != ":memory:" {
		t.Fatalf("recorded target = %q, want the tested target, not stale m.Target", got)
	}
}

func TestConnectionForm_rejectsInvalidConnectionWithoutClearingValues(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	model.connection.component.Form.Values.Driver, model.connection.component.Form.Values.Host = driverMySQL, ""
	model.connection.component.Form.Values.Port, model.connection.component.Form.Values.User, model.connection.component.Form.Values.Target = "not-a-port", "alice", "app"

	// When
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)
	model = resolveConnectionCommand(model, command)

	// Then
	if model.connection.component.Form.Confirmation != nil || model.State != stateConnection {
		t.Fatal("invalid connection advanced to an action")
	}
	if model.connection.component.Form.Values.Target != "app" || model.connection.component.Form.Values.User != "alice" {
		t.Fatal("invalid connection cleared populated fields")
	}
}

func TestConnectionForm_testsSQLiteConnection(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "Scratch", ":memory:"

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
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "Scratch", ":memory:"

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
	}, false)
	model.connection.component.Form.Values.Driver, model.connection.component.Form.Values.Host = driverMySQL, "localhost"
	model.connection.component.Form.Values.Port, model.connection.component.Form.Values.Target = "3306", "app"
	model.connection.component.Form.Values.User = "alice"

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

func TestConnectionForm_buildsPostgreSQLURLFromSeparateFields(t *testing.T) {
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverPostgreSQL, "2001:db8::1", "5433"
	form.Values.User, form.Values.Pass, form.Values.Target = "alice", "secret", "app data"

	target, err := url.Parse(form.TargetValue())
	if err != nil {
		t.Fatalf("parsing PostgreSQL URL: %v", err)
	}
	password, hasPassword := target.User.Password()
	if target.Scheme != "postgres" || target.User.Username() != "alice" || !hasPassword || password != "secret" || target.Host != "[2001:db8::1]:5433" || target.Path != "/app data" {
		t.Fatalf("PostgreSQL URL = %#v, want separate field values", target)
	}
}

func TestConnectionForm_opensPostgreSQLConnection(t *testing.T) {
	var openedTarget string
	model := New("", context.Background(), func(_ context.Context, target string) (sharedsql.Opened, error) {
		openedTarget = target
		return sharedsql.Opened{}, nil
	}, false)
	model.connection.component.Form.Values.Driver, model.connection.component.Form.Values.Host = driverPostgreSQL, "localhost"
	model.connection.component.Form.Values.Port, model.connection.component.Form.Values.Target = "5432", "app"
	model.connection.component.Form.Values.User = "alice"

	updated, command := model.openConnection()
	model = updated.(Model)
	if command != nil {
		_ = command()
	}

	if model.State != stateOpening || command == nil || !strings.HasPrefix(openedTarget, "postgres:") {
		t.Fatalf("PostgreSQL open = state %v, command %v, target %q", model.State, command, openedTarget)
	}
}

func TestConnectionForm_recordsRemoteConnectionProfile(t *testing.T) {
	// Given
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Profiles = nil
	model.connection.component.Form.Values.Driver, model.connection.component.Form.Values.Target = driverMySQL, "app"
	model.connection.component.Form.Values.Host, model.connection.component.Form.Values.Port, model.connection.component.Form.Values.User = "db.example.test", "3307", "alice"
	model.connection.component.Form.Values.Pass = "secret"
	model.connection.component.Form.Values.MySQLTLS = mysqlTLSSkipVerify

	// When
	model.recordConnection("")

	// Then
	if len(model.connection.component.Profiles) != 1 {
		t.Fatalf("recent MySQL profiles = %#v, want one", model.connection.component.Profiles)
	}
	profile := model.connection.component.Profiles[0]
	if profile.Driver != driverMySQL || profile.Host != "db.example.test" || profile.Port != "3307" || profile.User != "alice" || profile.Target != "app" || profile.MySQLTLS != mysqlTLSSkipVerify {
		t.Fatalf("remote profile = %#v, want non-secret connection fields", profile)
	}
	if profile.Pass != "secret" {
		t.Fatalf("remote profile password = %q, want secret", profile.Pass)
	}
}

func TestConnectionForm_recordsSQLiteProfileWithoutRemoteFields(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target = "Scratch", ":memory:"

	// When
	model.recordConnection("")

	// Then
	profile := model.connection.component.Profiles[0]
	if profile.Host != "" || profile.Port != "" || profile.User != "" {
		t.Fatalf("SQLite profile = %#v, want no remote connection fields", profile)
	}
}

func TestConnectionForm_driverSwitchInitializesRebuiltHuhForm(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	model = resolveConnectionCommand(model, model.connection.component.Form.Huh.Init())
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
	if model.connection.component.Form.Values.Driver != driverMySQL || !strings.Contains(model.connection.component.Form.View(), "Host") {
		t.Fatalf("driver switch form = driver %v, view %q", model.connection.component.Form.Values.Driver, model.connection.component.Form.View())
	}
}

func TestConnectionForm_f5AllowsBlankMySQLDatabase(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	model.connection.component.Form.Values.Driver = driverMySQL
	model.connection.component.Form.Values.Host, model.connection.component.Form.Values.Port, model.connection.component.Form.Values.User = "localhost", "3306", "alice"
	_ = model.connection.component.Form.Rebuild()

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	// Then
	if model.connection.component.Form.Confirmation == nil {
		t.Fatal("blank MySQL database did not reach connection confirmation")
	}
}

func TestConnectionForm_f5AllowsBlankPostgreSQLDatabase(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	model.connection.component.Form.Values.Driver = driverPostgreSQL
	model.connection.component.Form.Values.Host, model.connection.component.Form.Values.Port, model.connection.component.Form.Values.User = "localhost", "5432", "alice"
	_ = model.connection.component.Form.Rebuild()

	// When
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	model = updated.(Model)

	// Then
	if model.connection.component.Form.Confirmation == nil {
		t.Fatal("blank PostgreSQL database did not reach connection confirmation")
	}
}

func TestConnectionForm_completionSequencesInitBeforeConnectionAction(t *testing.T) {
	// Given
	form := connection.NewForm()
	form.Values.Target = ":memory:"
	init := form.Rebuild()

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
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Form.Focus = connectionFocusForm
	model.connection.component.Form.Values.Name = "Missing"
	model.connection.component.Form.Values.Target = t.TempDir() + "/missing.db"

	// When
	updated, command := model.openConnection()
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)

	// Then
	if model.State != stateConnection || model.connection.component.Form.Values.Name != "Missing" || model.connection.component.Form.Values.Target == "" {
		t.Fatalf("rejected connection = state %v, name %q, target %q", model.State, model.connection.component.Form.Values.Name, model.connection.component.Form.Values.Target)
	}
}

func TestConnectionForm_resolvesEnvVarPassword(t *testing.T) {
	t.Setenv("PERK_TEST_DB_PASS", "resolved-secret")
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverMySQL, "127.0.0.1", "3306"
	form.Values.User, form.Values.Pass, form.Values.Target = "admin", "${PERK_TEST_DB_PASS}", "app"
	dsn, err := mysql.ParseDSN(form.TargetValue())
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.Passwd != "resolved-secret" {
		t.Fatalf("MySQL DSN password = %q, want resolved-secret", dsn.Passwd)
	}
}

func TestConnectionForm_resolvesFilePassword(t *testing.T) {
	dir := t.TempDir()
	passFile := filepath.Join(dir, "db_pass")
	if err := os.WriteFile(passFile, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatalf("writing password file: %v", err)
	}
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverMySQL, "127.0.0.1", "3306"
	form.Values.User, form.Values.Pass, form.Values.Target = "admin", "file://"+passFile, "app"
	dsn, err := mysql.ParseDSN(form.TargetValue())
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.Passwd != "file-secret" {
		t.Fatalf("MySQL DSN password = %q, want file-secret", dsn.Passwd)
	}
}

func TestConnectionForm_resolvesPostgresEnvVarPassword(t *testing.T) {
	t.Setenv("PG_PASS", "pg-resolved")
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverPostgreSQL, "db.example.test", "5432"
	form.Values.User, form.Values.Pass, form.Values.Target = "analyst", "${PG_PASS}", "analytics"
	target := form.TargetValue()
	if !strings.Contains(target, "pg-resolved") {
		t.Fatalf("PostgreSQL target = %q, want resolved password", target)
	}
	if strings.Contains(target, "${PG_PASS}") {
		t.Fatalf("PostgreSQL target = %q, must not contain unresolved reference", target)
	}
}

func TestConnectionForm_doesNotResolveLiteralPassword(t *testing.T) {
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverMySQL, "127.0.0.1", "3306"
	form.Values.User, form.Values.Pass, form.Values.Target = "admin", "literal-secret", "app"
	dsn, err := mysql.ParseDSN(form.TargetValue())
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.Passwd != "literal-secret" {
		t.Fatalf("MySQL DSN password = %q, want literal-secret", dsn.Passwd)
	}
}

func TestConnectionForm_resolvesMissingEnvVarToEmpty(t *testing.T) {
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverMySQL, "127.0.0.1", "3306"
	form.Values.User, form.Values.Pass, form.Values.Target = "admin", "${MISSING_VAR}", "app"
	dsn, err := mysql.ParseDSN(form.TargetValue())
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.Passwd != "" {
		t.Fatalf("MySQL DSN password = %q, want empty for missing env var", dsn.Passwd)
	}
}
