package sql

import (
	"context"
	"time"
)

const (
	MaxRows  = 500
	MaxRunes = 300
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
	CreateForeignKey(context.Context, string, ForeignKeyChange) error
	ReplaceForeignKey(context.Context, string, string, ForeignKeyChange) error
	DropForeignKey(context.Context, string, string) error
	AlterColumn(context.Context, string, ColumnChange) error
	DropColumn(context.Context, string, string) error
	AddColumn(context.Context, string, ColumnDef) error
	BrowseTable(context.Context, string, BrowseOptions) (Result, error)
}
type BrowseOptions struct {
	Columns       []string
	Filters       []BrowseFilter
	Sorts         []BrowseSort
	Offset, Limit int
}

type BrowseFilterOperator string

const (
	BrowseFilterNone         BrowseFilterOperator = ""
	BrowseFilterLike         BrowseFilterOperator = "LIKE"
	BrowseFilterNotLike      BrowseFilterOperator = "NOT LIKE"
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
	Column   string
	Operator BrowseFilterOperator
	Value    string
}

type BrowseSort struct {
	Column     string
	Descending bool
}

type DatabaseInfo struct {
	Product string
	Version string
}

type Opened struct {
	Target  string
	Service Service
	Info    DatabaseInfo
	Objects []SchemaObject
}

type Result struct {
	Columns         []string
	ColumnTypes     []string
	Rows            [][]*string
	UntruncatedRows [][]*string
	RowsAffected    int64
	HasMore         bool
	Duration        time.Duration
	Truncated       bool
}

type SchemaObject struct {
	Database string
	Type     string
	Name     string
}

type IndexKind uint8

const (
	IndexPrimaryKey IndexKind = iota + 1
	IndexUnique
	IndexRegular
)

type ColumnInfo struct {
	Name         string
	Type         string
	Attributes   string
	Nullable     bool
	DefaultValue *string
	PrimaryKey   int
	Indexes      []IndexKind
}

type ColumnDef struct {
	Name         string
	Type         string
	Nullable     bool
	DefaultValue *string
	Attributes   *string
}

type ColumnChange struct {
	PreviousName string
	Name         string
	Type         string
	Nullable     bool
	DefaultValue *string
	Attributes   *string
}
