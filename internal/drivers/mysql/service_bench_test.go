package mysql

import (
	"context"
	stdsql "database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"testing"
)

func BenchmarkValidateCache(b *testing.B) {
	db := stdsql.OpenDB(mysqlBenchmarkConnector{})
	service := &Service{db: db}
	b.Cleanup(func() { _ = db.Close() })

	const statement = "SELECT id, name FROM projects WHERE id = 42"
	if err := service.Validate(context.Background(), statement); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := service.Validate(context.Background(), statement); err != nil {
			b.Fatal(err)
		}
	}
}

type mysqlBenchmarkConnector struct{}

func (mysqlBenchmarkConnector) Connect(context.Context) (sqldriver.Conn, error) {
	return mysqlBenchmarkConn{}, nil
}

func (mysqlBenchmarkConnector) Driver() sqldriver.Driver { return mysqlBenchmarkDriver{} }

type mysqlBenchmarkDriver struct{}

func (mysqlBenchmarkDriver) Open(string) (sqldriver.Conn, error) { return mysqlBenchmarkConn{}, nil }

type mysqlBenchmarkConn struct{}

func (mysqlBenchmarkConn) Prepare(query string) (sqldriver.Stmt, error) {
	return mysqlBenchmarkStmt{}, nil
}

func (mysqlBenchmarkConn) PrepareContext(ctx context.Context, query string) (sqldriver.Stmt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return mysqlBenchmarkStmt{}, nil
}

func (mysqlBenchmarkConn) Close() error { return nil }

func (mysqlBenchmarkConn) Begin() (sqldriver.Tx, error) {
	return nil, errors.New("benchmark transaction is not supported")
}

type mysqlBenchmarkStmt struct{}

func (mysqlBenchmarkStmt) Close() error { return nil }

func (mysqlBenchmarkStmt) NumInput() int { return -1 }

func (mysqlBenchmarkStmt) Exec([]sqldriver.Value) (sqldriver.Result, error) {
	return nil, errors.New("benchmark statement execution is not supported")
}

func (mysqlBenchmarkStmt) Query([]sqldriver.Value) (sqldriver.Rows, error) {
	return nil, errors.New("benchmark statement query is not supported")
}
