package connection

import (
	"errors"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Event is a typed request from the connection component to the root
// shell. The root performs database opening/testing and profile
// persistence side effects; the component only edits profiles and
// requests actions.
type Event interface{ isEvent() }

// OpenRequested asks the root to open the given target with the current
// form profile. Reconnect is reserved for sidebar database switches.
type OpenRequested struct {
	Target    string
	Profile   profile.Profile
	Reconnect bool
}

func (OpenRequested) isEvent() {}

// TestRequested asks the root to test-connect the given target.
type TestRequested struct{ Target string }

func (TestRequested) isEvent() {}

// ProfilesChanged reports that the component's profile list changed; the
// root re-reads it through the component API.
type ProfilesChanged struct{ Profiles []profile.Profile }

func (ProfilesChanged) isEvent() {}

// StatusChanged is the shared status-line event (uikit.StatusChanged).
type StatusChanged = uikit.StatusChanged

// selectOptions lists the option labels of the Driver and TLS selects in
// render order. The TLS labels are shared; the bound values differ per
// driver (see ApplySelectOption).
var selectOptions = map[string][]string{
	"driver": {"SQLite", "MySQL", "PostgreSQL"},
	"tls":    {"Verify certificate", "Encrypt, don't verify certificate", "Don't encrypt"},
}

// Form is the connection profile form: the huh form, its bound values,
// the optional confirmation dialog, and the pane geometry.
type Form struct {
	Huh          *huh.Form
	Confirmation *uikit.ConfirmationDialog
	Values       *FormValues
	Focus, Width int
	// Height is the pane body height the form viewport is clipped to; huh
	// scrolls its group viewport to the focused field (see SetHeight).
	Height int
}

// FormValues holds the editable form fields. ID is the opaque profile
// scope: never displayed or user-edited.
type FormValues struct {
	ID            string
	Driver        Driver
	Name, Target  string
	Host, Port    string
	User, Pass    string
	MySQLTLS      MySQLTLS
	PostgreSQLTLS PostgreSQLTLS
	ReadOnly      bool
	Action        string
}

// NewForm builds a fresh connection form with the disabled TLS defaults.
func NewForm() Form {
	form := Form{Values: &FormValues{MySQLTLS: MySQLTLSDisabled, PostgreSQLTLS: PostgreSQLTLSDisabled, Action: ActionTest}, Width: 80}
	_ = form.Rebuild()
	return form
}

// SetFocus switches the pane focus; leaving the form blurs its field.
func (f *Form) SetFocus(index int) tea.Cmd {
	f.Focus = index
	if index != FocusForm {
		f.Blur()
	}
	return nil
}

// fieldTitles lists the rendered titles of every connection form field in
// render order; the layout depends on the selected driver.
func (f Form) FieldTitles() []string {
	if f.Values.Driver == DriverSQLite {
		return []string{"Driver", "Name", "Target*", "Read-Only", "Action"}
	}
	return []string{"Driver", "Name", "Host", "Port", "Username*", "Password", "Database", "TLS", "Privacy", "Read-Only", "Action"}
}

func (f Form) fieldKeys() []string {
	if f.Values.Driver == DriverSQLite {
		return []string{"driver", "name", "target", "readOnly", "action"}
	}
	return []string{"driver", "name", "host", "port", "username", "password", "database", "tls", "privacy", "readOnly", "action"}
}

// focusField moves the field cursor to the field at index. The Privacy
// note is skipped by Huh navigation, so clicks on it leave focus
// unchanged. The loop bounds guard against navigation skipping fields.
func (f *Form) FocusField(field int) tea.Cmd {
	keys := f.fieldKeys()
	if field < 0 || field >= len(keys) || keys[field] == "privacy" {
		return nil
	}
	target := keys[field]
	for range len(keys) {
		if f.Huh.GetFocusedField().GetKey() == target {
			return f.FocusForm()
		}
		_ = f.Huh.NextField()
	}
	for range len(keys) {
		if f.Huh.GetFocusedField().GetKey() == target {
			return f.FocusForm()
		}
		_ = f.Huh.PrevField()
	}
	return nil
}

// Rebuild rebuilds the huh form for the current driver's layout.
func (f *Form) Rebuild() tea.Cmd {
	fields := []huh.Field{
		huh.NewSelect[Driver]().Key("driver").Title("Driver").Options(
			huh.NewOption("SQLite", DriverSQLite),
			huh.NewOption("MySQL", DriverMySQL),
			huh.NewOption("PostgreSQL", DriverPostgreSQL),
		).Value(&f.Values.Driver),
		uikit.NewEditableInput(huh.NewInput().Key("name").Title("Name").Placeholder("Local database").Value(&f.Values.Name), &f.Values.Name),
	}
	if f.Values.Driver != DriverSQLite {
		fields = append(fields,
			uikit.NewEditableInput(huh.NewInput().Key("host").Title("Host").Placeholder("localhost").Value(&f.Values.Host), &f.Values.Host),
			uikit.NewEditableInput(huh.NewInput().Key("port").Title("Port").Placeholder(f.defaultPort()).Value(&f.Values.Port), &f.Values.Port),
			uikit.NewEditableInput(huh.NewInput().Key("username").Title("Username*").Value(&f.Values.User).Validate(requiredUser), &f.Values.User),
			uikit.NewEditableInput(huh.NewInput().Key("password").Title("Password").Value(&f.Values.Pass).EchoMode(huh.EchoModePassword), &f.Values.Pass),
			uikit.NewEditableInput(huh.NewInput().Key("database").Title("Database").Placeholder("Optional").Value(&f.Values.Target), &f.Values.Target),
		)
		switch f.Values.Driver {
		case DriverMySQL:
			fields = append(fields,
				huh.NewSelect[MySQLTLS]().Key("tls").Title("TLS").Options(
					huh.NewOption("Verify certificate", MySQLTLSVerify),
					huh.NewOption("Encrypt, don't verify certificate", MySQLTLSSkipVerify),
					huh.NewOption("Don't encrypt", MySQLTLSDisabled),
				).Value(&f.Values.MySQLTLS),
			)
		case DriverPostgreSQL:
			fields = append(fields,
				huh.NewSelect[PostgreSQLTLS]().Key("tls").Title("TLS").Options(
					huh.NewOption("Verify certificate", PostgreSQLTLSVerifyFull),
					huh.NewOption("Encrypt, don't verify certificate", PostgreSQLTLSEncrypt),
					huh.NewOption("Don't encrypt", PostgreSQLTLSDisabled),
				).Value(&f.Values.PostgreSQLTLS),
			)
		}
		fields = append(fields,
			huh.NewNote().Title("Privacy").Description("Profiles save connection details. Passwords are stored encrypted at rest. Use ${ENV_VAR} or file:///path to reference secrets without persistence."),
		)
	} else {
		fields = append(fields, uikit.NewEditableInput(huh.NewInput().Key("target").Title("Target*").Placeholder("path/to/database.db or :memory:").Value(&f.Values.Target).Validate(requiredTarget), &f.Values.Target))
	}
	fields = append(fields,
		huh.NewConfirm().Key("readOnly").Title("Read-Only").Description("Block mutations (INSERT, UPDATE, DELETE, DDL)").Value(&f.Values.ReadOnly),
		NewActionButtons(&f.Values.Action),
	)
	f.Huh = uikit.NewForm(huh.NewGroup(fields...)).WithShowHelp(f.Width >= 40).WithWidth(max(f.Width, 1))
	if f.Height > 0 {
		f.Huh.WithHeight(f.Height)
	}
	return f.Huh.Init()
}

// UpdateHuh routes one message through the form while it is the insert
// target. It returns the form command and, when the form completes, the
// action request for the root to perform (test or open).
func (f *Form) UpdateHuh(message tea.Msg) (tea.Cmd, Event) {
	if f.Confirmation != nil {
		return nil, nil
	}
	driver := f.Values.Driver
	model, command := f.Huh.Update(message)
	f.Huh = model.(*huh.Form)
	if f.Values.Driver != driver {
		return f.SelectDriver(f.Values.Driver), nil
	}
	if f.Huh.State != huh.StateCompleted {
		return command, nil
	}
	action := f.Values.Action
	rebuild := f.Rebuild()
	switch action {
	case ActionTest:
		return rebuild, TestRequested{Target: f.ConnectionTarget()}
	case ActionConnect:
		return rebuild, OpenRequested{Target: f.ConnectionTarget(), Profile: f.Profile()}
	}
	return rebuild, nil
}

// SelectDriver applies a driver change: the port keeps its well-known
// default when switching between MySQL and PostgreSQL, then the form
// rebuilds for the new driver's layout.
func (f *Form) SelectDriver(driver Driver) tea.Cmd {
	f.Values.Driver = driver
	if driver == DriverPostgreSQL && f.Values.Port == "3306" {
		f.Values.Port = "5432"
	} else if driver == DriverMySQL && f.Values.Port == "5432" {
		f.Values.Port = "3306"
	}
	return f.Rebuild()
}

// SelectOptionAt maps a click on the rendered form view to an option row
// of the Driver or TLS select. It returns the field key and option index,
// or ("", -1) when the click misses every option row. Huh's select fields
// do not handle mouse clicks, so the app picks the option itself: option
// rows render below the select's title line, long option keys are
// word-wrapped across several lines, and the scan stops at the nearest
// field title so a value line that happens to read like an option (a
// database named "MySQL") is not mistaken for a select row.
func (f Form) SelectOptionAt(view string, viewLine int) (string, int) {
	if viewLine < 0 {
		return "", -1
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	if viewLine >= len(lines) {
		return "", -1
	}
	titles := f.FieldTitles()
	keys := f.fieldKeys()
	for line := viewLine; line >= 0; line-- {
		key := ""
		for index, title := range titles {
			if formLineIsTitle(lines[line], title) {
				key = keys[index]
				break
			}
		}
		if key == "" {
			continue
		}
		if key != "driver" && key != "tls" {
			break // a different field owns this line
		}
		for option, label := range selectOptions[key] {
			if selectLineIsOption(lines[viewLine], label, f.Width) {
				return key, option
			}
		}
		break
	}
	return "", -1
}

// selectLineIsOption reports whether the stripped form line is a row of
// the given option label. Huh word-wraps option keys with lipgloss.Wrap
// at the field width minus the frame and cursor cells, so any wrapped
// fragment of the label counts as the option's row.
func selectLineIsOption(line, label string, width int) bool {
	clean := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "┃"))
	clean = strings.TrimSpace(strings.TrimPrefix(clean, ">"))
	if clean == "" {
		return false
	}
	for _, fragment := range strings.Split(lipgloss.Wrap(label, max(width-4, 1), ",.-; "), "\n") {
		if clean == strings.TrimSpace(fragment) {
			return true
		}
	}
	return false
}

