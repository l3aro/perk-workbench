package database

import (
	"context"
	"errors"
	"fmt"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
	"slices"
	"strings"
	"sync"
)

// Spec describes one registered plugin instance: the database family it
// serves, the target forms it addresses, and the callbacks that cross the
// plugin boundary. PluginID is unique; Driver is deliberately non-unique.
type Spec struct {
	PluginID      string
	Driver        string
	Display       string
	Targets       []TargetPattern
	Open          func(ctx context.Context, target string) (sharedsql.Service, error)
	BuildTarget   TargetBuilder
	Form          *FormSpec
	QueryLanguage QueryLanguage
	Workspace     *sharedsql.WorkspaceCapability
	// Source identifies how the plugin is hosted ("builtin" or "external").
	// Built-ins remain child processes; this is presentation metadata only.
	Source string
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
	byPlugin  = map[string]Spec{}
	order     []string
)

// Register adds a plugin instance to the registry. Plugin IDs are unique;
// database families and target prefixes may be shared.
func Register(spec Spec) {
	if err := validateSpec(spec); err != nil {
		panic(err.Error())
	}
	driversMu.Lock()
	defer driversMu.Unlock()
	if _, exists := byPlugin[spec.PluginID]; exists {
		panic("database: plugin " + spec.PluginID + " registered twice")
	}
	byPlugin[spec.PluginID] = spec
	order = append(order, spec.PluginID)
}

// validateSpec checks the invariants every registered plugin must hold.
func validateSpec(spec Spec) error {
	switch {
	case strings.TrimSpace(spec.PluginID) == "":
		return errors.New("database: plugin spec needs a plugin ID")
	case strings.TrimSpace(spec.Driver) == "":
		return fmt.Errorf("database: plugin %q needs a driver family", spec.PluginID)
	case spec.Open == nil:
		return fmt.Errorf("database: plugin spec %q needs an open function", spec.PluginID)
	case len(spec.Targets) == 0 && spec.Driver != string(profile.DriverSQLite):
		return fmt.Errorf("database: plugin %q has no target forms; only the SQLite fallback may", spec.PluginID)
	}
	for _, pattern := range spec.Targets {
		if pattern.Prefix == "" {
			return fmt.Errorf("database: plugin %q has an empty target prefix", spec.PluginID)
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
			return fmt.Errorf("database: plugin %q form prefix %q must be one of its stripped target forms", spec.PluginID, spec.Form.Prefix)
		}
	}
	if err := validateQueryLanguage(spec.QueryLanguage); err != nil {
		return fmt.Errorf("database: plugin %q: %w", spec.PluginID, err)
	}
	if err := sharedsql.ValidateWorkspaceCapability(spec.Workspace); err != nil {
		return fmt.Errorf("database: plugin %q: %w", spec.PluginID, err)
	}
	return nil
}

// ByPlugin returns the registered plugin instance with the given ID.
func ByPlugin(pluginID string) (Spec, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	spec, ok := byPlugin[pluginID]
	return spec, ok
}

// PluginsByDriver returns all registered plugin instances serving one family,
// in deterministic plugin-ID order.
func PluginsByDriver(driver string) []Spec {
	driversMu.RLock()
	defer driversMu.RUnlock()
	specs := make([]Spec, 0)
	for _, pluginID := range order {
		spec := byPlugin[pluginID]
		if spec.Driver == driver {
			specs = append(specs, spec)
		}
	}
	slices.SortFunc(specs, func(a, b Spec) int { return strings.Compare(a.PluginID, b.PluginID) })
	return specs
}

// FormPlugins returns registered plugin instances that advertise a form, in
// registry order so built-ins retain their stable selector order.
func FormPlugins() []Spec {
	driversMu.RLock()
	defer driversMu.RUnlock()
	specs := make([]Spec, 0, len(order))
	for _, pluginID := range order {
		if spec := byPlugin[pluginID]; spec.Form != nil {
			specs = append(specs, spec)
		}
	}
	return specs
}

// targetMatch is one plugin-specific target route.
type targetMatch struct {
	Spec Spec
	DSN  string
}

