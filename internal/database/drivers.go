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
)

// Spec describes one driver in the group: how a target addresses it and
// how to open a service for a matched target. The group is the single
// place that maps target forms to backends; the workbench never switches
// on target prefixes itself.
type Spec struct {
	// Name identifies the driver. It is the profile driver name for
	// workbench-registered drivers ("sqlite", "mysql", "postgres").
	Name string

	// Match reports the connection target for Open when target addresses
	// this driver, and ok=false otherwise. A nil Match means the driver is
	// reached only through the default fallback (SQLite).
	Match func(target string) (dsn string, ok bool)

	// Open opens a service for a matched target.
	Open func(ctx context.Context, target string) (sharedsql.Service, error)
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

func init() {
	Register(Spec{
		Name:  "mongodb",
		Match: mongoMatch,
		Open: func(ctx context.Context, target string) (sharedsql.Service, error) {
			return mongodb.Open(ctx, target)
		},
	})
	Register(Spec{
		Name:  "mysql",
		Match: mysqlMatch,
		Open: func(ctx context.Context, target string) (sharedsql.Service, error) {
			return mysql.Open(ctx, target)
		},
	})
	Register(Spec{
		Name:  "postgres",
		Match: postgresMatch,
		Open: func(ctx context.Context, target string) (sharedsql.Service, error) {
			return postgres.Open(ctx, target)
		},
	})
	// SQLite is the default backend: no target form of its own, reached
	// through Open's fallback after resolution.
	Register(Spec{
		Name: "sqlite",
		Open: func(ctx context.Context, target string) (sharedsql.Service, error) {
			return sqlite.Open(ctx, target)
		},
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