// ApplySelectOption sets the clicked select's value and rebuilds the form
// so the selection highlight follows (huh renders the cursor from its own
// state). A TLS click restores focus to the TLS field; the driver rebuild
// resets focus to the first field, matching the keyboard path.
func (f *Form) ApplySelectOption(field string, option int) tea.Cmd {
	switch field {
	case "driver":
		return f.SelectDriver([]Driver{DriverSQLite, DriverMySQL, DriverPostgreSQL}[option])
	case "tls":
		if f.Values.Driver == DriverMySQL {
			f.Values.MySQLTLS = []MySQLTLS{MySQLTLSVerify, MySQLTLSSkipVerify, MySQLTLSDisabled}[option]
		} else {
			f.Values.PostgreSQLTLS = []PostgreSQLTLS{PostgreSQLTLSVerifyFull, PostgreSQLTLSEncrypt, PostgreSQLTLSDisabled}[option]
		}
		tls := slices.Index(f.fieldKeys(), "tls")
		if tls < 0 {
			return f.Rebuild()
		}
		return tea.Sequence(f.Rebuild(), f.FocusField(tls))
	}
	return nil
}

// BeginConfirmation opens the Connect confirmation for the current values.
func (f *Form) BeginConfirmation() {
	f.Confirmation = uikit.YesNoConfirmation("Connect to "+f.ConnectionName()+"?", "", ActionConnect)
}

