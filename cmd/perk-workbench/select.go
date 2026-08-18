package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/connection"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// errNoConnections reports that --select found no usable saved profile:
// there is nothing to choose from.
var errNoConnections = errors.New("no usable saved connections to choose from; save one inside the app, or pass a database path")

// connectionOptions maps saved profiles to interactive select options.
// Titles and descriptions mirror the workbench's recent-profiles list so
// the CLI picker presents exactly what the app shows. Profiles whose
// opener target is unusable (empty, or a retained undecryptable envelope)
// are skipped rather than offered and failed on open.
func connectionOptions(profiles []profile.Profile) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(profiles))
	for _, p := range profiles {
		if p.Target == "" || strings.HasPrefix(p.Target, "enc:") {
			continue
		}
		item := connection.RecentProfile{Profile: p}
		key := fmt.Sprintf("%s (%s)", item.Title(), item.Description())
		options = append(options, huh.NewOption(key, p.Target))
	}
	return options
}

// selectConnection runs the interactive CLI connection picker and returns
// the chosen opener target. It returns ("", nil) when the user aborts the
// picker without choosing.
func selectConnection(output io.Writer) (string, error) {
	path, err := profile.Path()
	if err != nil {
		return "", err
	}
	profiles, _, _ := profile.Load(path)
	options := connectionOptions(profiles)
	if len(options) == 0 {
		return "", errNoConnections
	}
	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a database connection").
				Options(options...).
				Value(&selected),
		),
	).WithOutput(output)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", nil
		}
		return "", err
	}
	return selected, nil
}
