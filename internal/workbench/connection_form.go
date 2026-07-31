package workbench

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/go-sql-driver/mysql"
)

type connectionDriver string
type mysqlTLSMode string

const (
	driverSQLite     connectionDriver = "sqlite"
	driverMySQL      connectionDriver = "mysql"
	driverPostgreSQL connectionDriver = "postgres"

	mysqlTLSVerify     mysqlTLSMode = "true"
	mysqlTLSSkipVerify mysqlTLSMode = "skip-verify"
	mysqlTLSDisabled   mysqlTLSMode = "false"
)

const (
	connectionFocusRecent = iota
	connectionFocusForm
)

const (
	connectionActionTest    = "Test connection"
	connectionActionConnect = "Connect"
)

type connectionForm struct {
	form         *huh.Form
	confirmation *confirmationDialog
	values       *connectionFormValues
	focus, width int
}

type connectionFormValues struct {
	driver       connectionDriver
	name, target string
	host, port   string
	user, pass   string
	mysqlTLS     mysqlTLSMode
	readOnly     bool
	action       string
}

type connectionTestMsg struct{ err error }

func newConnectionForm() connectionForm {
	form := connectionForm{values: &connectionFormValues{port: "3306", mysqlTLS: mysqlTLSVerify, action: connectionActionTest}, width: 80}
	_ = form.rebuildForm()
	return form
}

func (f *connectionForm) setFocus(index int) tea.Cmd {
	f.focus = index
	if index != connectionFocusForm {
		f.blur()
	}
	return nil
}

func (f *connectionForm) rebuildForm() tea.Cmd {
	fields := []huh.Field{
		huh.NewSelect[connectionDriver]().Key("driver").Title("Driver").Options(
			huh.NewOption("SQLite", driverSQLite),
			huh.NewOption("MySQL", driverMySQL),
			huh.NewOption("PostgreSQL", driverPostgreSQL),
		).Value(&f.values.driver),
		newEditableInput(huh.NewInput().Key("name").Title("Name").Placeholder("Local database").Value(&f.values.name), &f.values.name),
	}
	if f.values.driver != driverSQLite {
		fields = append(fields,
			newEditableInput(huh.NewInput().Key("host").Title("Host*").Placeholder("localhost").Value(&f.values.host).Validate(requiredConnectionHost), &f.values.host),
			newEditableInput(huh.NewInput().Key("port").Title("Port*").Value(&f.values.port).Validate(requiredConnectionPort), &f.values.port),
			newEditableInput(huh.NewInput().Key("username").Title("Username*").Value(&f.values.user).Validate(requiredConnectionUser), &f.values.user),
			newEditableInput(huh.NewInput().Key("password").Title("Password").Value(&f.values.pass).EchoMode(huh.EchoModePassword), &f.values.pass),
			newEditableInput(huh.NewInput().Key("database").Title("Database").Placeholder("Optional").Value(&f.values.target), &f.values.target),
		)
		if f.values.driver == driverMySQL {
			fields = append(fields,
				huh.NewSelect[mysqlTLSMode]().Key("mysql-tls").Title("TLS").Options(
					huh.NewOption("Verify certificate", mysqlTLSVerify),
					huh.NewOption("Encrypt, don't verify certificate", mysqlTLSSkipVerify),
					huh.NewOption("Disable TLS", mysqlTLSDisabled),
				).Value(&f.values.mysqlTLS),
			)
		}
		fields = append(fields,
			huh.NewNote().Title("Privacy").Description("Profiles save connection details. Passwords are stored encrypted at rest. Use ${ENV_VAR} or file:///path to reference secrets without persistence."),
		)
	} else {
		fields = append(fields, newEditableInput(huh.NewInput().Key("target").Title("Target*").Placeholder("path/to/database.db or :memory:").Value(&f.values.target).Validate(requiredConnectionTarget), &f.values.target))
	}
	fields = append(fields,
		huh.NewConfirm().Key("readOnly").Title("Read-Only").Description("Block mutations (INSERT, UPDATE, DELETE, DDL)").Value(&f.values.readOnly),
		newConnectionActionButtons(&f.values.action),
	)
	f.form = newForm(huh.NewGroup(fields...)).WithShowHelp(f.width >= 40).WithWidth(max(f.width, 1))
	return f.form.Init()
}

