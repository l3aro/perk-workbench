package postgres

import (
	"context"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

type sessionService struct {
	service *Service
}

func (s *sessionService) Execute(ctx context.Context, request driver.StatementRequest) (driver.Result, error) {
	return s.service.Execute(ctx, request.Statement)
}
func (s *sessionService) ExecuteReadOnly(ctx context.Context, request driver.StatementRequest) (driver.Result, error) {
	return s.service.ExecuteReadOnly(ctx, request.Statement)
}
func (s *sessionService) Validate(ctx context.Context, request driver.StatementRequest) error {
	return s.service.Validate(ctx, request.Statement)
}
func (s *sessionService) ListSchema(ctx context.Context, _ driver.EmptyRequest) ([]driver.SchemaObject, error) {
	return s.service.ListSchema(ctx)
}
func (s *sessionService) TableInfo(ctx context.Context, request driver.TableRequest) ([]driver.ColumnInfo, error) {
	return s.service.TableInfo(ctx, request.Table)
}
func (s *sessionService) ListIndexes(ctx context.Context, request driver.TableRequest) ([]driver.IndexInfo, error) {
	return s.service.ListIndexes(ctx, request.Table)
}
func (s *sessionService) CreateIndex(ctx context.Context, request driver.IndexChangeRequest) error {
	return s.service.CreateIndex(ctx, request.Table, request.Change)
}
func (s *sessionService) ReplaceIndex(ctx context.Context, request driver.ReplaceIndexRequest) error {
	return s.service.ReplaceIndex(ctx, request.Table, request.OldName, request.Change)
}
func (s *sessionService) DropIndex(ctx context.Context, request driver.DropRequest) error {
	return s.service.DropIndex(ctx, request.Table, request.Name)
}
func (s *sessionService) ListForeignKeys(ctx context.Context, request driver.TableRequest) ([]driver.ForeignKeyInfo, error) {
	return s.service.ListForeignKeys(ctx, request.Table)
}
func (s *sessionService) ListReferencingForeignKeys(ctx context.Context, request driver.TableRequest) ([]driver.ReferencingForeignKeyInfo, error) {
	return s.service.ListReferencingForeignKeys(ctx, request.Table)
}
func (s *sessionService) ListForeignKeysAll(ctx context.Context, _ driver.EmptyRequest) (map[string][]driver.ForeignKeyInfo, error) {
	return s.service.ListForeignKeysAll(ctx)
}
func (s *sessionService) ListIndexesAll(ctx context.Context, _ driver.EmptyRequest) (map[string][]driver.IndexInfo, error) {
	return s.service.ListIndexesAll(ctx)
}
func (s *sessionService) CreateForeignKey(ctx context.Context, request driver.ForeignKeyChangeRequest) error {
	return s.service.CreateForeignKey(ctx, request.Table, request.Change)
}
func (s *sessionService) ReplaceForeignKey(ctx context.Context, request driver.ReplaceForeignKeyRequest) error {
	return s.service.ReplaceForeignKey(ctx, request.Table, request.OldName, request.Change)
}
func (s *sessionService) DropForeignKey(ctx context.Context, request driver.DropRequest) error {
	return s.service.DropForeignKey(ctx, request.Table, request.Name)
}
func (s *sessionService) AlterColumn(ctx context.Context, request driver.ColumnChangeRequest) error {
	return s.service.AlterColumn(ctx, request.Table, request.Change)
}
func (s *sessionService) DropColumn(ctx context.Context, request driver.DropRequest) error {
	return s.service.DropColumn(ctx, request.Table, request.Name)
}
func (s *sessionService) AddColumn(ctx context.Context, request driver.AddColumnRequest) error {
	return s.service.AddColumn(ctx, request.Table, request.Def)
}
func (s *sessionService) BrowseTable(ctx context.Context, request driver.BrowseTableRequest) (driver.Result, error) {
	return s.service.BrowseTable(ctx, request.Table, request.Options)
}
func (s *sessionService) Close() error { return s.service.Close() }

func (s *sessionService) RowWrite(ctx context.Context, request driver.RowWriteRequest) (driver.RowWriteResponse, error) {
	var result driver.Result
	var err error
	switch request.Operation {
	case driver.RowWriteInsert:
		result, err = s.service.InsertRow(ctx, request.Table, request.Values)
	case driver.RowWriteUpdate:
		result, err = s.service.UpdateRow(ctx, request.Table, request.Key, request.Values)
	case driver.RowWriteDelete:
		result, err = s.service.DeleteRow(ctx, request.Table, request.Key)
	default:
		return driver.RowWriteResponse{}, driver.NewOperationError(driver.KindValidation, "unsupported row-write operation")
	}
	if err != nil {
		return driver.RowWriteResponse{}, err
	}
	return driver.RowWriteResponse{Result: driver.WriteResult{RowsAffected: result.RowsAffected}}, nil
}

var _ driver.SessionService = (*sessionService)(nil)
var _ driver.RowWriter = (*sessionService)(nil)
