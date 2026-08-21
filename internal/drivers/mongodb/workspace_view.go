package mongodb

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

const workspaceStatsViewID = "stats"

func (s *Service) WorkspaceView(ctx context.Context, request driver.WorkspaceViewRequest) (driver.Result, error) {
	if request.ViewID != workspaceStatsViewID {
		return driver.Result{}, unsupportedError("unknown workspace view")
	}
	var command bson.D
	switch request.Target.Kind {
	case driver.WorkspaceViewDatabase:
		command = bson.D{{Key: "dbStats", Value: 1}}
	case driver.WorkspaceViewTable:
		if request.Target.Table == "" {
			return driver.Result{}, validationError(errors.New("workspace statistics requires a collection"))
		}
		command = bson.D{{Key: "collStats", Value: request.Target.Table}}
	default:
		return driver.Result{}, unsupportedError("workspace statistics does not support this target")
	}
	started := time.Now()
	var document bson.D
	if err := s.db.RunCommand(ctx, command).Decode(&document); err != nil {
		return driver.Result{}, operationError(err)
	}
	return documentsResult([]bson.D{document}, false, time.Since(started)), nil
}
