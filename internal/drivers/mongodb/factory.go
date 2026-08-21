package mongodb

import (
	"context"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

type Factory struct{}

func (Factory) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		Name:    "mongodb",
		Driver:  "mongodb",
		Display: "MongoDB",
		Targets: []driver.TargetPattern{
			{Prefix: "mongo:"},
			{Prefix: "mongodb://", KeepTarget: true},
			{Prefix: "mongodb+srv://", KeepTarget: true},
		},
		QueryLanguage: &driver.QueryLanguage{
			Name:        "MongoDB",
			EditorLabel: "Command",
			Placeholder: "Enter a mongosh statement…",
			Lexer:       "javascript",
			Examples: []string{
				`db.restaurants.find({"borough": "Bronx"}).limit(5)`,
				`db.restaurants.countDocuments({"cuisine": "Chinese"})`,
				`show collections`,
			},
		},
		WriteCapabilities: driver.WriteCapabilities{
			Document: &driver.DocumentWriteCapability{
				Format: driver.DocumentFormatMongoExtendedJSON,
				Text:   true,
			},
		},
		Workspace: &driver.WorkspaceCapability{
			StandardTabs: []driver.StandardWorkspaceTab{
				driver.StandardWorkspaceTabColumns,
				driver.StandardWorkspaceTabIndexes,
			},
			CustomViews: []driver.CustomWorkspaceView{{
				ID:     workspaceStatsViewID,
				Label:  "Stats",
				Scopes: []driver.WorkspaceViewScope{driver.WorkspaceViewDatabase, driver.WorkspaceViewTable},
			}},
		},
	}
}

func (Factory) BuildTarget(_ context.Context, values driver.FormValues) (driver.BuildTargetResult, error) {
	return driver.BuildTargetResult{Target: values.Extras["target"], OK: false}, nil
}

func (Factory) Open(ctx context.Context, target string) (driver.OpenResult, error) {
	service, err := Open(ctx, target)
	if err != nil {
		return driver.OpenResult{}, driver.NewOperationError(driver.KindConnection, "opening MongoDB connection failed")
	}
	return driver.OpenResult{Info: service.info, Service: &sessionService{service: service}}, nil
}

var _ driver.Factory = Factory{}
