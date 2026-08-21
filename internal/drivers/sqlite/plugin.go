package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func validationError(err error) error {
	if err == nil {
		return nil
	}
	return driver.NewOperationError(driver.KindValidation, err.Error())
}

func operationError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*driver.OperationError); ok {
		return err
	}
	return driver.NewOperationError(driver.KindOperation, err.Error())
}

type sessionService struct{ service *Service }

func (s *sessionService) Execute(ctx context.Context, req driver.StatementRequest) (driver.Result, error) {
	if err := ValidateStatement(req.Statement); err != nil {
		return driver.Result{}, validationError(err)
	}
	result, err := s.service.Execute(ctx, req.Statement)
	return result, operationError(err)
}
func (s *sessionService) ExecuteReadOnly(ctx context.Context, req driver.StatementRequest) (driver.Result, error) {
	if err := ValidateStatement(req.Statement); err != nil {
		return driver.Result{}, validationError(err)
	}
	result, err := s.service.ExecuteReadOnly(ctx, req.Statement)
	return result, operationError(err)
}
func (s *sessionService) Validate(ctx context.Context, req driver.StatementRequest) error {
	if err := ValidateStatement(req.Statement); err != nil {
		return validationError(err)
	}
	return operationError(s.service.Validate(ctx, req.Statement))
}
func (s *sessionService) ListSchema(ctx context.Context, _ driver.EmptyRequest) ([]driver.SchemaObject, error) {
	result, err := s.service.ListSchema(ctx)
	return result, operationError(err)
}
func (s *sessionService) TableInfo(ctx context.Context, req driver.TableRequest) ([]driver.ColumnInfo, error) {
	result, err := s.service.TableInfo(ctx, req.Table)
	return result, operationError(err)
}
func (s *sessionService) ListIndexes(ctx context.Context, req driver.TableRequest) ([]driver.IndexInfo, error) {
	result, err := s.service.ListIndexes(ctx, req.Table)
	return result, operationError(err)
}
func (s *sessionService) CreateIndex(ctx context.Context, req driver.IndexChangeRequest) error {
	if err := ValidateIndexChange(req.Change); err != nil {
		return validationError(err)
	}
	return operationError(s.service.CreateIndex(ctx, req.Table, req.Change))
}
func (s *sessionService) ReplaceIndex(ctx context.Context, req driver.ReplaceIndexRequest) error {
	if err := ValidateIndexChange(req.Change); err != nil {
		return validationError(err)
	}
	return operationError(s.service.ReplaceIndex(ctx, req.Table, req.OldName, req.Change))
}
func (s *sessionService) DropIndex(ctx context.Context, req driver.DropRequest) error {
	return operationError(s.service.DropIndex(ctx, req.Table, req.Name))
}
func (s *sessionService) ListForeignKeys(ctx context.Context, req driver.TableRequest) ([]driver.ForeignKeyInfo, error) {
	result, err := s.service.ListForeignKeys(ctx, req.Table)
	return result, operationError(err)
}
func (s *sessionService) ListReferencingForeignKeys(ctx context.Context, req driver.TableRequest) ([]driver.ReferencingForeignKeyInfo, error) {
	result, err := s.service.ListReferencingForeignKeys(ctx, req.Table)
	return result, operationError(err)
}
func (s *sessionService) ListForeignKeysAll(ctx context.Context, _ driver.EmptyRequest) (map[string][]driver.ForeignKeyInfo, error) {
	result, err := s.service.ListForeignKeysAll(ctx)
	return result, operationError(err)
}
func (s *sessionService) ListIndexesAll(ctx context.Context, _ driver.EmptyRequest) (map[string][]driver.IndexInfo, error) {
	result, err := s.service.ListIndexesAll(ctx)
	return result, operationError(err)
}
func (s *sessionService) CreateForeignKey(ctx context.Context, req driver.ForeignKeyChangeRequest) error {
	if err := ValidateForeignKeyChange(req.Change); err != nil {
		return validationError(err)
	}
	return operationError(s.service.CreateForeignKey(ctx, req.Table, req.Change))
}
func (s *sessionService) ReplaceForeignKey(ctx context.Context, req driver.ReplaceForeignKeyRequest) error {
	if err := ValidateForeignKeyChange(req.Change); err != nil {
		return validationError(err)
	}
	return operationError(s.service.ReplaceForeignKey(ctx, req.Table, req.OldName, req.Change))
}
func (s *sessionService) DropForeignKey(ctx context.Context, req driver.DropRequest) error {
	return operationError(s.service.DropForeignKey(ctx, req.Table, req.Name))
}
func (s *sessionService) AlterColumn(ctx context.Context, req driver.ColumnChangeRequest) error {
	if err := ValidateColumnChange(req.Change); err != nil {
		return validationError(err)
	}
	return operationError(s.service.AlterColumn(ctx, req.Table, req.Change))
}
func (s *sessionService) DropColumn(ctx context.Context, req driver.DropRequest) error {
	return operationError(s.service.DropColumn(ctx, req.Table, req.Name))
}
func (s *sessionService) AddColumn(ctx context.Context, req driver.AddColumnRequest) error {
	if err := ValidateColumnDef(req.Def); err != nil {
		return validationError(err)
	}
	return operationError(s.service.AddColumn(ctx, req.Table, req.Def))
}
func (s *sessionService) BrowseTable(ctx context.Context, req driver.BrowseTableRequest) (driver.Result, error) {
	if req.Options.Offset < 0 || req.Options.Limit < 1 || req.Options.Limit > maxRows {
		return driver.Result{}, validationError(fmt.Errorf("invalid browse range: offset=%d limit=%d", req.Options.Offset, req.Options.Limit))
	}
	result, err := s.service.BrowseTable(ctx, req.Table, req.Options)
	return result, operationError(err)
}
func (s *sessionService) Close() error { return operationError(s.service.Close()) }

