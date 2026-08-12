package workbench

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench/internal/workbench/connection"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

func TestConnectionForm_rendersMySQLTLSChoices(t *testing.T) {
	// Given
	form := connection.NewForm()
	form.Values.Driver = driverMySQL
	form.Rebuild()
	_ = form.Huh.Init()

	// When
	view := form.View()

	// Then
	for _, choice := range []string{"TLS", "Verify certificate", "Encrypt, don't verify certificate", "Don't encrypt"} {
		if !strings.Contains(view, choice) {
			t.Fatalf("MySQL form = %q, want %q", view, choice)
		}
	}
}

func TestConnectionForm_rendersPostgreSQLTLSChoices(t *testing.T) {
	form := connection.NewForm()
	form.Values.Driver = driverPostgreSQL
	form.Rebuild()
	_ = form.Huh.Init()

	view := form.View()
	for _, choice := range []string{"TLS", "Verify certificate", "Encrypt, don't verify certificate", "Don't encrypt"} {
		if !strings.Contains(view, choice) {
			t.Fatalf("PostgreSQL form = %q, want %q", view, choice)
		}
	}
}

func TestConnectionForm_defaultsPostgreSQLTLSToDisabled(t *testing.T) {
	form := connection.NewForm()
	form.Values.Driver, form.Values.Host, form.Values.Port = driverPostgreSQL, "127.0.0.1", "5432"

	target := form.TargetValue()
	if !strings.Contains(target, "sslmode=disable") {
		t.Fatalf("PostgreSQL DSN = %q, want sslmode=disable", target)
	}
}

func TestConnectionForm_restoresPostgreSQLTLSFromRecentProfile(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Profiles = []profile.Profile{{
		Driver:        driverPostgreSQL,
		Name:          "Local Docker",
		Host:          "127.0.0.1",
		Port:          "5432",
		User:          "postgres",
		PostgreSQLTLS: postgresTLSEncrypt,
	}}
	_ = model.connection.component.Recent.SetItems(connection.RecentListItems(model.connection.component.Profiles))
	model.connection.component.Form.SetFocus(connectionFocusRecent)

	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	if model.connection.component.Form.Values.PostgreSQLTLS != postgresTLSEncrypt {
		t.Fatalf("PostgreSQL TLS mode = %q, want %q", model.connection.component.Form.Values.PostgreSQLTLS, postgresTLSEncrypt)
	}
}

func TestConnectionForm_restoresMySQLTLSFromRecentProfile(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.connection.component.Profiles = []profile.Profile{{
		Driver:   driverMySQL,
		Name:     "Local Docker",
		Host:     "127.0.0.1",
		Port:     "3306",
		User:     "root",
		MySQLTLS: mysqlTLSSkipVerify,
	}}
	_ = model.connection.component.Recent.SetItems(connection.RecentListItems(model.connection.component.Profiles))
	model.connection.component.Form.SetFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	if model.connection.component.Form.Values.MySQLTLS != mysqlTLSSkipVerify {
		t.Fatalf("MySQL TLS mode = %q, want %q", model.connection.component.Form.Values.MySQLTLS, mysqlTLSSkipVerify)
	}
}

func TestRecentConnections_persistMySQLTLSMode(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "connections.json")
	connections := []profile.Profile{{
		Driver:   driverMySQL,
		Name:     "Local Docker",
		Host:     "127.0.0.1",
		Port:     "3306",
		User:     "root",
		MySQLTLS: mysqlTLSSkipVerify,
	}}

	// When
	if err := profile.Save(path, connections); err != nil {
		t.Fatalf("saving recent connections: %v", err)
	}
	loaded, _ := profile.Load(path)

	// Then
	if len(loaded) != 1 || loaded[0].MySQLTLS != mysqlTLSSkipVerify {
		t.Fatalf("loaded connections = %#v, want persisted MySQL TLS mode", loaded)
	}
}
func TestRecentConnections_persistPostgreSQLTLSMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	connections := []profile.Profile{{
		Driver:        driverPostgreSQL,
		Name:          "Local Docker",
		Host:          "127.0.0.1",
		Port:          "5432",
		User:          "postgres",
		PostgreSQLTLS: postgresTLSEncrypt,
	}}

	if err := profile.Save(path, connections); err != nil {
		t.Fatalf("saving recent connections: %v", err)
	}
	loaded, _ := profile.Load(path)

	if len(loaded) != 1 || loaded[0].PostgreSQLTLS != postgresTLSEncrypt {
		t.Fatalf("loaded connections = %#v, want persisted PostgreSQL TLS mode", loaded)
	}
}
