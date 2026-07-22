package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/go-sql-driver/mysql"
)

func TestConnectionForm_buildsMySQLDSNFromSeparateFields(t *testing.T) {
	form := newConnectionForm()
	form.driver = driverMySQL
	form.host.SetValue("2001:db8::1")
	form.port.SetValue("3307")
	form.user.SetValue("alice")
	form.pass.SetValue("secret")
	form.target.SetValue("app")

	dsn, err := mysql.ParseDSN(form.targetValue())
	if err != nil {
		t.Fatalf("parsing MySQL DSN: %v", err)
	}
	if dsn.User != "alice" || dsn.Passwd != "secret" || dsn.Addr != "[2001:db8::1]:3307" || dsn.DBName != "app" {
		t.Fatalf("MySQL DSN = %#v, want separate field values", dsn)
	}
}

func TestConnectionForm_alignsInputLabels(t *testing.T) {
	form := newConnectionForm()
	for _, test := range []struct {
		label  string
		prompt string
	}{
		{label: "Name", prompt: form.name.Prompt},
		{label: "Target", prompt: form.target.Prompt},
		{label: "Host", prompt: form.host.Prompt},
		{label: "Port", prompt: form.port.Prompt},
		{label: "Username", prompt: form.user.Prompt},
		{label: "Password", prompt: form.pass.Prompt},
	} {
		t.Run(test.label, func(t *testing.T) {
			want := test.label + strings.Repeat(" ", formLabelWidth-len(test.label)) + formFieldGap
			if test.prompt != want {
				t.Fatalf("prompt = %q, want %q", test.prompt, want)
			}
		})
	}
}

func TestConnectionForm_showsModeAtPaneBottom(t *testing.T) {
	for _, test := range []struct {
		mode connectionFormMode
		want string
	}{
		{mode: connectionFormNormal, want: "NORMAL"},
		{mode: connectionFormInsert, want: "INSERT"},
	} {
		t.Run(test.want, func(t *testing.T) {
			// Given
			model := New("", Open(context.Background()))
			model.connection.mode = test.mode

			// When
			view := model.connectionPaneView(12)
			lines := strings.Split(view, "\n")

			// Then
			if strings.Contains(view, "Tab to a control") {
				t.Fatal("connection pane retained inline help")
			}
			if got := lines[len(lines)-1]; !strings.Contains(got, test.want) {
				t.Fatalf("last pane line = %q, want mode %q", got, test.want)
			}
			if len(lines) != 12 {
				t.Fatalf("pane line count = %d, want 12", len(lines))
			}
		})
	}
}

func TestConnectionForm_showsMySQLControls(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.setFocus(connectionFocusDriver)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model = updated.(Model)
	view := model.connectionView()
	for _, label := range []string{"Host", "Port", "Username", "Password", "Database"} {
		if !strings.Contains(view, label) {
			t.Fatalf("MySQL connection view = %q, missing %q", view, label)
		}
	}

	for _, want := range []int{connectionFocusName, connectionFocusHost, connectionFocusPort, connectionFocusUsername, connectionFocusPassword, connectionFocusTarget, connectionFocusTest, connectionFocusConnect} {
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		model = updated.(Model)
		if model.connection.focus != want {
			t.Fatalf("connection focus = %d, want %d", model.connection.focus, want)
		}
	}
}

func TestConnectionForm_driverSwitchesWithDirectionalKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyLeft},
		{Code: tea.KeyRight},
		{Code: 'h', Text: "h"},
		{Code: 'l', Text: "l"},
	} {
		model := New("", Open(context.Background()))
		model.connection.setFocus(connectionFocusDriver)

		updated, _ := model.Update(key)
		model = updated.(Model)
		if model.connection.driver != driverMySQL {
			t.Fatalf("driver after %q = %d, want MySQL", key.String(), model.connection.driver)
		}
	}
}

func TestConnectionForm_allowsQInMySQLHost(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.driver = driverMySQL
	model.connection.setFocus(connectionFocusHost)
	model.connection.enterInsertMode()

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(Model)
	if model.connection.host.Value() != "q" {
		t.Fatalf("host = %q, want q", model.connection.host.Value())
	}
}