// ShowValidationError rebuilds the form and focuses the invalid field.
func (f *Form) ShowValidationError() tea.Cmd {
	return tea.Sequence(f.Rebuild(), func() tea.Msg { return ValidationMsg{} })
}

// FocusValidationError moves the field cursor to the first invalid field.
func (f *Form) FocusValidationError() {
	for range 7 {
		field := f.Huh.GetFocusedField()
		_ = field.Blur()
		if field.Error() != nil {
			return
		}
		_ = f.Huh.NextField()
	}
}

// Blur removes focus from the focused field.
func (f *Form) Blur() {
	if f.Huh != nil {
		_ = f.Huh.GetFocusedField().Blur()
	}
}

// FocusForm refocuses the focused field.
func (f *Form) FocusForm() tea.Cmd {
	if f.Huh == nil {
		return nil
	}
	return f.Huh.GetFocusedField().Focus()
}

// SetWidth clamps the form width. Huh's text input panics when its
// internal width goes negative, which happens below roughly frame(2) +
// prompt(2) + 1 cells. Keep the form renderable even during degenerate
// 0x0 layouts.
func (f *Form) SetWidth(width int) {
	f.Width = max(width, 8)
	if f.Huh != nil {
		f.Huh.WithWidth(f.Width).WithShowHelp(f.Width >= 40)
	}
}

// SetHeight clips the form viewport to the pane body height. Huh's group
// viewport scrolls to the focused field on every navigation, so fields
// that overflow the pane (TLS options, Read-Only, the action buttons)
// stay reachable and visible instead of being clipped away.
func (f *Form) SetHeight(height int) {
	f.Height = max(height, 1)
	if f.Huh != nil {
		f.Huh.WithHeight(f.Height)
	}
}

