package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/l3aro/perk-workbench/internal/mongodb"
	"github.com/l3aro/perk-workbench/internal/mysql"
	"github.com/l3aro/perk-workbench/internal/postgres"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/sqlite"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// Spec describes one driver in the group: which target forms address it,
// how to open a service for a matched target, and (optionally) how the
// connection form presents it. The group is the single place that maps
// target forms to backends; the workbench never switches on target
// prefixes itself. Everything but Open is plain data, so a spec crosses
// the plugin DTO boundary unchanged.
type Spec struct {
	// Name identifies the driver. It is the profile driver name
	// ("sqlite", "mysql", "postgres").
	Name string

	// Display is the human-readable driver label ("SQLite").
	Display string

	// Targets lists the target forms this driver addresses, in check
	// order. Empty means the driver is reached only through the default
	// fallback (SQLite).
	Targets []TargetPattern

	// Open opens a service for a matched target.
	Open func(ctx context.Context, target string) (sharedsql.Service, error)

	// Form describes the connection form for this driver, or nil when the
	// driver has no form entry (MongoDB is opened by target URL only).
	// Target serialization is a registered in-process builder (see
	// BuildTarget), not part of the spec.
	Form *FormSpec
}

// TargetPattern declaratively addresses one target form of a driver. A
// pattern matches targets beginning with Prefix. Label prefixes end with
// ":" ("mysql:") and are stripped from the target passed to Open; scheme
// prefixes are full URL schemes ("postgres://") whose target is passed to
// Open unchanged. A scheme pattern must be declared before a label
// pattern that would otherwise shadow it ("redis://" before "redis:").
type TargetPattern struct {
	Prefix string `json:"prefix"`
	// KeepTarget keeps the whole target for Open when true (URL scheme
	// forms); when false the prefix is stripped.
	KeepTarget bool `json:"keep_target,omitempty"`
}

// FormFieldKind selects the widget for a connection-form field.
type FormFieldKind int

const (
	// FormInput is a plain text input.
	FormInput FormFieldKind = iota
	// FormPassword is a masked text input.
	FormPassword
	// FormSelect is a select with fixed options.
	FormSelect
)

// FormValidation selects the validation rule for a form field.
type FormValidation int

const (
	// FormNone disables validation.
	FormNone FormValidation = iota
	// FormRequired requires a non-blank value.
	FormRequired
	// FormPort accepts a blank value or a port number in 1-65535.
	FormPort
)

// FormOption is one option of a select field.
type FormOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// FormField describes one driver-specific connection-form field. Keys
// from the fixed set bind to the form's typed fields: host, port,
// username, password, database, target, tls. Any other key binds to the
// profile's generic extras.
type FormField struct {
	Key         string        `json:"key"`
	Title       string        `json:"title"`
	Kind        FormFieldKind `json:"kind"`
	Placeholder string        `json:"placeholder,omitempty"`
	// Default is the well-known value shown when the field is blank
	// (e.g. the default port).
	Default string       `json:"default,omitempty"`
	Options []FormOption `json:"options,omitempty"`
	// Validate is the rule applied to the field value; Error is the
	// message shown when the rule fails. Kind and Validate are the Go
	// iota constants — a transport must not reorder them.
	Validate FormValidation `json:"validate"`
	Error    string         `json:"error,omitempty"`
}

// FormSpec declaratively describes the connection form for one driver:
// which fields to show, in order, and the opener-target prefix. It
// carries no code, so it can cross the plugin DTO boundary unchanged.
type FormSpec struct {
	Fields []FormField `json:"fields"`
	// Prefix is prepended to the serialized target so Match can route it
	// back to this driver ("" for SQLite).
	Prefix string `json:"prefix,omitempty"`
}

