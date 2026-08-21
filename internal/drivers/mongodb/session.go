package mongodb

import (
	"context"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

type sessionService struct{ service *Service }

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
func (s *sessionService) WorkspaceView(ctx context.Context, request driver.WorkspaceViewRequest) (driver.Result, error) {
	return s.service.WorkspaceView(ctx, request)
}

func (s *sessionService) Close() error { return s.service.Close() }

func (s *sessionService) DocumentWrite(ctx context.Context, request driver.DocumentWriteRequest) (driver.DocumentWriteResponse, error) {
	var result driver.Result
	var document *driver.DocumentPayload
	var err error
	switch request.Operation {
	case driver.DocumentWriteRead:
		if request.ID == nil {
			return driver.DocumentWriteResponse{}, driver.NewOperationError(driver.KindValidation, "document id is required")
		}
		loaded, readErr := s.service.ReadDocument(ctx, request.Collection, *request.ID)
		document, err = &loaded, readErr
	case driver.DocumentWriteInsert:
		if request.Document == nil {
			return driver.DocumentWriteResponse{}, driver.NewOperationError(driver.KindValidation, "document is required")
		}
		result, err = s.service.InsertDocument(ctx, request.Collection, *request.Document)
	case driver.DocumentWriteReplace:
		if request.ID == nil || request.Document == nil {
			return driver.DocumentWriteResponse{}, driver.NewOperationError(driver.KindValidation, "document id and document are required")
		}
		result, err = s.service.ReplaceDocument(ctx, request.Collection, *request.ID, *request.Document)
	case driver.DocumentWriteDelete:
		if request.ID == nil {
			return driver.DocumentWriteResponse{}, driver.NewOperationError(driver.KindValidation, "document id is required")
		}
		result, err = s.service.DeleteDocument(ctx, request.Collection, *request.ID)
	default:
		return driver.DocumentWriteResponse{}, driver.NewOperationError(driver.KindValidation, "unsupported document-write operation")
	}
	if err != nil {
		return driver.DocumentWriteResponse{}, err
	}
	return driver.DocumentWriteResponse{Result: driver.WriteResult{RowsAffected: result.RowsAffected}, Document: document}, nil
}

var _ driver.SessionService = (*sessionService)(nil)
var _ driver.DocumentWriter = (*sessionService)(nil)
var _ driver.WorkspaceViewProvider = (*sessionService)(nil)
