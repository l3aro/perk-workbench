package workbench

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionForm_rendersMySQLTLSChoices(t *testing.T) {
	// Given
	form := newConnectionForm()
	form.values.driver = driverMySQL
	form.rebuildForm()
	_ = form.form.Init()

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
	form := newConnectionForm()
	form.values.driver = driverPostgreSQL
	form.rebuildForm()
	_ = form.form.Init()

	view := form.View()
	for _, choice := range []string{"TLS", "Verify certificate", "Encrypt, don't verify certificate", "Don't encrypt"} {
		if !strings.Contains(view, choice) {
			t.Fatalf("PostgreSQL form = %q, want %q", view, choice)
		}
	}
}

func TestConnectionForm_defaultsPostgreSQLTLSToDisabled(t *testing.T) {
	form := newConnectionForm()
	form.values.driver, form.values.host, form.values.port = driverPostgreSQL, "127.0.0.1", "5432"

	target := form.targetValue()
	if !strings.Contains(target, "sslmode=disable") {
		t.Fatalf("PostgreSQL DSN = %q, want sslmode=disable", target)
	}
}

func TestConnectionForm_restoresPostgreSQLTLSFromRecentProfile(t *testing.T) {
	model := New("", context.Background(), testOpen, false)
	model.recentConnections = []recentConnection{{
		Driver:        driverPostgreSQL,
		Name:          "Local Docker",
		Host:          "127.0.0.1",
		Port:          "5432",
		User:          "postgres",
		PostgreSQLTLS: postgresTLSEncrypt,
	}}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	model.connection.setFocus(connectionFocusRecent)

	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	if model.connection.values.postgresTLS != postgresTLSEncrypt {
		t.Fatalf("PostgreSQL TLS mode = %q, want %q", model.connection.values.postgresTLS, postgresTLSEncrypt)
	}
}

func TestConnectionForm_restoresMySQLTLSFromRecentProfile(t *testing.T) {
	// Given
	model := New("", context.Background(), testOpen, false)
	model.recentConnections = []recentConnection{{
		Driver:   driverMySQL,
		Name:     "Local Docker",
		Host:     "127.0.0.1",
		Port:     "3306",
		User:     "root",
		MySQLTLS: mysqlTLSSkipVerify,
	}}
	_ = model.recent.SetItems(recentListItems(model.recentConnections))
	model.connection.setFocus(connectionFocusRecent)

	// When
	command := model.editSelectedRecentConnection()
	model = resolveConnectionCommand(model, command)

	// Then
	if model.connection.values.mysqlTLS != mysqlTLSSkipVerify {
		t.Fatalf("MySQL TLS mode = %q, want %q", model.connection.values.mysqlTLS, mysqlTLSSkipVerify)
	}
}

func TestRecentConnections_persistMySQLTLSMode(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "connections.json")
	connections := []recentConnection{{
		Driver:   driverMySQL,
		Name:     "Local Docker",
		Host:     "127.0.0.1",
		Port:     "3306",
		User:     "root",
		MySQLTLS: mysqlTLSSkipVerify,
	}}

	// When
	if err := saveRecentConnections(path, connections); err != nil {
		t.Fatalf("saving recent connections: %v", err)
	}
	loaded := loadRecentConnections(path)

	// Then
	if len(loaded) != 1 || loaded[0].MySQLTLS != mysqlTLSSkipVerify {
		t.Fatalf("loaded connections = %#v, want persisted MySQL TLS mode", loaded)
	}
}
func TestRecentConnections_persistPostgreSQLTLSMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	connections := []recentConnection{{
		Driver:        driverPostgreSQL,
		Name:          "Local Docker",
		Host:          "127.0.0.1",
		Port:          "5432",
		User:          "postgres",
		PostgreSQLTLS: postgresTLSEncrypt,
	}}

	if err := saveRecentConnections(path, connections); err != nil {
		t.Fatalf("saving recent connections: %v", err)
	}
	loaded := loadRecentConnections(path)

	if len(loaded) != 1 || loaded[0].PostgreSQLTLS != postgresTLSEncrypt {
		t.Fatalf("loaded connections = %#v, want persisted PostgreSQL TLS mode", loaded)
	}
}
