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
	ListSchema(context.Context) ([]SchemaObject, error)
	TableInfo(context.Context, string) ([]ColumnInfo, error)
	AlterColumn(context.Context, string, ColumnChange) error
	BrowseTable(context.Context, string, int, int) (Result, error)
}

type DatabaseInfo struct {
	Product string
	Version string
}

type Result struct {
	Columns      []string
	Rows         [][]*string
	RowsAffected int64
	Duration     time.Duration
	Truncated    bool
}

type SchemaObject struct {
	Type string
	Name string
}

type ColumnInfo struct {
	Name         string
	Type         string
	Nullable     bool
	DefaultValue *string
	PrimaryKey   int
}

type ColumnChange struct {
	PreviousName string
	Name         string
	Type         string
	Nullable     bool
	DefaultValue *string
}
