package workbench

import (
	"charm.land/bubbles/v2/list"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// recentProfile adapts a persisted connection profile to the profiles
// list. Driver display labels and profile descriptions are presentation
// concerns, so they live with the connection feature rather than in the
// profile persistence package.
type recentProfile struct {
	profile.Profile
}

func (c recentProfile) FilterValue() string { return c.Name + " " + c.Target }
func (c recentProfile) Title() string       { return safeText(c.Name) }
func (c recentProfile) Description() string {
	desc := ""
	if c.Driver != profile.DriverSQLite {
		desc = safeText(c.driverName() + ": " + c.User + "@" + c.Host + ":" + c.Port + "/" + c.Target)
	} else {
		desc = safeText(c.driverName() + ": " + c.Target)
	}
	if c.ReadOnly {
		desc += " [READONLY]"
	}
	return desc
}

func (c recentProfile) driverName() string {
	switch c.Driver {
	case profile.DriverMySQL:
		return "MySQL"
	case profile.DriverPostgreSQL:
		return "PostgreSQL"
	default:
		return "SQLite"
	}
}

func recentListItems(profiles []profile.Profile) []list.Item {
	items := make([]list.Item, len(profiles))
	for index, connection := range profiles {
		items[index] = recentProfile{connection}
	}
	return items
}
