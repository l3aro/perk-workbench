package connection

import (
	"charm.land/bubbles/v2/list"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
	"github.com/l3aro/perk-workbench/internal/workbench/uikit"
)

// RecentProfile adapts a persisted connection profile to the profiles
// list. Driver display labels and profile descriptions are presentation
// concerns, so they live with the connection feature rather than in the
// profile persistence package.
type RecentProfile struct {
	profile.Profile
}

func (c RecentProfile) FilterValue() string { return c.Name + " " + c.Target }
func (c RecentProfile) Title() string       { return uikit.SafeText(c.Name) }
func (c RecentProfile) Description() string {
	desc := ""
	if c.Driver != profile.DriverSQLite {
		desc = uikit.SafeText(c.driverName() + ": " + c.User + "@" + c.Host + ":" + c.Port + "/" + c.Target)
	} else {
		desc = uikit.SafeText(c.driverName() + ": " + c.Target)
	}
	if c.ReadOnly {
		desc += " [READONLY]"
	}
	return desc
}

func (c RecentProfile) driverName() string {
	switch c.Driver {
	case profile.DriverMySQL:
		return "MySQL"
	case profile.DriverPostgreSQL:
		return "PostgreSQL"
	default:
		return "SQLite"
	}
}

// RecentListItems converts profiles into list items for the profiles pane.
func RecentListItems(profiles []profile.Profile) []list.Item {
	items := make([]list.Item, len(profiles))
	for index, connection := range profiles {
		items[index] = RecentProfile{connection}
	}
	return items
}
