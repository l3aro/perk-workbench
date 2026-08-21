package connection

import (
	"charm.land/bubbles/v2/list"
	"github.com/l3aro/perk-workbench/internal/database"
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

func (c RecentProfile) FilterValue() string { return c.Name + " " + c.Target + " " + c.Plugin }
func (c RecentProfile) Title() string       { return uikit.SafeText(c.Name) }
func (c RecentProfile) Description() string {
	desc := ""
	if c.Driver != profile.DriverSQLite {
		desc = uikit.SafeText(c.driverName() + ": " + c.User + "@" + c.Host + ":" + c.Port + "/" + c.Target)
	} else {
		desc = uikit.SafeText(c.driverName() + ": " + c.Target)
	}
	if len(database.PluginsByDriver(string(c.Driver))) > 1 && c.Plugin != "" {
		desc += " · " + uikit.SafeText(c.Plugin)
	}
	if c.ReadOnly {
		desc += " [READONLY]"
	}
	return desc
}

func (c RecentProfile) driverName() string {
	if c.Plugin != "" {
		if spec, ok := database.ByPlugin(c.Plugin); ok {
			return spec.Display
		}
	}
	if specs := database.PluginsByDriver(string(c.Driver)); len(specs) > 0 {
		return specs[0].Display
	}
	return string(c.Driver)
}

// RecentListItems converts profiles into list items for the profiles pane.
func RecentListItems(profiles []profile.Profile) []list.Item {
	items := make([]list.Item, len(profiles))
	for index, connection := range profiles {
		items[index] = RecentProfile{connection}
	}
	return items
}
