package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
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

	// QueryLanguage describes how the query editor presents this
	// driver's language. A zero value carries no advertisement (the UI
	// falls back to its defaults); built-ins and registered shims always
	// carry an explicit one.
	QueryLanguage QueryLanguage

	// Workspace advertises the driver's workspace tab capability: the
	// standard tabs it supports beyond Query/Browse and its ordered
	// custom plain-data views. Nil keeps the legacy per-product tab
	// policy exactly; a present advertisement is authoritative for the
	// tab row.
	Workspace *sharedsql.WorkspaceCapability
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
	if err := validateQueryLanguage(spec.QueryLanguage); err != nil {
		return fmt.Errorf("database: driver %q: %w", spec.Name, err)
	}
	if err := sharedsql.ValidateWorkspaceCapability(spec.Workspace); err != nil {
		return fmt.Errorf("database: driver %q: %w", spec.Name, err)
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

// QueryLanguage is the query editor presentation of a driver's
// language; the canonical type and the legacy SQL default live in the
// shared contract package and cross the plugin DTO boundary unchanged.
type QueryLanguage = sharedsql.QueryLanguage

// QueryCommand is one static completion entry of a query language
// advertisement; the canonical type lives in the shared contract
// package and crosses the plugin DTO boundary unchanged.
type QueryCommand = sharedsql.QueryCommand

// SQLQueryLanguage is the legacy SQL default every driver without an
// explicit query language advertisement gets.
var SQLQueryLanguage = sharedsql.SQLQueryLanguage

// Workspace types: the workspace tab advertisement and view request
// DTOs live in the shared contract package and cross the plugin DTO
// boundary unchanged.
type (
	// WorkspaceCapability is a driver's workspace tab advertisement.
	WorkspaceCapability = sharedsql.WorkspaceCapability
	// CustomWorkspaceView is one advertised custom plain-data tab.
	CustomWorkspaceView = sharedsql.CustomWorkspaceView
	// StandardWorkspaceTab is one advertised standard tab key.
	StandardWorkspaceTab = sharedsql.StandardWorkspaceTab
	// WorkspaceViewKind is a workspace view target kind.
	WorkspaceViewKind = sharedsql.WorkspaceViewKind
	// WorkspaceViewTarget is the active structured target of a view.
	WorkspaceViewTarget = sharedsql.WorkspaceViewTarget
	// WorkspaceViewRequest is one custom view request.
	WorkspaceViewRequest = sharedsql.WorkspaceViewRequest
)

// Workspace view target kinds, aliased from the shared contract.
const (
	WorkspaceViewDatabase = sharedsql.WorkspaceViewDatabase
	WorkspaceViewSchema   = sharedsql.WorkspaceViewSchema
	WorkspaceViewTable    = sharedsql.WorkspaceViewTable
)

// Standard workspace tab keys, aliased from the shared contract.
const (
	StandardWorkspaceTabColumns     = sharedsql.StandardWorkspaceTabColumns
	StandardWorkspaceTabIndexes     = sharedsql.StandardWorkspaceTabIndexes
	StandardWorkspaceTabForeignKeys = sharedsql.StandardWorkspaceTabForeignKeys
	StandardWorkspaceTabDiagram     = sharedsql.StandardWorkspaceTabDiagram
)

// isZeroQueryLanguage reports whether ql carries no advertisement at
// all — every field blank and no examples.
func isZeroQueryLanguage(ql QueryLanguage) bool {
	return sharedsql.IsZeroQueryLanguage(ql)
}

// validateQueryLanguage checks the invariant set every nonzero query
// language advertisement must hold: name, editor label, and placeholder
// must be nonblank after trimming, every example must be nonblank, and
// every optional command entry must be nonblank, bounded, control-free,
// and case-insensitively unique within the capped list. A zero value is
// not an advertisement and passes. The invariant set lives in the
// shared contract package so registration and the plugin conformance
// runner can never drift apart.
func validateQueryLanguage(ql QueryLanguage) error {
	return sharedsql.ValidateQueryLanguage(ql)
}

// normalizeQueryLanguage resolves a plugin's query_language
// advertisement: an absent or all-zero advertisement falls back to the
// legacy SQL default; a present one is returned for validation by
// validateSpec — invalid metadata is rejected, never silently defaulted.
// This is the single normalization point for plugin capabilities.
func normalizeQueryLanguage(ql *QueryLanguage) QueryLanguage {
	if ql == nil || isZeroQueryLanguage(*ql) {
		return SQLQueryLanguage
	}
	return *ql
}

// normalizeCapabilities trims the plugin identity fields once at the
// registration boundary. Name remains the unique plugin identity; Driver
// is the non-unique database-family key and falls back to Name when omitted.
func normalizeCapabilities(caps Capabilities) Capabilities {
	caps.Name = strings.TrimSpace(caps.Name)
	caps.Driver = strings.TrimSpace(caps.Driver)
	if caps.Driver == "" {
		caps.Driver = caps.Name
	}
	return caps
}

// Capabilities is the serializable advertisement an external driver
// serves over its transport: identity, the target forms it addresses,
// the connection form description, and the query editor language — the
// DTO twin of Spec's data fields. Compiled-in drivers never travel as
// capabilities.
type Capabilities struct {
	Name    string `json:"name"`
	Display string `json:"display"`
	// Driver is the non-unique database-family key. When omitted, it
	// normalizes to Name for compatibility with existing v1 plugins.
	Driver  string          `json:"driver,omitempty"`
	Targets []TargetPattern `json:"targets,omitempty"`
	Form    *FormSpec       `json:"form,omitempty"`
	// QueryLanguage advertises the query editor language for this
	// driver, or nil when the plugin does not advertise one. The host
	// normalizes nil and zero advertisements to the legacy SQL default
	// at registration.
	QueryLanguage *QueryLanguage `json:"query_language,omitempty"`
	// WriteCapabilities advertises the optional row/document write
	// interfaces a plugin's sessions implement. A zero value means no
	// write support: the workbench never attempts row or document writes.
	WriteCapabilities sharedsql.WriteCapabilities `json:"write_capabilities"`
	// Workspace advertises the optional workspace tab metadata: the
	// standard tabs beyond Query/Browse the driver supports and its
	// ordered custom plain-data views. Absent (nil) keeps the legacy
	// per-product tab policy exactly, so old plugins load unchanged.
	Workspace *sharedsql.WorkspaceCapability `json:"workspace,omitempty"`
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

// checkShimConflictsLocked rejects a shim whose identity or target
// forms collide with the drivers visible under driversMu (held read or
// write by the caller): duplicate names, and cross-driver target
// prefixes that would shadow or be shadowed by another driver's forms.
// Overlaps within one shim's own patterns stay legal.
func checkShimConflictsLocked(caps Capabilities) error {
	if _, exists := byName[caps.Name]; exists {
		return fmt.Errorf("database: driver %q registered twice", caps.Name)
	}
	for _, pattern := range caps.Targets {
		for name, existing := range byName {
			for _, other := range existing.Targets {
				if strings.HasPrefix(pattern.Prefix, other.Prefix) || strings.HasPrefix(other.Prefix, pattern.Prefix) {
					return fmt.Errorf("database: driver %q target prefix %q overlaps %q of driver %q", caps.Name, pattern.Prefix, other.Prefix, name)
				}
			}
		}
	}
	return nil
}

// ValidateShim checks the registration invariants RegisterShim enforces
// — a valid capability advertisement, a name unique against the
// registered drivers, and no cross-driver target-prefix overlap —
// without installing anything. It is the read-only face of shim
// registration for diagnostic tooling: validating the same shim any
// number of times, including duplicate identities and overlapping
// target prefixes across items, never mutates the driver group.
func ValidateShim(shim Shim) error {
	if shim == nil {
		return errors.New("database: nil shim")
	}
	caps := normalizeCapabilities(shim.Capabilities())
	if err := validateSpec(Spec{
		Name: caps.Name, Targets: caps.Targets, Open: shim.Open, Form: caps.Form,
		QueryLanguage: normalizeQueryLanguage(caps.QueryLanguage),
		Workspace:     caps.Workspace,
	}); err != nil {
		return err
	}
	driversMu.RLock()
	defer driversMu.RUnlock()
	return checkShimConflictsLocked(caps)
}

// RegisterShim installs a plugin-backed driver into the group, deriving
// the spec from the shim's declarative capabilities. Unlike Register, a
// misconfigured shim returns an error: a broken plugin must not take the
// app down. Formless drivers never register a target builder; the
// connection form then falls back to the raw target field, like a
// target-only driver. ValidateShim is the side-effect-free face of the
// same checks.
func RegisterShim(shim Shim) error {
	if shim == nil {
		return errors.New("database: nil shim")
	}
	caps := normalizeCapabilities(shim.Capabilities())
	queryLanguage := normalizeQueryLanguage(caps.QueryLanguage)
	if err := validateSpec(Spec{
		Name: caps.Name, Targets: caps.Targets, Open: shim.Open, Form: caps.Form,
		QueryLanguage: queryLanguage,
		Workspace:     caps.Workspace,
	}); err != nil {
		return err
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	if err := checkShimConflictsLocked(caps); err != nil {
		return err
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
		Name:          caps.Name,
		Display:       caps.Display,
		Targets:       caps.Targets,
		Open:          shim.Open,
		Form:          caps.Form,
		QueryLanguage: queryLanguage,
		Workspace:     caps.Workspace,
	}
	byName[caps.Name] = spec
	order = append(order, caps.Name)
	return nil
}

// ValidateShimReplacement validates a restart replacement whose driver
// is already registered under its own identity: the replacement must be
// self-consistent and must not conflict with any OTHER registered
// driver, while the entry's own registration is excluded — it is the
// registration being swapped in place, never duplicated. The caller is
// still responsible for requiring the replacement to keep the
// registered identity. Side-effect-free, like ValidateShim.
func ValidateShimReplacement(shim Shim) error {
	if shim == nil {
		return errors.New("database: nil shim")
	}
	caps := normalizeCapabilities(shim.Capabilities())
	if err := validateSpec(Spec{
		Name: caps.Name, Targets: caps.Targets, Open: shim.Open, Form: caps.Form,
		QueryLanguage: normalizeQueryLanguage(caps.QueryLanguage),
		Workspace:     caps.Workspace,
	}); err != nil {
		return err
	}
	driversMu.RLock()
	defer driversMu.RUnlock()
	for _, pattern := range caps.Targets {
		for name, existing := range byName {
			if name == caps.Name {
				continue // the entry's own registration, being swapped
			}
			for _, other := range existing.Targets {
				if strings.HasPrefix(pattern.Prefix, other.Prefix) || strings.HasPrefix(other.Prefix, pattern.Prefix) {
					return fmt.Errorf("database: driver %q target prefix %q overlaps %q of driver %q", caps.Name, pattern.Prefix, other.Prefix, name)
				}
			}
		}
	}
	return nil
}
