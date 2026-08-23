package postgres

import (
	"context"
	stdsql "database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"testing"
)

func BenchmarkValidateCache(b *testing.B) {
	db := stdsql.OpenDB(postgresBenchmarkConnector{})
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

type postgresBenchmarkConnector struct{}

func (postgresBenchmarkConnector) Connect(context.Context) (sqldriver.Conn, error) {
	return postgresBenchmarkConn{}, nil
}

func (postgresBenchmarkConnector) Driver() sqldriver.Driver { return postgresBenchmarkDriver{} }

type postgresBenchmarkDriver struct{}

func (postgresBenchmarkDriver) Open(string) (sqldriver.Conn, error) {
	return postgresBenchmarkConn{}, nil
}

type postgresBenchmarkConn struct{}

func (postgresBenchmarkConn) Prepare(query string) (sqldriver.Stmt, error) {
	return postgresBenchmarkStmt{}, nil
}

func (postgresBenchmarkConn) PrepareContext(ctx context.Context, query string) (sqldriver.Stmt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return postgresBenchmarkStmt{}, nil
}

func (postgresBenchmarkConn) Close() error { return nil }

func (postgresBenchmarkConn) Begin() (sqldriver.Tx, error) {
	return nil, errors.New("benchmark transaction is not supported")
}

type postgresBenchmarkStmt struct{}

func (postgresBenchmarkStmt) Close() error { return nil }

func (postgresBenchmarkStmt) NumInput() int { return -1 }

func (postgresBenchmarkStmt) Exec([]sqldriver.Value) (sqldriver.Result, error) {
	return nil, errors.New("benchmark statement execution is not supported")
}

func (postgresBenchmarkStmt) Query([]sqldriver.Value) (sqldriver.Rows, error) {
	return nil, errors.New("benchmark statement query is not supported")
}