func TestConnectionForm_editsFieldsOnlyInInsertMode(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: tea.KeyEnter},
	} {
		t.Run(key.String(), func(t *testing.T) {
			model := New("", Open(context.Background()))
			model.connection.setFocus(connectionFocusName)

			updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
			model = updated.(Model)
			if model.connection.name.Value() != "" {
				t.Fatalf("name in normal mode = %q, want empty", model.connection.name.Value())
			}

			updated, _ = model.Update(key)
			model = updated.(Model)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
			model = updated.(Model)
			if model.connection.name.Value() != "a" {
				t.Fatalf("name in insert mode = %q, want a", model.connection.name.Value())
			}

			updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			model = updated.(Model)
			updated, _ = model.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
			model = updated.(Model)
			if model.connection.name.Value() != "a" {
				t.Fatalf("name after escape = %q, want a", model.connection.name.Value())
			}
		})
	}
}

func TestConnectionForm_navigatesNormalModeWithVimKeys(t *testing.T) {
	for _, test := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key: tea.KeyPressMsg{Code: 'j', Text: "j"}, want: connectionFocusTarget},
		{key: tea.KeyPressMsg{Code: 'k', Text: "k"}, want: connectionFocusDriver},
	} {
		t.Run(test.key.String(), func(t *testing.T) {
			// Given
			model := New("", Open(context.Background()))
			model.connection.setFocus(connectionFocusName)

			// When
			updated, _ := model.Update(test.key)
			model = updated.(Model)

			// Then
			if model.connection.focus != test.want {
				t.Fatalf("connection focus = %d, want %d", model.connection.focus, test.want)
			}
		})
	}
}

func TestConnectionForm_highlightsOnlyFocusedLabelInNormalMode(t *testing.T) {
	// Given
	model := New("", Open(context.Background()))
	model.connection.setFocus(connectionFocusName)
	model.connection.name.SetValue("value")

	// When
	got := model.connectionInputView(connectionFocusName, model.connection.name)

	// Then
	if model.connection.name.Focused() {
		t.Fatal("name input is focused in normal mode")
	}
	wantInput := model.connection.name
	wantInput.Prompt = "Name" + strings.Repeat(" ", formLabelWidth-len("Name")) + formFieldGap
	styles := wantInput.Styles()
	styles.Focused.Prompt = headerStyle.Padding(0, 0)
	styles.Blurred.Prompt = headerStyle.Padding(0, 0)
	wantInput.SetStyles(styles)
	want := wantInput.View()
	if got != want {
		t.Fatalf("focused connection input = %q, want %q", got, want)
	}
}

func TestConnectionForm_testsSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.name.SetValue("Scratch")
	model.connection.target.SetValue(":memory:")

	message := model.testConnection()()
	updated, _ := model.Update(message)
	model = updated.(Model)

	if model.Status != "connection test succeeded: Scratch" {
		t.Fatalf("connection status = %q, want successful test", model.Status)
	}
}

func TestConnectionForm_opensSQLiteConnection(t *testing.T) {
	model := New("", Open(context.Background()))
	model.connection.name.SetValue("Scratch")
	model.connection.target.SetValue(":memory:")

	updated, command := model.openConnection()
	model = updated.(Model)
	if model.State != stateOpening {
		t.Fatalf("model state = %v, want opening", model.State)
	}
	if command == nil {
		t.Fatal("open connection command = nil")
	}

	updated, _ = model.Update(command())
	model = updated.(Model)
	if model.State != stateReady {
		t.Fatalf("model state = %v, want ready", model.State)
	}
	if model.Database == nil {
		t.Fatal("model service = nil, want opened service")
	}
	if model.Status != "ready: Scratch" {
		t.Fatalf("connection status = %q, want connection name", model.Status)
	}
	t.Cleanup(func() {
		if err := model.Database.Close(); err != nil {
			t.Errorf("closing connection: %v", err)
		}
	})
}

func TestConnectionForm_opensMySQLConnection(t *testing.T) {
	var openedTarget string
	model := New("", databaseOpener{
		ctx: context.Background(),
		command: func(target string) tea.Cmd {
			openedTarget = target
			return nil
		},
	})
	model.connection.driver = driverMySQL
	model.connection.host.SetValue("localhost")
	model.connection.port.SetValue("3306")
	model.connection.target.SetValue("app")

	updated, command := model.openConnection()
	model = updated.(Model)
	if model.State != stateOpening {
		t.Fatalf("model state = %v, want opening", model.State)
	}
	if command != nil {
		t.Fatal("open command = non-nil, want nil from test opener")
	}
	if !strings.HasPrefix(openedTarget, "mysql:") {
		t.Fatalf("opened target = %q, want mysql DSN prefix", openedTarget)
	}
}
