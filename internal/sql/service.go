package sql

import (
	"context"
	"time"
)

const (
	MaxRows  = 500
	MaxRunes = 50
)

type Service interface {
	Close() error
	Info() DatabaseInfo
	Execute(context.Context, string) (Result, error)
	ExecuteReadOnly(context.Context, string) (Result, error)
	Validate(context.Context, string) error
	ListSchema(context.Context) ([]SchemaObject, error)
	TableInfo(context.Context, string) ([]ColumnInfo, error)
	ListIndexes(context.Context, string) ([]IndexInfo, error)
	CreateIndex(context.Context, string, IndexChange) error
	ReplaceIndex(context.Context, string, string, IndexChange) error
	DropIndex(context.Context, string, string) error
	ListForeignKeys(context.Context, string) ([]ForeignKeyInfo, error)
	ListReferencingForeignKeys(context.Context, string) ([]ReferencingForeignKeyInfo, error)
	// ListForeignKeysAll returns every foreign key in the connected schema,
	// keyed by the table that declares it. Products without foreign keys
	// (MongoDB) return an empty map. The app derives inbound edges by
	// scanning for ReferenceTable.
	ListForeignKeysAll(context.Context) (map[string][]ForeignKeyInfo, error)
	// ListIndexesAll returns every index in the connected schema, keyed by
	// table name (collection name for MongoDB).
	ListIndexesAll(context.Context) (map[string][]IndexInfo, error)
	CreateForeignKey(context.Context, string, ForeignKeyChange) error
	ReplaceForeignKey(context.Context, string, string, ForeignKeyChange) error
	DropForeignKey(context.Context, string, string) error
	AlterColumn(context.Context, string, ColumnChange) error
	DropColumn(context.Context, string, string) error
	AddColumn(context.Context, string, ColumnDef) error
	BrowseTable(context.Context, string, BrowseOptions) (Result, error)
}
type BrowseOptions struct {
	Columns []string       `json:"columns"`
	Filters []BrowseFilter `json:"filters"`
	Sorts   []BrowseSort   `json:"sorts"`
	Offset  int            `json:"offset"`
	Limit   int            `json:"limit"`
}

type BrowseFilterOperator string

const (
	BrowseFilterNone         BrowseFilterOperator = ""
	BrowseFilterLike         BrowseFilterOperator = "LIKE"
	BrowseFilterNotLike      BrowseFilterOperator = "NOT LIKE"
	BrowseFilterPattern      BrowseFilterOperator = "PATTERN"
	BrowseFilterNotPattern   BrowseFilterOperator = "NOT PATTERN"
	BrowseFilterEqual        BrowseFilterOperator = "="
	BrowseFilterNotEqual     BrowseFilterOperator = "!="
	BrowseFilterLess         BrowseFilterOperator = "<"
	BrowseFilterLessEqual    BrowseFilterOperator = "<="
	BrowseFilterGreater      BrowseFilterOperator = ">"
	BrowseFilterGreaterEqual BrowseFilterOperator = ">="
	BrowseFilterIsNull       BrowseFilterOperator = "IS NULL"
	BrowseFilterIsNotNull    BrowseFilterOperator = "IS NOT NULL"
)

type BrowseFilter struct {
	Column   string               `json:"column"`
	Operator BrowseFilterOperator `json:"operator"`
	Value    string               `json:"value"`
}

type BrowseSort struct {
	Column     string `json:"column"`
	Descending bool   `json:"descending"`
}

type DatabaseInfo struct {
	Product string `json:"product"`
	Version string `json:"version"`
}

type Opened struct {
	Target  string
	Service Service
	Info    DatabaseInfo
	Objects []SchemaObject
	// QueryLanguage advertises how the query editor presents this
	// connection's statements; a zero value (no advertisement) falls
	// back to the legacy SQL defaults in the UI.
	QueryLanguage QueryLanguage
	// Workspace advertises the connection's workspace tab capability:
	// the standard tabs it supports beyond Query/Browse and its custom
	// plain-data views. Nil keeps the legacy per-product tab policy
	// exactly; a non-nil advertisement is authoritative for the tab row.
	Workspace *WorkspaceCapability
}

type Result struct {
	Columns         []string      `json:"columns"`
	ColumnTypes     []string      `json:"column_types"`
	Rows            [][]*string   `json:"rows"`
	UntruncatedRows [][]*string   `json:"untruncated_rows"`
	RowsAffected    int64         `json:"rows_affected"`
	HasMore         bool          `json:"has_more"`
	Duration        time.Duration `json:"duration_ns"`
	Truncated       bool          `json:"truncated"`
	// DocumentIDs carries one stable document identity per row, parallel to
	// Rows, for document-capable browse results. Empty when the backend is
	// not document-capable or a row has no identity.
	DocumentIDs []DocumentPayload `json:"document_ids"`
	// Statement is an optional backend-native statement for the operation
	// that produced this result (external plugins return the exact command
	// they executed; compiled-in drivers leave it empty). The workbench
	// logs it in place of the generic write preview when non-blank.
	// Replayability and sensitivity are described by StatementMetadata.
	// Omitted from the wire when empty.
	Statement string `json:"statement,omitempty"`
	// StatementMetadata optionally describes Statement. It is meaningful
	// only when Statement is nonblank; orphan metadata (metadata without a
	// statement) is rejected at the plugin boundary. Omitted from the wire
	// when nil, so older plugins keep the prior shape.
	StatementMetadata *StatementMetadata `json:"statement_metadata,omitempty"`
}

// StatementMetadata is optional structured metadata for a backend-native
// statement. It is meaningful only when the accompanying statement is
// nonblank. A nil StatementMetadata (the object omitted from the wire)
// keeps the legacy defaults — replayable, not sensitive, no language —
// so a nonblank legacy statement without metadata keeps exactly its
// current behavior. When the object is present it is authoritative:
// plugins send all three fields, and the zero value of an absent field
// decodes as false/empty.
type StatementMetadata struct {
	Language   string `json:"language"`
	Replayable bool   `json:"replayable"`
	Sensitive  bool   `json:"sensitive"`
}

type SchemaObject struct {
	Database string `json:"database"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	// RowCount is the estimated row count where the engine exposes one
	// (PostgreSQL pg_class.reltuples, MySQL information_schema.table_rows);
	// nil when unknown or when only an exact count exists (SQLite, views).
	RowCount *int64 `json:"row_count"`
}

type IndexKind uint8

const (
	IndexPrimaryKey IndexKind = 1
	IndexUnique     IndexKind = 2
	IndexRegular    IndexKind = 3
)

type ColumnInfo struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	Attributes   string      `json:"attributes"`
	Nullable     bool        `json:"nullable"`
	DefaultValue *string     `json:"default_value"`
	PrimaryKey   int         `json:"primary_key"`
	Indexes      []IndexKind `json:"indexes"`
}

type ColumnDef struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Nullable     bool    `json:"nullable"`
	DefaultValue *string `json:"default_value"`
	Attributes   *string `json:"attributes"`
}

type ColumnChange struct {
	PreviousName string  `json:"previous_name"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Nullable     bool    `json:"nullable"`
	DefaultValue *string `json:"default_value"`
	Attributes   *string `json:"attributes"`
}
