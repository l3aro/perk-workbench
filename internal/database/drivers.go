package database

import (
	"context"
	"strings"
	"sync"

	"github.com/l3aro/perk-workbench/internal/mongodb"
	"github.com/l3aro/perk-workbench/internal/mysql"
	"github.com/l3aro/perk-workbench/internal/postgres"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/sqlite"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// Spec describes one driver in the group: how a target addresses it, how
// to open a service for a matched target, and (optionally) how the
// connection form presents it. The group is the single place that maps
// target forms to backends; the workbench never switches on target
// prefixes itself.
type Spec struct {
	// Name identifies the driver. It is the profile driver name
	// ("sqlite", "mysql", "postgres").
	Name string

	// Display is the human-readable driver label ("SQLite").
	Display string

	// Match reports the connection target for Open when target addresses
	// this driver, and ok=false otherwise. A nil Match means the driver is
	// reached only through the default fallback (SQLite).
	Match func(target string) (dsn string, ok bool)

	// Open opens a service for a matched target.
	Open func(ctx context.Context, target string) (sharedsql.Service, error)

	// Form describes the connection form for this driver, or nil when the
	// driver has no form entry (MongoDB is opened by target URL only).
	// The spec is plain data — no code — so plugin-supplied specs survive
	// the DTO boundary; target serialization is a registered in-process
	// builder (see RegisterTargetBuilder), not part of the spec.
	Form *FormSpec
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
	Label string
	Value string
}

// FormField describes one driver-specific connection-form field. Keys
// from the fixed set bind to the form's typed fields: host, port,
// username, password, database, target, tls. Any other key binds to the
// profile's generic extras.
type FormField struct {
	Key         string
	Title       string
	Kind        FormFieldKind
	Placeholder string
	// Default is the well-known value shown when the field is blank
	// (e.g. the default port).
	Default string
	Options []FormOption
	// Validate is the rule applied to the field value; Error is the
	// message shown when the rule fails.
	Validate FormValidation
	Error    string
}

// FormSpec declaratively describes the connection form for one driver:
// which fields to show, in order, and the opener-target prefix. It
// carries no code, so it can cross the plugin DTO boundary unchanged.
type FormSpec struct {
	Fields []FormField
	// Prefix is prepended to the serialized target so Match can route it
	// back to this driver ("" for SQLite).
	Prefix string
}

// FormValues is the driver-facing view of the connection form: effective
// host/port, credentials, the database field, the selected TLS mode, and
// driver-specific extras (secret references resolved).
type FormValues struct {
	Host     string
	Port     string
	User     string
	Pass     string
	Database string
	TLS      string
	Extras   map[string]string
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
	if spec.Name == "" || spec.Open == nil {
		panic("database: driver spec needs a name and an open function")
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	if _, exists := byName[spec.Name]; exists {
		panic("database: driver " + spec.Name + " registered twice")
	}
	byName[spec.Name] = spec
	order = append(order, spec.Name)
}

// ByName returns the registered driver with the given name.
func ByName(name string) (Spec, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	spec, ok := byName[name]
	return spec, ok
}

// Match selects the driver whose target form addresses target, returning
// the connection target to open. Registration order is precedence order.
func Match(target string) (Spec, string, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	for _, name := range order {
		spec := byName[name]
		if spec.Match == nil {
			continue
		}
		if dsn, ok := spec.Match(target); ok {
			return spec, dsn, true
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
// dialect in the adapter packages; a future plugin shim registers one for
// its driver, receiving the same field-value DTO.
type TargetBuilder func(values FormValues) (string, bool)

// registerTargetBuilder registers the target serializer for a driver.
// Built-ins register theirs here; a future plugin shim's transport gets
// its own registration path when it exists. Names are unique: registering
// a duplicate name panics.
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
		Match:   mysqlMatch,
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
		Match:   postgresMatch,
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
		Match:   mongoMatch,
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

func mongoMatch(target string) (string, bool) {
	if dsn, ok := strings.CutPrefix(target, "mongo:"); ok {
		return dsn, true
	}
	if strings.HasPrefix(target, "mongodb://") || strings.HasPrefix(target, "mongodb+srv://") {
		return target, true
	}
	return "", false
}

func mysqlMatch(target string) (string, bool) {
	if dsn, ok := strings.CutPrefix(target, "mysql:"); ok {
		return dsn, true
	}
	return "", false
}

func postgresMatch(target string) (string, bool) {
	if strings.HasPrefix(target, "postgres://") || strings.HasPrefix(target, "postgresql://") {
		return target, true
	}
	if dsn, ok := strings.CutPrefix(target, "postgres:"); ok {
		return dsn, true
	}
	return "", false
}