// DriverName returns the display label of the selected driver.
func (f Form) DriverName() string {
	switch f.Values.Driver {
	case DriverMySQL:
		return "MySQL"
	case DriverPostgreSQL:
		return "PostgreSQL"
	default:
		return "SQLite"
	}
}

// HostValue returns the effective connection host: an empty Host field
// falls back to localhost so the user can leave the field untouched.
func (f Form) HostValue() string {
	if host := strings.TrimSpace(f.Values.Host); host != "" {
		return host
	}
	return "localhost"
}

// defaultPort returns the well-known port for the selected driver.
func (f Form) defaultPort() string {
	if f.Values.Driver == DriverPostgreSQL {
		return "5432"
	}
	return "3306"
}

// PortValue returns the effective connection port: an empty Port field
// falls back to the driver default so the user can leave the field
// untouched.
func (f Form) PortValue() string {
	if port := strings.TrimSpace(f.Values.Port); port != "" {
		return port
	}
	return f.defaultPort()
}

// TargetValue builds the driver-specific opener target from the form
// values, resolving secret references.
func (f Form) TargetValue() string {
	pass := profile.ResolveSecretRef(f.Values.Pass)
	if f.Values.Driver == DriverMySQL {
		config := mysql.NewConfig()
		config.User = strings.TrimSpace(f.Values.User)
		config.Passwd = pass
		config.Net = "tcp"
		config.Addr = net.JoinHostPort(f.HostValue(), f.PortValue())
		config.DBName = strings.TrimSpace(f.Values.Target)
		tls := f.Values.MySQLTLS
		if tls == "" {
			tls = MySQLTLSDisabled
		}
		config.TLSConfig = string(tls)
		return config.FormatDSN()
	}
	if f.Values.Driver == DriverPostgreSQL {
		target := &url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(strings.TrimSpace(f.Values.User), pass),
			Host:   net.JoinHostPort(f.HostValue(), f.PortValue()),
			Path:   strings.TrimSpace(f.Values.Target),
		}
		tls := f.Values.PostgreSQLTLS
		if tls == "" {
			tls = PostgreSQLTLSDisabled
		}
		target.RawQuery = url.Values{"sslmode": {string(tls)}}.Encode()
		return target.String()
	}
	return strings.TrimSpace(f.Values.Target)
}

// Validate checks the required fields for the selected driver.
func (f Form) Validate() error {
	if f.Values.Driver == DriverSQLite {
		return requiredTarget(f.Values.Target)
	}
	if err := requiredPort(f.PortValue()); err != nil {
		return err
	}
	if err := requiredUser(f.Values.User); err != nil {
		return err
	}
	return nil
}

func requiredTarget(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("target is required")
	}
	return nil
}

func requiredPort(value string) error {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

func requiredUser(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("username is required")
	}
	return nil
}

// ConnectionName returns the display name of the current values.
func (f Form) ConnectionName() string {
	if name := strings.TrimSpace(f.Values.Name); name != "" {
		return name
	}
	return f.DriverName()
}

// ConnectionTarget returns the full opener target for the current values:
// server drivers gain their URL scheme prefix.
func (f Form) ConnectionTarget() string {
	target := f.TargetValue()
	switch f.Values.Driver {
	case DriverMySQL:
		return "mysql:" + target
	case DriverPostgreSQL:
		return "postgres:" + target
	default:
		return target
	}
}

// Profile builds the persisted profile for the current form values,
// without an identity (record assigns one).
func (f Form) Profile() profile.Profile {
	p := profile.Profile{
		Driver:   f.Values.Driver,
		Name:     f.ConnectionName(),
		Target:   strings.TrimSpace(f.Values.Target),
		Pass:     f.Values.Pass,
		ReadOnly: f.Values.ReadOnly,
	}
	if p.Driver != DriverSQLite {
		p.Host = f.HostValue()
		p.Port = f.PortValue()
		p.User = strings.TrimSpace(f.Values.User)
		switch p.Driver {
		case DriverMySQL:
			p.MySQLTLS = f.Values.MySQLTLS
		case DriverPostgreSQL:
			p.PostgreSQLTLS = f.Values.PostgreSQLTLS
		}
	}
	return p
}

// View renders the form, or empty while unbuilt.
func (f Form) View() string {
	if f.Huh == nil {
		return ""
	}
	return f.Huh.View()
}

// formLineIsTitle reports whether a stripped form line is the title row
// of the given field.
func formLineIsTitle(line, title string) bool {
	clean := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "┃"))
	clean = strings.TrimSpace(strings.TrimPrefix(clean, ">"))
	return clean == title
}