// Matches returns every plugin instance whose target pattern addresses target.
// Results are sorted by plugin ID, never by load order.
func Matches(target string) []targetMatch {
	driversMu.RLock()
	defer driversMu.RUnlock()
	matches := make([]targetMatch, 0)
	for _, pluginID := range order {
		spec := byPlugin[pluginID]
		for _, pattern := range spec.Targets {
			if !strings.HasPrefix(target, pattern.Prefix) {
				continue
			}
			dsn := target
			if !pattern.KeepTarget {
				dsn = strings.TrimPrefix(target, pattern.Prefix)
			}
			matches = append(matches, targetMatch{Spec: spec, DSN: dsn})
			break
		}
	}
	slices.SortFunc(matches, func(a, b targetMatch) int {
		return strings.Compare(a.Spec.PluginID, b.Spec.PluginID)
	})
	return matches
}

// TargetBuilder serializes connection-form values into the opener target
// body for one plugin (without its form prefix).
type TargetBuilder func(values FormValues) (string, bool)

// BuildTarget serializes form values through the callback carried by spec.
func BuildTarget(spec Spec, values FormValues) (string, bool) {
	if spec.Form == nil || spec.BuildTarget == nil {
		return "", false
	}
	return spec.BuildTarget(values)
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
// shimSource is optional metadata supplied by the loader. It distinguishes
// self-hosted built-ins from external children without moving either into the
// host process.
type shimSource interface {
	PluginSource() string
}

// checkShimConflictsLocked rejects only duplicate plugin IDs. Families and
// target prefixes are intentionally shareable.
func checkShimConflictsLocked(caps Capabilities) error {
	if _, exists := byPlugin[caps.Name]; exists {
		return fmt.Errorf("database: plugin %q registered twice", caps.Name)
	}
	return nil
}

// ValidateShim checks registration invariants without installing the shim.
func ValidateShim(shim Shim) error {
	if shim == nil {
		return errors.New("database: nil shim")
	}
	caps := normalizeCapabilities(shim.Capabilities())
	if err := validateSpec(Spec{
		PluginID:      caps.Name,
		Driver:        caps.Driver,
		Targets:       caps.Targets,
		Open:          shim.Open,
		BuildTarget:   shim.BuildTarget,
		Form:          caps.Form,
		QueryLanguage: normalizeQueryLanguage(caps.QueryLanguage),
		Workspace:     caps.Workspace,
	}); err != nil {
		return err
	}
	driversMu.RLock()
	defer driversMu.RUnlock()
	return checkShimConflictsLocked(caps)
}

// RegisterShim installs one plugin-backed driver into the registry.
func RegisterShim(shim Shim) error {
	source := ""
	if sourced, ok := shim.(shimSource); ok {
		source = sourced.PluginSource()
	}
	return registerShim(shim, source)
}

func registerShim(shim Shim, source string) error {
	if shim == nil {
		return errors.New("database: nil shim")
	}
	caps := normalizeCapabilities(shim.Capabilities())
	queryLanguage := normalizeQueryLanguage(caps.QueryLanguage)
	if err := validateSpec(Spec{
		PluginID:      caps.Name,
		Driver:        caps.Driver,
		Targets:       caps.Targets,
		Open:          shim.Open,
		BuildTarget:   shim.BuildTarget,
		Form:          caps.Form,
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
	spec := Spec{
		PluginID:      caps.Name,
		Driver:        caps.Driver,
		Display:       caps.Display,
		Targets:       caps.Targets,
		Open:          shim.Open,
		BuildTarget:   shim.BuildTarget,
		Form:          caps.Form,
		QueryLanguage: queryLanguage,
		Workspace:     caps.Workspace,
		Source:        source,
	}
	byPlugin[caps.Name] = spec
	order = append(order, caps.Name)
	return nil
}

// ValidateShimReplacement validates a restart replacement against all other
// registered plugin instances while excluding its own plugin ID.
func ValidateShimReplacement(shim Shim) error {
	if shim == nil {
		return errors.New("database: nil shim")
	}
	caps := normalizeCapabilities(shim.Capabilities())
	if err := validateSpec(Spec{
		PluginID:      caps.Name,
		Driver:        caps.Driver,
		Targets:       caps.Targets,
		Open:          shim.Open,
		BuildTarget:   shim.BuildTarget,
		Form:          caps.Form,
		QueryLanguage: normalizeQueryLanguage(caps.QueryLanguage),
		Workspace:     caps.Workspace,
	}); err != nil {
		return err
	}
	driversMu.RLock()
	defer driversMu.RUnlock()
	return nil
}
