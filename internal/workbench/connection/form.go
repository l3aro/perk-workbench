package connection

import (
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// Event is a typed request from the connection component to the root
// shell. The root performs database opening/testing and profile
// persistence side effects; the component only edits profiles and
// requests actions.
type Event interface{ isEvent() }

// OpenRequested asks the root to open the given target with the selected
// plugin and current form profile. Reconnect is reserved for sidebar switches.
type OpenRequested struct {
	Plugin    string
	Target    string
	Profile   profile.Profile
	Reconnect bool
}

func (OpenRequested) isEvent() {}

// TestRequested asks the root to test-connect the given target through plugin.
type TestRequested struct {
	Plugin string
	Target string
}

func (TestRequested) isEvent() {}

// ProfilesChanged reports that the component's profile list changed; the
// root re-reads it through the component API.
type ProfilesChanged struct{ Profiles []profile.Profile }

func (ProfilesChanged) isEvent() {}

// StatusChanged is the shared status-line event (uikit.StatusChanged).
type StatusChanged = uikit.StatusChanged

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
	// extras holds the value slots for spec fields outside the fixed key
	// set; flushExtras copies them back into Values.
	extras map[string]*string
}

// FormValues holds the editable connection fields. Plugin is the selected
// plugin instance; Driver remains its non-unique database family.
type FormValues struct {
	ID            string
	Plugin        string
	Driver        Driver
	Name, Target  string
	Host, Port    string
	User, Pass    string
	MySQLTLS      MySQLTLS
	PostgreSQLTLS PostgreSQLTLS
	ReadOnly      bool
	Action        string
	Extras        map[string]string
	Undecryptable map[string]string
}

// NewForm builds a fresh connection form with the disabled TLS defaults and
// the first registered plugin selected.
func NewForm() Form {
	values := &FormValues{
		MySQLTLS:      MySQLTLSDisabled,
		PostgreSQLTLS: PostgreSQLTLSDisabled,
		Action:        ActionTest,
	}
	if plugins := database.FormPlugins(); len(plugins) > 0 {
		values.Plugin = plugins[0].PluginID
		values.Driver = Driver(plugins[0].Driver)
	}
	form := Form{Values: values, Width: 80}
	_ = form.Rebuild()
	return form
}
func (f Form) selectedSpec() (database.Spec, bool) {
	if pluginID := strings.TrimSpace(f.Values.Plugin); pluginID != "" {
		spec, ok := database.ByPlugin(pluginID)
		if !ok {
			return database.Spec{}, false
		}
		if driver := strings.TrimSpace(string(f.Values.Driver)); driver != "" && spec.Driver != driver {
			return database.Spec{}, false
		}
		return spec, true
	}
	candidates := database.PluginsByDriver(string(f.Values.Driver))
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return database.Spec{}, false
}

// SetFocus switches the pane focus; leaving the form blurs its field.
func (f *Form) SetFocus(index int) tea.Cmd {
	f.Focus = index
	if index != FocusForm {
		f.Blur()
	}
	return nil
}

// FieldTitles lists the rendered titles of every connection form field in
// render order; the layout follows the selected plugin's spec.
func (f Form) FieldTitles() []string {
	titles := []string{"Plugin", "Name"}
	if spec, ok := f.selectedSpec(); ok && spec.Form != nil {
		for _, field := range spec.Form.Fields {
			titles = append(titles, field.Title)
		}
		if hasPasswordField(spec.Form.Fields) {
			titles = append(titles, "Privacy")
		}
	}
	return append(titles, "Read-Only", "Action")
}

