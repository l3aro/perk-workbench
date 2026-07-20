package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"

	"charm.land/bubbles/v2/list"
)

const maxRecentConnections = 20

type recentConnection struct {
	Driver connectionDriver `json:"driver"`
	Name   string           `json:"name"`
	Target string           `json:"target"`
}

func (c recentConnection) FilterValue() string { return c.Name + " " + c.Target }
func (c recentConnection) Title() string       { return safeText(c.Name) }
func (c recentConnection) Description() string {
	return safeText(c.driverName() + ": " + c.Target)
}

func (c recentConnection) driverName() string {
	if c.Driver == driverMySQL {
		return "MySQL"
	}
	return "SQLite"
}

func recentConnectionsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk", "recent.json"), nil
}

func loadRecentConnections(path string) []recentConnection {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var connections []recentConnection
	if json.Unmarshal(contents, &connections) != nil {
		return nil
	}
	result := make([]recentConnection, 0, min(len(connections), maxRecentConnections))
	for _, connection := range connections {
		if connection.Driver != driverSQLite || connection.Name == "" || connection.Target == "" {
			continue
		}
		result = append(result, connection)
		if len(result) == maxRecentConnections {
			break
		}
	}
	return result
}

func saveRecentConnections(path string, connections []recentConnection) error {
	persisted := make([]recentConnection, 0, len(connections))
	for _, connection := range connections {
		// ponytail: MySQL DSNs may contain passwords; persist SQLite only until a credential store exists.
		if connection.Driver == driverSQLite {
			persisted = append(persisted, connection)
		}
	}
	contents, err := json.Marshal(persisted)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "recent-*.json")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func recentListItems(connections []recentConnection) []list.Item {
	items := make([]list.Item, len(connections))
	for index, connection := range connections {
		items[index] = connection
	}
	return items
}