// FormValues is the driver-facing view of the connection form: effective
// host/port, credentials, the database field, the selected TLS mode, and
// driver-specific extras (secret references resolved). It is the
// field-value DTO a transport sends to a plugin for target
// serialization.
type FormValues struct {
	Host     string            `json:"host,omitempty"`
	Port     string            `json:"port,omitempty"`
	User     string            `json:"user,omitempty"`
	Pass     string            `json:"pass,omitempty"`
	Database string            `json:"database,omitempty"`
	TLS      string            `json:"tls,omitempty"`
	Extras   map[string]string `json:"extras,omitempty"`
}

var (
	driversMu sync.RWMutex
	byName    = map[string]Spec{}
	order     []string
)

// Register adds a driver to the group. Driver names are unique: registering
// a duplicate name panics. Registration is init-time only — the group is
// fully populated before any Open call — but the lock keeps late in-process
// registration (tests, future shims) safe.
func Register(spec Spec) {
	if err := validateSpec(spec); err != nil {
		panic(err.Error())
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	if _, exists := byName[spec.Name]; exists {
		panic("database: driver " + spec.Name + " registered twice")
	}
	byName[spec.Name] = spec
	order = append(order, spec.Name)
}

// validateSpec checks the invariants every registered driver must hold:
// a name, an open function, at least one target form (only the default
// fallback driver may have none), and — when the driver has a
// connection form — a form prefix routed by one of its stripped target
// forms, so serialized targets always reach the driver back.
func validateSpec(spec Spec) error {
	switch {
	case spec.Name == "":
		return errors.New("database: driver spec needs a name")
	case spec.Open == nil:
		return fmt.Errorf("database: driver spec %q needs an open function", spec.Name)
	case len(spec.Targets) == 0 && spec.Name != string(profile.DriverSQLite):
		return fmt.Errorf("database: driver %q has no target forms; only the fallback driver %q may", spec.Name, profile.DriverSQLite)
	}
	for _, pattern := range spec.Targets {
		if pattern.Prefix == "" {
			return fmt.Errorf("database: driver %q has an empty target prefix", spec.Name)
		}
	}
	if spec.Form != nil && spec.Form.Prefix != "" {
		routed := false
		for _, pattern := range spec.Targets {
			if pattern.Prefix == spec.Form.Prefix && !pattern.KeepTarget {
				routed = true
				break
			}
		}
		if !routed {
			return fmt.Errorf("database: driver %q form prefix %q must be one of its stripped target forms", spec.Name, spec.Form.Prefix)
		}
	}
	return nil
}

// ByName returns the registered driver with the given name.
func ByName(name string) (Spec, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	spec, ok := byName[name]
	return spec, ok
}

// Match selects the driver whose target form addresses target, returning
// the connection target to open. Registration order is precedence order;
// within a driver, patterns are checked in declared order.
func Match(target string) (Spec, string, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	for _, name := range order {
		for _, pattern := range byName[name].Targets {
			if !strings.HasPrefix(target, pattern.Prefix) {
				continue
			}
			if pattern.KeepTarget {
				return byName[name], target, true
			}
			return byName[name], strings.TrimPrefix(target, pattern.Prefix), true
		}
	}
	return Spec{}, "", false
}

// FormDrivers returns the registered drivers that offer a connection
// form, in registration order — the driver select's render order.
func FormDrivers() []Spec {
	driversMu.RLock()
	defer driversMu.RUnlock()
	drivers := make([]Spec, 0, len(order))
	for _, name := range order {
		if spec := byName[name]; spec.Form != nil {
			drivers = append(drivers, spec)
		}
	}
	return drivers
}

var (
	buildersMu     sync.RWMutex
	targetBuilders = map[string]TargetBuilder{}
)

// TargetBuilder serializes connection-form values into the opener target
// body for one driver (no prefix). Built-in builders implement their
// dialect in the adapter packages; RegisterShim installs the builder of a
// plugin transport, receiving the same field-value DTO.
type TargetBuilder func(values FormValues) (string, bool)

// registerTargetBuilder registers the target serializer for a driver.
// Built-ins register theirs here; plugin shims register theirs through
// RegisterShim. Names are unique: registering a duplicate name panics.
func registerTargetBuilder(name string, build TargetBuilder) {
	if name == "" || build == nil {
		panic("database: target builder needs a driver name and a function")
	}
	buildersMu.Lock()
	defer buildersMu.Unlock()
	if _, exists := targetBuilders[name]; exists {
		panic("database: target builder " + name + " registered twice")
	}
	targetBuilders[name] = build
}

// BuildTarget serializes form values into the opener target body for
// spec's driver. ok=false when the driver has no form or no registered
// builder; the caller then falls back to the raw target field.
func BuildTarget(spec Spec, values FormValues) (string, bool) {
	if spec.Form == nil {
		return "", false
	}
	buildersMu.RLock()
	defer buildersMu.RUnlock()
	build, ok := targetBuilders[spec.Name]
	if !ok {
		return "", false
	}
	return build(values)
}

// Capabilities is the serializable advertisement an external driver
// serves over its transport: identity, the target forms it addresses,
// and the connection form description — the DTO twin of Spec's data
// fields. Compiled-in drivers never travel as capabilities.
type Capabilities struct {
	Name    string          `json:"name"`
	Display string          `json:"display"`
	Targets []TargetPattern `json:"targets,omitempty"`
	Form    *FormSpec       `json:"form,omitempty"`
}

// Shim is the in-process face of one plugin-backed driver: the
// declarative capabilities the plugin advertised, plus the dialect the
// transport owns — target serialization and service opening, both
// bridged over its wire protocol. A shim-registered driver is
// indistinguishable from a compiled-in one.
type Shim interface {
	Capabilities() Capabilities
	BuildTarget(values FormValues) (string, bool)
	Open(ctx context.Context, target string) (sharedsql.Service, error)
}

// RegisterShim installs a plugin-backed driver into the group, deriving
// the spec from the shim's declarative capabilities. Unlike Register, a
// misconfigured shim returns an error: a broken plugin must not take the
// app down. Formless drivers never register a target builder; the
// connection form then falls back to the raw target field, like a
// target-only driver.
func RegisterShim(shim Shim) error {
	if shim == nil {
		return errors.New("database: nil shim")
	}
	caps := shim.Capabilities()
	if err := validateSpec(Spec{
		Name: caps.Name, Targets: caps.Targets, Open: shim.Open, Form: caps.Form,
	}); err != nil {
		return err
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	if _, exists := byName[caps.Name]; exists {
		return fmt.Errorf("database: driver %q registered twice", caps.Name)
	}
	// Install the target builder before the driver becomes visible, so a
	// concurrent BuildTarget can never observe a partially registered
	// shim. Formless drivers (MongoDB-style, target URL only) never
	// register one.
	if caps.Form != nil {
		buildersMu.Lock()
		defer buildersMu.Unlock()
		targetBuilders[caps.Name] = shim.BuildTarget
	}
	spec := Spec{
		Name:    caps.Name,
		Display: caps.Display,
		Targets: caps.Targets,
		Open:    shim.Open,
		Form:    caps.Form,
	}
	byName[caps.Name] = spec
	order = append(order, caps.Name)
	return nil
}

func init() {
	// Registration order is both the match precedence and the connection
	// form's driver order; target forms are disjoint, so order is safe
	// for matching.
	Register(Spec{
		Name:    string(profile.DriverSQLite),
		Display: "SQLite",
		Open: func(ctx context.Context, target string) (sharedsql.Service, error) {
			return sqlite.Open(ctx, target)
		},
		Form: &FormSpec{
			Fields: []FormField{{
				Key: "target", Title: "Target*", Kind: FormInput,
				Placeholder: "path/to/database.db or :memory:",
				Validate:    FormRequired,
				Error:       "target is required",
			}},
		},
	})
	Register(Spec{
		Name:    string(profile.DriverMySQL),
		Display: "MySQL",
		Targets: []TargetPattern{{Prefix: "mysql:"}},
		Open: func(ctx context.Context, target string) (sharedsql.Service, error) {
			return mysql.Open(ctx, target)
		},
		Form: &FormSpec{
			Prefix: "mysql:",
			Fields: []FormField{
				{Key: "host", Title: "Host", Kind: FormInput, Placeholder: "localhost"},
				{Key: "port", Title: "Port", Kind: FormInput, Default: "3306", Validate: FormPort, Error: "port must be between 1 and 65535"},
				{Key: "username", Title: "Username*", Kind: FormInput, Validate: FormRequired, Error: "username is required"},
				{Key: "password", Title: "Password", Kind: FormPassword},
				{Key: "database", Title: "Database", Kind: FormInput, Placeholder: "Optional"},
				{Key: "tls", Title: "TLS", Kind: FormSelect, Options: []FormOption{
					{Label: "Verify certificate", Value: string(profile.MySQLTLSVerify)},
					{Label: "Encrypt, don't verify certificate", Value: string(profile.MySQLTLSSkipVerify)},
					{Label: "Don't encrypt", Value: string(profile.MySQLTLSDisabled)},
				}},
			},
		},
	})
	Register(Spec{
		Name:    string(profile.DriverPostgreSQL),
		Display: "PostgreSQL",
		Targets: []TargetPattern{
			{Prefix: "postgres://", KeepTarget: true},
			{Prefix: "postgresql://", KeepTarget: true},
			{Prefix: "postgres:"},
		},
		Open: func(ctx context.Context, target string) (sharedsql.Service, error) {
			return postgres.Open(ctx, target)
		},
		Form: &FormSpec{
			Prefix: "postgres:",
			Fields: []FormField{
				{Key: "host", Title: "Host", Kind: FormInput, Placeholder: "localhost"},
				{Key: "port", Title: "Port", Kind: FormInput, Default: "5432", Validate: FormPort, Error: "port must be between 1 and 65535"},
				{Key: "username", Title: "Username*", Kind: FormInput, Validate: FormRequired, Error: "username is required"},
				{Key: "password", Title: "Password", Kind: FormPassword},
				{Key: "database", Title: "Database", Kind: FormInput, Placeholder: "Optional"},
				{Key: "tls", Title: "TLS", Kind: FormSelect, Options: []FormOption{
					{Label: "Verify certificate", Value: string(profile.PostgreSQLTLSVerifyFull)},
					{Label: "Encrypt, don't verify certificate", Value: string(profile.PostgreSQLTLSEncrypt)},
					{Label: "Don't encrypt", Value: string(profile.PostgreSQLTLSDisabled)},
				}},
			},
		},
	})
	// MongoDB is opened by target URL only; it has no connection-form
	// entry.
	Register(Spec{
		Name:    "mongodb",
		Display: "MongoDB",
		Targets: []TargetPattern{
			{Prefix: "mongo:"},
			{Prefix: "mongodb://", KeepTarget: true},
			{Prefix: "mongodb+srv://", KeepTarget: true},
		},
		Open: func(ctx context.Context, target string) (sharedsql.Service, error) {
			return mongodb.Open(ctx, target)
		},
	})

	registerTargetBuilder(string(profile.DriverSQLite), func(values FormValues) (string, bool) {
		return strings.TrimSpace(values.Database), true
	})
	registerTargetBuilder(string(profile.DriverMySQL), func(values FormValues) (string, bool) {
		return mysql.Target(values.User, values.Pass, values.Host, values.Port, values.Database, values.TLS), true
	})
	registerTargetBuilder(string(profile.DriverPostgreSQL), func(values FormValues) (string, bool) {
		return postgres.Target(values.User, values.Pass, values.Host, values.Port, values.Database, values.TLS), true
	})
}