func (f *connectionForm) updateHuh(message tea.Msg, controller *formModeController) (tea.Cmd, string) {
	if f.confirmation != nil {
		return nil, ""
	}
	driver := f.values.driver
	model, command := f.form.Update(message)
	f.form = model.(*huh.Form)
	if f.values.driver != driver {
		if f.values.driver == driverPostgreSQL && f.values.port == "3306" {
			f.values.port = "5432"
		} else if f.values.driver == driverMySQL && f.values.port == "5432" {
			f.values.port = "3306"
		}
		return f.rebuildForm(), ""
	}
	if f.form.State != huh.StateCompleted {
		return command, ""
	}
	action := f.values.action
	return f.rebuildForm(), action
}

func (f *connectionForm) beginConfirmation() tea.Cmd {
	f.confirmation = yesNoConfirmation("Connect to "+f.connectionName()+"?", "", connectionActionConnect)
	return nil
}

func (f *connectionForm) showValidationError() tea.Cmd {
	return tea.Sequence(f.rebuildForm(), func() tea.Msg { return connectionValidationMsg{} })
}

func (f *connectionForm) focusValidationError() {
	for range 7 {
		field := f.form.GetFocusedField()
		_ = field.Blur()
		if field.Error() != nil {
			return
		}
		_ = f.form.NextField()
	}
}

func (f *connectionForm) blur() {
	if f.form != nil {
		_ = f.form.GetFocusedField().Blur()
	}
}

func (f *connectionForm) focusForm() tea.Cmd {
	if f.form == nil {
		return nil
	}
	return f.form.GetFocusedField().Focus()
}

func (f *connectionForm) setWidth(width int) {
	f.width = max(width, 1)
	if f.form != nil {
		f.form.WithWidth(f.width).WithShowHelp(f.width >= 40)
	}
}

func (f connectionForm) driverName() string {
	switch f.values.driver {
	case driverMySQL:
		return "MySQL"
	case driverPostgreSQL:
		return "PostgreSQL"
	default:
		return "SQLite"
	}
}

func (f connectionForm) targetValue() string {
	pass := resolveSecretRef(f.values.pass)
	if f.values.driver == driverMySQL {
		config := mysql.NewConfig()
		config.User = strings.TrimSpace(f.values.user)
		config.Passwd = pass
		config.Net = "tcp"
		config.Addr = net.JoinHostPort(strings.TrimSpace(f.values.host), strings.TrimSpace(f.values.port))
		config.DBName = strings.TrimSpace(f.values.target)
		tls := f.values.mysqlTLS
		if tls == "" {
			tls = mysqlTLSVerify
		}
		config.TLSConfig = string(tls)
		return config.FormatDSN()
	}
	if f.values.driver == driverPostgreSQL {
		target := &url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(strings.TrimSpace(f.values.user), pass),
			Host:   net.JoinHostPort(strings.TrimSpace(f.values.host), strings.TrimSpace(f.values.port)),
			Path:   strings.TrimSpace(f.values.target),
		}
		target.RawQuery = url.Values{"sslmode": {"verify-full"}}.Encode()
		return target.String()
	}
	return strings.TrimSpace(f.values.target)
}

func (f connectionForm) validate() error {
	if f.values.driver == driverSQLite {
		return requiredConnectionTarget(f.values.target)
	}
	if err := requiredConnectionHost(f.values.host); err != nil {
		return err
	}
	if err := requiredConnectionPort(f.values.port); err != nil {
		return err
	}
	if err := requiredConnectionUser(f.values.user); err != nil {
		return err
	}
	return nil
}

func requiredConnectionTarget(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("target is required")
	}
	return nil
}

func requiredConnectionHost(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("host is required")
	}
	return nil
}

func requiredConnectionPort(value string) error {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

func requiredConnectionUser(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("username is required")
	}
	return nil
}

func (f connectionForm) connectionName() string {
	if name := strings.TrimSpace(f.values.name); name != "" {
		return name
	}
	return f.driverName()
}

func (f connectionForm) View() string {
	if f.form == nil {
		return ""
	}
	return f.form.View()
}
