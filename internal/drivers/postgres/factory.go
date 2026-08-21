package postgres

import (
	"context"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

type Factory struct{}

func (Factory) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		Name:    "postgres",
		Driver:  "postgres",
		Display: "PostgreSQL",
		Targets: []driver.TargetPattern{
			{Prefix: "postgres://", KeepTarget: true},
			{Prefix: "postgresql://", KeepTarget: true},
			{Prefix: "postgres:"},
		},
		Form: &driver.FormSpec{
			Prefix: "postgres:",
			Fields: []driver.FormField{
				{Key: "host", Title: "Host", Kind: driver.FormFieldInput, Placeholder: "localhost"},
				{Key: "port", Title: "Port", Kind: driver.FormFieldInput, Default: "5432", Validate: driver.FormValidationPort, Error: "port must be between 1 and 65535"},
				{Key: "username", Title: "Username*", Kind: driver.FormFieldInput, Validate: driver.FormValidationRequired, Error: "username is required"},
				{Key: "password", Title: "Password", Kind: driver.FormFieldPassword},
				{Key: "database", Title: "Database", Kind: driver.FormFieldInput, Placeholder: "Optional"},
				{Key: "tls", Title: "TLS", Kind: driver.FormFieldSelect, Options: []driver.FormOption{
					{Label: "Verify certificate", Value: "verify-full"},
					{Label: "Encrypt, don't verify certificate", Value: "require"},
					{Label: "Don't encrypt", Value: "disable"},
				}},
			},
		},
		QueryLanguage: &driver.QueryLanguage{
			Name:        "SQL",
			EditorLabel: "SQL",
			Placeholder: "Enter a query…",
			Lexer:       "sql",
		},
		WriteCapabilities: driver.WriteCapabilities{RowWriter: true},
		Workspace: &driver.WorkspaceCapability{StandardTabs: []driver.StandardWorkspaceTab{
			driver.StandardWorkspaceTabColumns,
			driver.StandardWorkspaceTabIndexes,
			driver.StandardWorkspaceTabForeignKeys,
			driver.StandardWorkspaceTabDiagram,
		}},
	}
}

func (Factory) BuildTarget(_ context.Context, values driver.FormValues) (driver.BuildTargetResult, error) {
	return driver.BuildTargetResult{Target: Target(values), OK: true}, nil
}

func (Factory) Open(ctx context.Context, target string) (driver.OpenResult, error) {
	service, err := Open(ctx, target)
	if err != nil {
		return driver.OpenResult{}, driver.NewOperationError(driver.KindConnection, "opening postgres database failed")
	}
	return driver.OpenResult{Info: service.info, Service: &sessionService{service: service}}, nil
}

var _ driver.Factory = Factory{}
