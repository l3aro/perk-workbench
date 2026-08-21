package connection

import (
	"context"
	"net"
	"net/url"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

type connectionTestShim struct {
	caps    database.Capabilities
	build   func(database.FormValues) (string, bool)
	product string
}

func (s connectionTestShim) Capabilities() database.Capabilities { return s.caps }
func (s connectionTestShim) BuildTarget(values database.FormValues) (string, bool) {
	return s.build(values)
}
func (s connectionTestShim) Open(context.Context, string) (sharedsql.Service, error) {
	return &connectionTestService{product: s.product}, nil
}

type connectionTestService struct {
	sharedsql.Service
	product string
}

func (*connectionTestService) Close() error { return nil }
func (s *connectionTestService) Info() sharedsql.DatabaseInfo {
	product := s.product
	if product == "" {
		product = "test"
	}
	return sharedsql.DatabaseInfo{Product: product, Version: "test"}
}
func (*connectionTestService) ListSchema(context.Context) ([]sharedsql.SchemaObject, error) {
	return nil, nil
}

func init() {
	sqlLanguage := sharedsql.SQLQueryLanguage
	shims := []database.Shim{
		connectionTestShim{
			caps: database.Capabilities{
				Name: "sqlite", Display: "SQLite", QueryLanguage: &sqlLanguage,
				Form: &database.FormSpec{Fields: []database.FormField{{Key: "target", Title: "Target*", Kind: database.FormInput, Validate: database.FormRequired, Error: "target is required"}}},
			},
			build: func(values database.FormValues) (string, bool) { return strings.TrimSpace(values.Database), true },
		},
		connectionTestShim{
			caps: database.Capabilities{
				Name: "mysql", Display: "MySQL", Targets: []database.TargetPattern{{Prefix: "mysql:"}}, QueryLanguage: &sqlLanguage,
				Form: &database.FormSpec{Prefix: "mysql:", Fields: []database.FormField{
					{Key: "host", Title: "Host", Kind: database.FormInput, Placeholder: "localhost"},
					{Key: "port", Title: "Port", Kind: database.FormInput, Default: "3306", Validate: database.FormPort},
					{Key: "username", Title: "Username*", Kind: database.FormInput, Validate: database.FormRequired},
					{Key: "password", Title: "Password", Kind: database.FormPassword},
					{Key: "database", Title: "Database", Kind: database.FormInput},
					{Key: "tls", Title: "TLS", Kind: database.FormSelect, Options: []database.FormOption{{Label: "Verify certificate", Value: "true"}, {Label: "Encrypt, don't verify certificate", Value: "skip-verify"}, {Label: "Don't encrypt", Value: "false"}}},
				}},
			},
			build: func(values database.FormValues) (string, bool) {
				config := mysql.NewConfig()
				config.User, config.Passwd, config.Net = strings.TrimSpace(values.User), values.Pass, "tcp"
				config.Addr, config.DBName, config.TLSConfig = net.JoinHostPort(values.Host, values.Port), strings.TrimSpace(values.Database), values.TLS
				if config.TLSConfig == "" {
					config.TLSConfig = "false"
				}
				return config.FormatDSN(), true
			},
		},
		connectionTestShim{
			caps: database.Capabilities{
				Name: "postgres", Display: "PostgreSQL", Targets: []database.TargetPattern{{Prefix: "postgres://", KeepTarget: true}, {Prefix: "postgresql://", KeepTarget: true}, {Prefix: "postgres:"}}, QueryLanguage: &sqlLanguage,
				Form: &database.FormSpec{Prefix: "postgres:", Fields: []database.FormField{
					{Key: "host", Title: "Host", Kind: database.FormInput, Placeholder: "localhost"},
					{Key: "port", Title: "Port", Kind: database.FormInput, Default: "5432", Validate: database.FormPort},
					{Key: "username", Title: "Username*", Kind: database.FormInput, Validate: database.FormRequired},
					{Key: "password", Title: "Password", Kind: database.FormPassword},
					{Key: "database", Title: "Database", Kind: database.FormInput},
					{Key: "tls", Title: "TLS", Kind: database.FormSelect, Options: []database.FormOption{{Label: "Verify certificate", Value: "verify-full"}, {Label: "Encrypt, don't verify certificate", Value: "require"}, {Label: "Don't encrypt", Value: "disable"}}},
				}},
			},
			build: func(values database.FormValues) (string, bool) {
				target := &url.URL{Scheme: "postgres", User: url.UserPassword(strings.TrimSpace(values.User), values.Pass), Host: net.JoinHostPort(values.Host, values.Port), Path: strings.TrimSpace(values.Database)}
				query := target.Query()
				query.Set("sslmode", values.TLS)
				if values.TLS == "" {
					query.Set("sslmode", "disable")
				}
				target.RawQuery = query.Encode()
				return target.String(), true
			},
		},
	}
	for _, shim := range shims {
		if err := database.RegisterShim(shim); err != nil {
			panic(err)
		}
	}
}
