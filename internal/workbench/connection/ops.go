package connection

import (
	"maps"
	"path/filepath"
	"strings"

	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// ValidationMsg asks the root to re-render the form focused on its first
// invalid field after a failed validation attempt.
type ValidationMsg struct{}

// Same reports whether two profiles describe the same connection. Plugin
// identity is part of the scope; legacy profiles without it compare by family.
func Same(left, right profile.Profile) bool {
	if left.Driver != right.Driver {
		return false
	}
	if left.Plugin != "" && right.Plugin != "" && left.Plugin != right.Plugin {
		return false
	}
	if left.Driver == DriverSQLite {
		return left.Target == right.Target
	}
	return left.Name == right.Name
}

// openedTarget is the target that was actually opened (Connect after
// updateOpen, or the target a successful Test just verified); for SQLite
// it wins over the form value, so Connect records the resolved file while
// Test records the form target and never picks up a previously connected
// root target.
func (m *Model) Record(openedTarget string, readOnly bool) (profile.Profile, error) {
	driver := m.Form.Values.Driver
	pluginID := strings.TrimSpace(m.Form.Values.Plugin)
	if pluginID == "" {
		if candidates := database.PluginsByDriver(string(driver)); len(candidates) == 1 {
			pluginID = candidates[0].PluginID
		}
	}
	target := strings.TrimSpace(m.Form.Values.Target)
	name := strings.TrimSpace(m.Form.Values.Name)
	if driver == DriverSQLite {
		if opened := strings.TrimSpace(openedTarget); opened != "" {
			target = opened
		}
		if name == "" {
			name = filepath.Base(target)
		}
	} else if name == "" {
		name = m.Form.ConnectionName()
	}
	connection := profile.Profile{
		Plugin:   pluginID,
		Driver:   driver,
		Name:     name,
		Target:   target,
		Pass:     m.Form.Values.Pass,
		ReadOnly: readOnly,
	}
	if connection.Driver != DriverSQLite {
		connection.Host = m.Form.HostValue()
		connection.Port = m.Form.PortValue()
		connection.User = strings.TrimSpace(m.Form.Values.User)
		switch connection.Driver {
		case DriverMySQL:
			connection.MySQLTLS = m.Form.Values.MySQLTLS
		case DriverPostgreSQL:
			connection.PostgreSQLTLS = m.Form.Values.PostgreSQLTLS
		}
	}
	connection.Extras = maps.Clone(m.Form.Values.Extras)
	// Carry the fail-closed marker through the edit round-trip: a field
	// still holding its retained undecryptable blob must never be saved
	// (Save refuses it until the user re-enters the value).
	connection.Undecryptable = maps.Clone(m.Form.Values.Undecryptable)
	// Reuse or generate the opaque profile identity. Editing a profile
	// keeps its ID; a new profile that matches an existing one reuses that
	// ID so its scoped chat/query history survives; otherwise mint a
	// UUIDv7. Never write a profile without a valid scope.
	id := strings.TrimSpace(m.Form.Values.ID)
	if !profile.ValidID(id) {
		for _, existing := range m.Profiles {
			if Same(existing, connection) && profile.ValidID(existing.ID) {
				id = existing.ID
				break
			}
		}
	}
	if !profile.ValidID(id) {
		generated, err := profile.NewID()
		if err != nil {
			return profile.Profile{}, err
		}
		id = generated
	}
	connection.ID = id
	connections := make([]profile.Profile, 0, min(len(m.Profiles)+1, profile.MaxProfiles))
	connections = append(connections, connection)
	for _, existing := range m.Profiles {
		if Same(existing, connection) {
			continue
		}
		if len(connections) == profile.MaxProfiles {
			break
		}
		connections = append(connections, existing)
	}
	m.SetProfiles(connections)
	if err := m.Save(); err != nil {
		return profile.Profile{}, err
	}
	return connection, nil
}

// LoadValues copies a saved profile into the connection form's values,
// normalizing empty TLS modes to the disabled defaults.
func (m *Model) LoadValues(connection profile.Profile) {
	m.Form.Values.ID = connection.ID
	m.Form.Values.Plugin, m.Form.Values.Driver, m.Form.Values.Name, m.Form.Values.Target = connection.Plugin, connection.Driver, connection.Name, connection.Target
	m.Form.Values.Host, m.Form.Values.Port, m.Form.Values.User = connection.Host, connection.Port, connection.User
	m.Form.Values.MySQLTLS = connection.MySQLTLS
	if m.Form.Values.MySQLTLS == "" {
		m.Form.Values.MySQLTLS = MySQLTLSDisabled
	}
	m.Form.Values.PostgreSQLTLS = connection.PostgreSQLTLS
	if m.Form.Values.PostgreSQLTLS == "" {
		m.Form.Values.PostgreSQLTLS = PostgreSQLTLSDisabled
	}
	m.Form.Values.ReadOnly = connection.ReadOnly
	m.Form.Values.Pass = connection.Pass
	m.Form.Values.Extras = maps.Clone(connection.Extras)
	m.Form.Values.Undecryptable = maps.Clone(connection.Undecryptable)
}

// Delete removes the given profile from the recent list.
func (m *Model) Delete(connection profile.Profile) {
	connections := make([]profile.Profile, 0, len(m.Profiles)-1)
	for _, existing := range m.Profiles {
		if Same(existing, connection) {
			continue
		}
		connections = append(connections, existing)
	}
	m.SetProfiles(connections)
	m.Save()
}