func (f Form) fieldKeys() []string {
	keys := []string{"plugin", "name"}
	if spec, ok := f.selectedSpec(); ok && spec.Form != nil {
		for _, field := range spec.Form.Fields {
			keys = append(keys, field.Key)
		}
		if hasPasswordField(spec.Form.Fields) {
			keys = append(keys, "privacy")
		}
	}
	return append(keys, "readOnly", "action")
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

// Rebuild rebuilds the huh form for the current plugin's spec layout.
func (f *Form) Rebuild() tea.Cmd {
	f.extras = map[string]*string{}
	fields := []huh.Field{
		f.pluginSelect(),
		uikit.NewEditableInput(huh.NewInput().Key("name").Title("Name").Placeholder("Local database").Value(&f.Values.Name), &f.Values.Name),
	}
	if spec, ok := f.selectedSpec(); ok && spec.Form != nil {
		for _, field := range spec.Form.Fields {
			fields = append(fields, f.buildField(field))
		}
		if hasPasswordField(spec.Form.Fields) {
			fields = append(fields, huh.NewNote().Title("Privacy").Description("Profiles save connection details. Literal passwords are stored encrypted with a key in the same user-only config directory: this protects accidental disclosure and backups, not an attacker with your account access. Use ${ENV_VAR} or file:///path to reference secrets without persistence."))
		}
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

// pluginSelect lists plugin instances rather than database families.
func (f *Form) pluginSelect() huh.Field {
	plugins := database.FormPlugins()
	options := make([]huh.Option[string], len(plugins))
	for i, spec := range plugins {
		suffix := spec.PluginID
		if spec.Source == "builtin" {
			suffix = "Built-in"
		}
		options[i] = huh.NewOption(spec.Display+" · "+suffix, spec.PluginID)
	}
	return huh.NewSelect[string]().Key("plugin").Title("Plugin").Options(options...).Value(&f.Values.Plugin)
}

// buildField maps one spec field to its huh widget. Fields outside the
// fixed key set bind to the profile's generic extras.
func (f *Form) buildField(field database.FormField) huh.Field {
	if field.Kind == database.FormSelect {
		return f.buildSelectField(field)
	}
	bound := f.bindValue(field)
	input := huh.NewInput().Key(field.Key).Title(field.Title).Value(bound)
	if field.Placeholder != "" {
		input = input.Placeholder(field.Placeholder)
	}
	if field.Kind == database.FormPassword {
		input = input.EchoMode(huh.EchoModePassword)
	}
	if validate := f.fieldValidate(field); validate != nil {
		input = input.Validate(validate)
	}
	return uikit.NewEditableInput(input, bound)
}

// buildSelectField maps a select field to its widget. The TLS select
// binds to the driver's typed TLS mode; any other select binds to the
// extras.
func (f *Form) buildSelectField(field database.FormField) huh.Field {
	if field.Key == "tls" {
		if f.Values.Driver == DriverMySQL {
			options := make([]huh.Option[MySQLTLS], len(field.Options))
			for i, option := range field.Options {
				options[i] = huh.NewOption(option.Label, MySQLTLS(option.Value))
			}
			return huh.NewSelect[MySQLTLS]().Key(field.Key).Title(field.Title).Options(options...).Value(&f.Values.MySQLTLS)
		}
		options := make([]huh.Option[PostgreSQLTLS], len(field.Options))
		for i, option := range field.Options {
			options[i] = huh.NewOption(option.Label, PostgreSQLTLS(option.Value))
		}
		return huh.NewSelect[PostgreSQLTLS]().Key(field.Key).Title(field.Title).Options(options...).Value(&f.Values.PostgreSQLTLS)
	}
	options := make([]huh.Option[string], len(field.Options))
	for i, option := range field.Options {
		options[i] = huh.NewOption(option.Label, option.Value)
	}
	return huh.NewSelect[string]().Key(field.Key).Title(field.Title).Options(options...).Value(f.bindValue(field))
}

// bindValue returns the value slot for a spec field: the typed form
// fields for the fixed key set, a stable extras slot otherwise.
func (f *Form) bindValue(field database.FormField) *string {
	switch field.Key {
	case "host":
		return &f.Values.Host
	case "port":
		return &f.Values.Port
	case "username":
		return &f.Values.User
	case "password":
		return &f.Values.Pass
	case "database", "target":
		return &f.Values.Target
	default:
		value := new(string)
		*value = f.Values.Extras[field.Key]
		f.extras[field.Key] = value
		return value
	}
}

// flushExtras copies the extras-bound field values back into Values so
// target building and profile recording see the edited fields.
func (f *Form) flushExtras() {
	if len(f.extras) == 0 {
		return
	}
	if f.Values.Extras == nil {
		f.Values.Extras = make(map[string]string, len(f.extras))
	}
	for key, value := range f.extras {
		f.Values.Extras[key] = *value
	}
}

// UpdateHuh routes one message through the form while it is the insert
// target. It returns the form command and, when the form completes, the
// action request for the root to perform (test or open).
func (f *Form) UpdateHuh(message tea.Msg) (tea.Cmd, Event) {
	if f.Confirmation != nil {
		return nil, nil
	}
	driver := f.Values.Driver
	pluginID := f.Values.Plugin
	model, command := f.Huh.Update(message)
	f.Huh = model.(*huh.Form)
	f.flushExtras()
	if f.Values.Plugin != pluginID || f.Values.Driver != driver {
		return f.SelectPlugin(f.Values.Plugin), nil
	}
	if f.Huh.State != huh.StateCompleted {
		return command, nil
	}
	action := f.Values.Action
	rebuild := f.Rebuild()
	switch action {
	case ActionTest:
		return rebuild, TestRequested{Target: f.ConnectionTarget(), Plugin: f.Values.Plugin}
	case ActionConnect:
		return rebuild, OpenRequested{Target: f.ConnectionTarget(), Plugin: f.Values.Plugin, Profile: f.Profile()}
	}
	return rebuild, nil
}

// SelectPlugin applies a plugin change and rebuilds the form for its family.
func (f *Form) SelectPlugin(pluginID string) tea.Cmd {
	spec, ok := database.ByPlugin(pluginID)
	if !ok {
		f.Values.Plugin = ""
		f.Values.Driver = ""
		return f.Rebuild()
	}
	f.Values.Plugin = spec.PluginID
	f.Values.Driver = Driver(spec.Driver)
	newDefault := f.defaultPort()
	if newDefault != "" {
		for _, other := range database.FormPlugins() {
			if other.PluginID == pluginID {
				continue
			}
			if port := portDefault(other); port != "" && f.Values.Port == port {
				f.Values.Port = newDefault
				break
			}
		}
	}
	return f.Rebuild()
}

// portDefault returns the well-known port of a driver spec, or "" when
// the driver has no port field.
func portDefault(spec database.Spec) string {
	if spec.Form == nil {
		return ""
	}
	for _, field := range spec.Form.Fields {
		if field.Key == "port" {
			return field.Default
		}
	}
	return ""
}

// selectLabels lists option labels for the Plugin or TLS select.
func (f Form) selectLabels(key string) []string {
	switch key {
	case "plugin":
		plugins := database.FormPlugins()
		labels := make([]string, len(plugins))
		for i, spec := range plugins {
			suffix := spec.PluginID
			if spec.Source == "builtin" {
				suffix = "Built-in"
			}
			labels[i] = spec.Display + " · " + suffix
		}
		return labels
	case "tls":
		if spec, ok := f.selectedSpec(); ok && spec.Form != nil {
			for _, field := range spec.Form.Fields {
				if field.Key == "tls" {
					labels := make([]string, len(field.Options))
					for i, option := range field.Options {
						labels[i] = option.Label
					}
					return labels
				}
			}
		}
	}
	return nil
}

// SelectOptionAt maps a click on the rendered form view to an option row
// of the Plugin or TLS select. It returns the field key and option index,
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
		if key != "plugin" && key != "tls" {
			break // a different field owns this line
		}
		for option, label := range f.selectLabels(key) {
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

func (f *Form) ApplySelectOption(field string, option int) tea.Cmd {
	switch field {
	case "plugin":
		plugins := database.FormPlugins()
		if option < 0 || option >= len(plugins) {
			return nil
		}
		return f.SelectPlugin(plugins[option].PluginID)
	case "tls":
		spec, ok := f.selectedSpec()
		if !ok || spec.Form == nil {
			return nil
		}
		for _, specField := range spec.Form.Fields {
			if specField.Key != "tls" {
				continue
			}
			if option < 0 || option >= len(specField.Options) {
				return nil
			}
			value := specField.Options[option].Value
			switch spec.Driver {
			case string(profile.DriverMySQL):
				f.Values.MySQLTLS = MySQLTLS(value)
			case string(profile.DriverPostgreSQL):
				f.Values.PostgreSQLTLS = PostgreSQLTLS(value)
			}
			break
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

// DriverName returns the display label of the selected plugin's family.
func (f Form) DriverName() string {
	if spec, ok := f.selectedSpec(); ok {
		return spec.Display
	}
	if driver := strings.TrimSpace(string(f.Values.Driver)); driver != "" {
		if specs := database.PluginsByDriver(driver); len(specs) > 0 {
			return specs[0].Display
		}
		return driver
	}
	return "SQLite"
}

// HostValue returns the effective connection host: an empty Host field
// falls back to localhost so the user can leave the field untouched.
func (f Form) HostValue() string {
	if host := strings.TrimSpace(f.Values.Host); host != "" {
		return host
	}
	return "localhost"
}

// defaultPort returns the well-known port for the selected plugin.
func (f Form) defaultPort() string {
	if spec, ok := f.selectedSpec(); ok {
		return portDefault(spec)
	}
	return ""
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

// tlsValue returns the selected TLS mode as the wire string.
func (f Form) tlsValue() string {
	spec, ok := f.selectedSpec()
	if !ok {
		return ""
	}
	switch spec.Driver {
	case string(profile.DriverMySQL):
		return string(f.Values.MySQLTLS)
	case string(profile.DriverPostgreSQL):
		return string(f.Values.PostgreSQLTLS)
	}
	return ""
}

// fieldValue returns the raw form value for a spec field key.
func (f Form) fieldValue(key string) string {
	switch key {
	case "host":
		return f.Values.Host
	case "port":
		return f.Values.Port
	case "username":
		return f.Values.User
	case "password":
		return f.Values.Pass
	case "database", "target":
		return f.Values.Target
	case "tls":
		return f.tlsValue()
	default:
		return f.Values.Extras[key]
	}
}

// TargetValue builds the selected plugin's opener target body from the form
// values, resolving secret references.
func (f Form) TargetValue() string {
	spec, ok := f.selectedSpec()
	if !ok || spec.Form == nil {
		return strings.TrimSpace(f.Values.Target)
	}
	target, ok := database.BuildTarget(spec, database.FormValues{
		Host:     f.HostValue(),
		Port:     f.PortValue(),
		User:     f.Values.User,
		Pass:     profile.ResolveSecretRef(f.Values.Pass),
		Database: f.Values.Target,
		TLS:      f.tlsValue(),
		Extras:   resolveExtras(f.Values.Extras),
	})
	if !ok {
		return strings.TrimSpace(f.Values.Target)
	}
	return target
}

// resolveExtras expands secret references in extras values.
func resolveExtras(extras map[string]string) map[string]string {
	if len(extras) == 0 {
		return extras
	}
	resolved := make(map[string]string, len(extras))
	for key, value := range extras {
		resolved[key] = profile.ResolveSecretRef(value)
	}
	return resolved
}

// fieldValidate interprets the spec's validation rule for huh and
// form-level validation.
func (f Form) fieldValidate(field database.FormField) func(string) error {
	switch field.Validate {
	case database.FormRequired:
		return func(value string) error {
			if strings.TrimSpace(value) == "" {
				return errors.New(field.Error)
			}
			return nil
		}
	case database.FormPort:
		return func(value string) error {
			value = strings.TrimSpace(value)
			if value == "" {
				return nil
			}
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				return errors.New(field.Error)
			}
			return nil
		}
	}
	return nil
}

// hasPasswordField reports whether the spec carries a password field; the
// privacy note is shown for drivers that persist credentials.
func hasPasswordField(fields []database.FormField) bool {
	for _, field := range fields {
		if field.Kind == database.FormPassword {
			return true
		}
	}
	return false
}

// Validate checks the selected plugin's advertised field rules.
func (f Form) Validate() error {
	if strings.TrimSpace(f.Values.Plugin) == "" && len(database.PluginsByDriver(string(f.Values.Driver))) != 1 {
		return errors.New("select a plugin")
	}
	spec, ok := f.selectedSpec()
	if !ok {
		return errors.New("select a plugin")
	}
	if spec.Form == nil {
		return nil
	}
	for _, field := range spec.Form.Fields {
		if field.Validate == database.FormNone {
			continue
		}
		if err := f.fieldValidate(field)(f.fieldValue(field.Key)); err != nil {
			return err
		}
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
// the selected plugin's form prefix remains intact for explicit routing.
func (f Form) ConnectionTarget() string {
	target := f.TargetValue()
	if spec, ok := f.selectedSpec(); ok && spec.Form != nil {
		return spec.Form.Prefix + target
	}
	return target
}

// Profile builds the persisted profile for the current form values,
// without an identity (record assigns one).
func (f Form) Profile() profile.Profile {
	p := profile.Profile{
		Plugin:   f.Values.Plugin,
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
	if len(f.Values.Extras) > 0 {
		p.Extras = maps.Clone(f.Values.Extras)
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
