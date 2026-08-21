package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/workbench/connection"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// errNoConnections reports that --select found no usable saved profile:
// there is nothing to choose from.
var errNoConnections = errors.New("no usable saved connections to choose from; save one inside the app, or pass a database path")

type selectedConnection struct {
	// ID is the stable profile scope; persistence matches on it so two
	// saved records sharing a target can never migrate each other.
	ID     string
	Plugin string
	Target string
}

// connectionOptions maps saved profiles to interactive select options.
func connectionOptions(profiles []profile.Profile) []huh.Option[selectedConnection] {
	options := make([]huh.Option[selectedConnection], 0, len(profiles))
	for _, p := range profiles {
		if p.Target == "" || strings.HasPrefix(p.Target, "enc:") {
			continue
		}
		pluginID := p.Plugin
		if pluginID == "" {
			candidates := database.PluginsByDriver(string(p.Driver))
			if len(candidates) != 1 {
				continue
			}
			pluginID = candidates[0].PluginID
		}
		item := connection.RecentProfile{Profile: p}
		key := fmt.Sprintf("%s (%s)", item.Title(), item.Description())
		options = append(options, huh.NewOption(key, selectedConnection{ID: p.ID, Plugin: pluginID, Target: p.Target}))
	}
	return options
}

// selectConnection runs the interactive CLI connection picker and returns
// the selected plugin and target. It returns an empty selection on abort.
// It must be called with the configured plugins already registered: a legacy
// record whose plugin field is omitted resolves only against the live
// registry, and only when exactly one enabled plugin serves its saved driver;
// that resolution is persisted. Ambiguous legacy records are never offered,
// so they can never open from this seam.
func selectConnection(output io.Writer) (selectedConnection, error) {
	path, err := profile.Path()
	if err != nil {
		return selectedConnection{}, err
	}
	profiles, _, _ := profile.Load(path)
	options := connectionOptions(profiles)
	if len(options) == 0 {
		return selectedConnection{}, errNoConnections
	}
	var selected selectedConnection
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[selectedConnection]().
				Title("Select a database connection").
				Options(options...).
				Value(&selected),
		),
	).WithOutput(output)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return selectedConnection{}, nil
		}
		return selectedConnection{}, err
	}
	if err := persistResolvedPlugin(path, profiles, selected); err != nil {
		return selected, err
	}
	return selected, nil
}

// persistResolvedPlugin writes back the selected legacy profile when this
// pick resolved its omitted plugin ID to exactly one serving plugin. The
// migration is additive: an already-resolved record or a record that stays
// ambiguous leaves the file untouched.
func persistResolvedPlugin(path string, profiles []profile.Profile, selected selectedConnection) error {
	for i := range profiles {
		p := &profiles[i]
		if p.Plugin != "" || p.ID != selected.ID {
			continue
		}
		candidates := database.PluginsByDriver(string(p.Driver))
		if len(candidates) != 1 || candidates[0].PluginID != selected.Plugin {
			return nil
		}
		p.Plugin = candidates[0].PluginID
		return profile.Save(path, profiles)
	}
	return nil
}
