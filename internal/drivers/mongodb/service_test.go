package mongodb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// testURI returns the MongoDB URI for integration tests, or "" when the
// tests should skip. Point it at any disposable server, e.g.
// PERK_WORKBENCH_TEST_MONGO_URI=mongodb://localhost:27017/perk-test
func testURI() string {
	return os.Getenv("PERK_WORKBENCH_TEST_MONGO_URI")
}

func openTestService(t *testing.T) *Service {
	t.Helper()
	if testURI() == "" {
		t.Skip("PERK_WORKBENCH_TEST_MONGO_URI not set; skipping live MongoDB integration test")
	}
	service, err := Open(context.Background(), testURI())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func TestIntegration_documentsAndSchema(t *testing.T) {
	service := openTestService(t)
	ctx := context.Background()

	if service.Info().Product != "MongoDB" {
		t.Fatalf("product = %q, want MongoDB", service.Info().Product)
	}

	// Seed a known document through the driver itself.
	if _, err := service.Execute(ctx, `db.perk_test.drop()`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := service.Execute(ctx, `db.perk_test.insertMany([
		{"name": "first", "score": 10, "address": {"building": "1007", "coord": [-73.85, 40.84], "street": "Morris Park Ave", "zipcode": "10462"}},
		{"name": "second", "score": 20},
		{"name": "third", "score": 30}
	])`); err != nil {
		t.Fatalf("insertMany: %v", err)
	}

	result, err := service.Execute(ctx, `db.perk_test.find({"score": {"$gte": 20}}).sort({"score": 1})`)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("find rows = %d, want 2", len(result.Rows))
	}
	if result.Columns[0] != "_id" {
		t.Fatalf("columns = %v, want _id first", result.Columns)
	}
	// The raw cell of an object column must be valid JSON for the cell viewer.
	for columnIndex, column := range result.Columns {
		if column != "address" {
			continue
		}
		raw := result.UntruncatedRows[0][columnIndex]
		if raw == nil {
			t.Fatal("address raw cell is nil")
		}
		var parsed bson.D
		if err := bson.UnmarshalExtJSON([]byte(*raw), false, &parsed); err != nil {
			t.Fatalf("address raw cell is not valid JSON: %v\n%s", err, *raw)
		}
	}

	if _, err := service.Execute(ctx, `db.perk_test.insertOne({"name": "bin", "data": {"$binary": {"base64": "3q0=", "subType": "80"}}})`); err != nil {
		t.Fatalf("insertOne binary: %v", err)
	}
	result, err = service.Execute(ctx, `db.perk_test.find({"name": "bin"})`)
	if err != nil {
		t.Fatalf("find binary: %v", err)
	}
	for columnIndex, column := range result.Columns {
		if column != "data" {
			continue
		}
		if result.ColumnTypes[columnIndex] != "binary" {
			t.Fatalf("binary column type = %q, want binary", result.ColumnTypes[columnIndex])
		}
		if got := *result.UntruncatedRows[0][columnIndex]; got != `{"$binary":{"base64":"3q0=","subType":"80"}}` {
			t.Fatalf("binary raw cell = %q, want $binary JSON", got)
		}
	}

	if _, err := service.Execute(ctx, `db.perk_test.insertOne({"name": "rx", "pattern": {"$regularExpression": {"pattern": "^a.*", "options": "i"}}})`); err != nil {
		t.Fatalf("insertOne regex: %v", err)
	}
	result, err = service.Execute(ctx, `db.perk_test.find({"name": "rx"})`)
	if err != nil {
		t.Fatalf("find regex: %v", err)
	}
	for columnIndex, column := range result.Columns {
		if column != "pattern" {
			continue
		}
		if result.ColumnTypes[columnIndex] != "regex" {
			t.Fatalf("regex column type = %q, want regex", result.ColumnTypes[columnIndex])
		}
		if got := *result.Rows[0][columnIndex]; got != "/^a.*/i" {
			t.Fatalf("regex display cell = %q, want /^a.*/i", got)
		}
		if got := *result.UntruncatedRows[0][columnIndex]; got != `{"$regularExpression":{"pattern":"^a.*","options":"i"}}` {
			t.Fatalf("regex raw cell = %q, want $regularExpression JSON", got)
		}
	}

	if _, err := service.Execute(ctx, `db.perk_test.insertOne({"name": "arr", "tags": ["a", "b", {"k": 1}]})`); err != nil {
		t.Fatalf("insertOne array: %v", err)
	}
	result, err = service.Execute(ctx, `db.perk_test.find({"name": "arr"})`)
	if err != nil {
		t.Fatalf("find array: %v", err)
	}
	for columnIndex, column := range result.Columns {
		if column != "tags" {
			continue
		}
		if result.ColumnTypes[columnIndex] != "array" {
			t.Fatalf("array column type = %q, want array", result.ColumnTypes[columnIndex])
		}
		if got := *result.Rows[0][columnIndex]; got != `["a", "b", {k: 1}]` {
			t.Fatalf("array display cell = %q, want mongosh style", got)
		}
		if got := *result.UntruncatedRows[0][columnIndex]; got != `["a","b",{"k":1}]` {
			t.Fatalf("array raw cell = %q, want JSON", got)
		}
	}

	count, err := service.Execute(ctx, `db.perk_test.countDocuments({"name": "first"})`)
	if err != nil {
		t.Fatalf("countDocuments: %v", err)
	}
	if got := *count.Rows[0][0]; got != "1" {
		t.Fatalf("count = %q, want 1", got)
	}

	distinct, err := service.Execute(ctx, `db.perk_test.distinct("score")`)
	if err != nil {
		t.Fatalf("distinct: %v", err)
	}
	if len(distinct.Rows) != 3 {
		t.Fatalf("distinct rows = %d, want 3", len(distinct.Rows))
	}

	// More than 500 distinct values must truncate with the flag set.
	bulk := strings.Builder{}
	bulk.WriteString(`db.perk_test.insertMany([`)
	for i := 0; i < 600; i++ {
		if i > 0 {
			bulk.WriteByte(',')
		}
		fmt.Fprintf(&bulk, `{"name": "bulk_%d", "score": %d}`, i, 1000+i)
	}
	bulk.WriteString(`])`)
	if _, err := service.Execute(ctx, bulk.String()); err != nil {
		t.Fatalf("insertMany bulk: %v", err)
	}
	distinct, err = service.Execute(ctx, `db.perk_test.distinct("score")`)
	if err != nil {
		t.Fatalf("distinct bulk: %v", err)
	}
	if len(distinct.Rows) != sharedsql.MaxRows || !distinct.Truncated {
		t.Fatalf("distinct bulk rows = %d truncated = %t, want %d with truncation", len(distinct.Rows), distinct.Truncated, sharedsql.MaxRows)
	}

	update, err := service.Execute(ctx, `db.perk_test.updateOne({"name": "first"}, {"$set": {"score": 11}})`)
	if err != nil {
		t.Fatalf("updateOne: %v", err)
	}
	if update.RowsAffected != 1 || *update.Rows[0][1] != "1" {
		t.Fatalf("update result = %+v", update)
	}

	delete, err := service.Execute(ctx, `db.perk_test.deleteMany({"score": {"$lt": 15}})`)
	if err != nil {
		t.Fatalf("deleteMany: %v", err)
	}
	if delete.RowsAffected != 1 {
		t.Fatalf("deleted = %d, want 1", delete.RowsAffected)
	}

	// Read-only mode blocks writes but allows reads.
	if _, err := service.ExecuteReadOnly(ctx, `db.perk_test.insertOne({"name": "blocked"})`); err == nil {
		t.Fatal("ExecuteReadOnly(insertOne) error = nil, want rejection")
	}

	objects, err := service.ListSchema(ctx)
	if err != nil {
		t.Fatalf("ListSchema: %v", err)
	}
	found := false
	for _, object := range objects {
		if object.Type == "collection" && object.Name == "perk_test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListSchema objects = %+v, want perk_test collection", objects)
	}

	columns, err := service.TableInfo(ctx, "perk_test")
	if err != nil {
		t.Fatalf("TableInfo: %v", err)
	}
	if len(columns) == 0 || columns[0].Name != "_id" || columns[0].PrimaryKey != 1 {
		t.Fatalf("TableInfo columns = %+v, want _id primary key first", columns)
	}

	browse, err := service.BrowseTable(ctx, "perk_test", sharedsql.BrowseOptions{
		Sorts: []sharedsql.BrowseSort{{Column: "score", Descending: true}},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("BrowseTable: %v", err)
	}
	if !browse.HasMore || len(browse.Rows) != 1 {
		t.Fatalf("BrowseTable rows = %d hasMore = %t, want 1 with more", len(browse.Rows), browse.HasMore)
	}

	indexes, err := service.ListIndexes(ctx, "perk_test")
	if err != nil {
		t.Fatalf("ListIndexes: %v", err)
	}
	if len(indexes) == 0 || !indexes[0].PrimaryKey || indexes[0].Name != "_id_" {
		t.Fatalf("indexes = %+v, want _id_ primary index", indexes)
	}

	if err := service.CreateIndex(ctx, "perk_test", sharedsql.IndexChange{Name: "name_idx", Columns: []string{"name"}, Unique: true}); err != nil {
		t.Fatalf("CreateIndex: %v", err)
	}
	if err := service.DropIndex(ctx, "perk_test", "name_idx"); err != nil {
		t.Fatalf("DropIndex: %v", err)
	}
	if err := service.DropIndex(ctx, "perk_test", "_id_"); err == nil {
		t.Fatal("DropIndex(_id_) error = nil, want rejection")
	}

	if _, err := service.ListForeignKeys(ctx, "perk_test"); err != nil {
		t.Fatalf("ListForeignKeys: %v", err)
	}
	if err := service.AlterColumn(ctx, "perk_test", sharedsql.ColumnChange{}); err == nil {
		t.Fatal("AlterColumn error = nil, want unsupported")
	}

	if err := service.Validate(ctx, `db.perk_test.find({"score": 20})`); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
	if err := service.Validate(ctx, `db.perk_test.find({not json)`); err == nil {
		t.Fatal("Validate(invalid) error = nil, want parse error")
	}

	if _, err := service.Execute(ctx, `db.perk_test.drop()`); err != nil {
		t.Fatalf("cleanup drop: %v", err)
	}
}