func (s *sessionService) RowWrite(ctx context.Context, req driver.RowWriteRequest) (driver.RowWriteResponse, error) {
	var result driver.Result
	var err error
	switch req.Operation {
	case driver.RowWriteInsert:
		result, err = s.service.InsertRow(ctx, req.Table, req.Values)
	case driver.RowWriteUpdate:
		result, err = s.service.UpdateRow(ctx, req.Table, req.Key, req.Values)
	case driver.RowWriteDelete:
		result, err = s.service.DeleteRow(ctx, req.Table, req.Key)
	default:
		return driver.RowWriteResponse{}, validationError(fmt.Errorf("unsupported row write operation %q", req.Operation))
	}
	return driver.RowWriteResponse{Result: driver.WriteResult{RowsAffected: result.RowsAffected}}, operationError(err)
}

func (s *sessionService) isValid() bool { return s != nil && s.service != nil }

var _ driver.SessionService = (*sessionService)(nil)
var _ driver.RowWriter = (*sessionService)(nil)

// Factory is the standalone SQLite plugin implementation exposed to server.Run.
type Factory struct{}

func (Factory) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		Name:              "sqlite",
		Driver:            "sqlite",
		Display:           "SQLite",
		Targets:           []driver.TargetPattern{{Prefix: "sqlite:"}},
		Form:              &driver.FormSpec{Fields: []driver.FormField{{Key: "target", Title: "Target*", Kind: driver.FormFieldInput, Placeholder: "path/to/database.db or :memory:", Validate: driver.FormValidationRequired, Error: "target is required"}}},
		QueryLanguage:     &driver.QueryLanguage{Name: "SQL", EditorLabel: "SQL", Placeholder: "Enter a query…", Lexer: "sql"},
		WriteCapabilities: driver.WriteCapabilities{RowWriter: true},
	}
}
func (Factory) BuildTarget(_ context.Context, values driver.FormValues) (driver.BuildTargetResult, error) {
	target := strings.TrimSpace(values.Database)
	target = strings.TrimPrefix(target, "sqlite:")
	target = strings.TrimSpace(target)
	if target == "" {
		return driver.BuildTargetResult{OK: false}, validationError(fmt.Errorf("target is required"))
	}
	return driver.BuildTargetResult{Target: target, OK: true}, nil
}
func (Factory) Open(ctx context.Context, target string) (driver.OpenResult, error) {
	service, err := Open(ctx, target)
	if err != nil {
		return driver.OpenResult{}, driver.NewOperationError(driver.KindConnection, "opening sqlite database failed")
	}
	return driver.OpenResult{Info: service.Info(), Service: &sessionService{service: service}}, nil
}

var _ driver.Factory = Factory{}
