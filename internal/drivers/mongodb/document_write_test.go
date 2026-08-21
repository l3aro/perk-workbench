package mongodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

// TestDocumentsResult_emitsDocumentIDs proves browse results carry one
// stable extended-JSON identity per row, independent of the display cell:
// an ObjectID renders as ObjectId("...") but its DocumentIDs entry is the
// declared Mongo extended JSON format.
func TestDocumentsResult_emitsDocumentIDs(t *testing.T) {
	id := bson.NewObjectID()
	docs := []bson.D{
		{{Key: "_id", Value: id}, {Key: "name", Value: "first"}},
		{{Key: "_id", Value: bson.NewObjectID()}, {Key: "name", Value: "second"}},
	}
	result := documentsResult(docs, false, 0)
	if len(result.DocumentIDs) != 2 {
		t.Fatalf("DocumentIDs = %d entries, want 2", len(result.DocumentIDs))
	}
	for index, payload := range result.DocumentIDs {
		if payload.Format != driver.DocumentFormatMongoExtendedJSON {
			t.Fatalf("DocumentIDs[%d].Format = %q, want Mongo extended JSON", index, payload.Format)
		}
		if len(payload.Data) == 0 {
			t.Fatalf("DocumentIDs[%d].Data is empty", index)
		}
	}
	// The visible _id cell keeps its mongosh form.
	if got := *result.Rows[0][0]; !strings.HasPrefix(got, `ObjectId("`) {
		t.Fatalf("display cell = %q, want ObjectId(\"...\")", got)
	}
	// The identity round-trips back to the same ObjectID.
	identity, err := parseIdentity(result.DocumentIDs[0])
	if err != nil {
		t.Fatalf("parseIdentity: %v", err)
	}
	if got := identity.(bson.ObjectID); got != id {
		t.Fatalf("parsed identity = %v, want %v", got, id)
	}
	// Rows without _id carry an empty payload.
	summary := documentsResult([]bson.D{{{Key: "count", Value: 1}}}, false, 0)
	if len(summary.DocumentIDs) != 1 || summary.DocumentIDs[0].Format != "" || len(summary.DocumentIDs[0].Data) != 0 {
		t.Fatalf("identity-less DocumentIDs = %#v, want empty payload", summary.DocumentIDs)
	}
}

func TestDocumentWrite_capabilities(t *testing.T) {
	service := openTestService(t)
	capabilities := service.WriteCapabilities()
	if capabilities.RowWriter {
		t.Fatal("MongoDB advertises RowWriter")
	}
	if capabilities.Document == nil || !capabilities.Document.Text || capabilities.Document.Format != driver.DocumentFormatMongoExtendedJSON {
		t.Fatalf("capabilities = %#v, want editable Mongo extended JSON document", capabilities)
	}
}

// TestIntegration_documentWriteRoundTrip exercises insert, read, replace,
// and delete against a document carrying string, nested object, array,
// date, and ObjectID values.
func TestIntegration_documentWriteRoundTrip(t *testing.T) {
	service := openTestService(t)
	ctx := context.Background()
	collection := "perk_document_write_test"
	if _, err := service.Execute(ctx, `db.perk_document_write_test.drop()`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	format := driver.DocumentFormatMongoExtendedJSON

	document := `{"name": "widget", "meta": {"tags": ["a", "b"]}, "scores": [1, 2.5], "born": {"$date": "2000-01-02T03:04:05Z"}, "ref": {"$oid": "000000000000000000000001"}}`
	inserted, err := service.InsertDocument(ctx, collection, driver.DocumentPayload{Format: format, Data: []byte(document)})
	if err != nil {
		t.Fatalf("InsertDocument: %v", err)
	}
	if inserted.RowsAffected != 1 {
		t.Fatalf("insert RowsAffected = %d, want 1", inserted.RowsAffected)
	}

	browse, err := service.BrowseTable(ctx, collection, driver.BrowseOptions{Limit: 10})
	if err != nil {
		t.Fatalf("BrowseTable: %v", err)
	}
	if len(browse.DocumentIDs) != 1 {
		t.Fatalf("DocumentIDs = %d, want 1", len(browse.DocumentIDs))
	}
	identity := browse.DocumentIDs[0]

	loaded, err := service.ReadDocument(ctx, collection, identity)
	if err != nil {
		t.Fatalf("ReadDocument: %v", err)
	}
	var decoded bson.D
	if err := bson.UnmarshalExtJSON(loaded.Data, false, &decoded); err != nil {
		t.Fatalf("loaded document is not valid extended JSON: %v", err)
	}
	born, ok := docValue(decoded, "born")
	if !ok {
		t.Fatal("loaded document lost the born field")
	}
	var bornTime time.Time
	switch value := born.(type) {
	case time.Time:
		bornTime = value
	case bson.DateTime:
		bornTime = value.Time()
	default:
		t.Fatalf("born = %T, want a date", born)
	}
	if got := bornTime.UTC().Format(time.RFC3339); got != "2000-01-02T03:04:05Z" {
		t.Fatalf("born = %v, want 2000-01-02T03:04:05Z", got)
	}

	// Replace: same _id, updated body; RowsAffected = MatchedCount.
	updated := `{"name": "widget v2", "meta": {"tags": ["a"]}}`
	replaced, err := service.ReplaceDocument(ctx, collection, identity, driver.DocumentPayload{Format: format, Data: []byte(updated)})
	if err != nil {
		t.Fatalf("ReplaceDocument: %v", err)
	}
	if replaced.RowsAffected != 1 {
		t.Fatalf("replace RowsAffected = %d, want 1", replaced.RowsAffected)
	}
	loaded, err = service.ReadDocument(ctx, collection, identity)
	if err != nil {
		t.Fatalf("ReadDocument after replace: %v", err)
	}
	if !strings.Contains(string(loaded.Data), "widget v2") {
		t.Fatalf("replaced document = %s, want widget v2", loaded.Data)
	}
	if got, want := string(loaded.Data), ""; got == want {
		t.Fatalf("replaced document lost the _id: %s", loaded.Data)
	}

	// Replace with an explicit matching _id is accepted.
	withID := `{"_id": ` + string(identity.Data) + `, "name": "widget v3"}`
	replaced, err = service.ReplaceDocument(ctx, collection, identity, driver.DocumentPayload{Format: format, Data: []byte(withID)})
	if err != nil {
		t.Fatalf("ReplaceDocument with explicit id: %v", err)
	}
	if replaced.RowsAffected != 1 {
		t.Fatalf("replace RowsAffected = %d, want 1", replaced.RowsAffected)
	}

	// Replace with a mismatched _id is rejected without mutating data.
	mismatched := `{"_id": {"$oid": "0000000000000000000000ff"}, "name": "evil"}`
	if _, err := service.ReplaceDocument(ctx, collection, identity, driver.DocumentPayload{Format: format, Data: []byte(mismatched)}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want mismatched-_id rejection", err)
	}
	loaded, err = service.ReadDocument(ctx, collection, identity)
	if err != nil {
		t.Fatalf("ReadDocument after rejected replace: %v", err)
	}
	if strings.Contains(string(loaded.Data), "evil") {
		t.Fatalf("rejected replace mutated the document: %s", loaded.Data)
	}

	// Delete; a second delete reports 0 affected.
	deleted, err := service.DeleteDocument(ctx, collection, identity)
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if deleted.RowsAffected != 1 {
		t.Fatalf("delete RowsAffected = %d, want 1", deleted.RowsAffected)
	}
	deleted, err = service.DeleteDocument(ctx, collection, identity)
	if err != nil {
		t.Fatalf("DeleteDocument again: %v", err)
	}
	if deleted.RowsAffected != 0 {
		t.Fatalf("second delete RowsAffected = %d, want 0", deleted.RowsAffected)
	}
}

func TestDocumentWrite_rejectsBadPayloads(t *testing.T) {
	service := openTestService(t)
	ctx := context.Background()
	format := driver.DocumentFormatMongoExtendedJSON
	collection := "perk_document_write_bad"

	other := driver.DocumentPayload{Format: "application/x-other", Data: []byte("{}")}
	valid := driver.DocumentPayload{Format: format, Data: []byte(`{"name": "x"}`)}
	scalarID := driver.DocumentPayload{Format: format, Data: []byte(`{"$oid": "000000000000000000000001"}`)}

	// Mismatched formats are rejected by every operation.
	if _, err := service.InsertDocument(ctx, collection, other); err == nil || !strings.Contains(err.Error(), "unsupported document format") {
		t.Fatalf("insert error = %v, want format rejection", err)
	}
	if _, err := service.DeleteDocument(ctx, collection, other); err == nil || !strings.Contains(err.Error(), "unsupported document format") {
		t.Fatalf("delete error = %v, want format rejection", err)
	}
	if _, err := service.ReplaceDocument(ctx, collection, other, valid); err == nil || !strings.Contains(err.Error(), "unsupported document format") {
		t.Fatalf("replace id error = %v, want format rejection", err)
	}
	if _, err := service.ReplaceDocument(ctx, collection, scalarID, other); err == nil || !strings.Contains(err.Error(), "unsupported document format") {
		t.Fatalf("replace doc error = %v, want format rejection", err)
	}
	if _, err := service.ReadDocument(ctx, collection, other); err == nil || !strings.Contains(err.Error(), "unsupported document format") {
		t.Fatalf("read error = %v, want format rejection", err)
	}

	// Malformed text fails without mutating data.
	if _, err := service.InsertDocument(ctx, collection, driver.DocumentPayload{Format: format, Data: []byte(`{"name": `)}); err == nil {
		t.Fatal("malformed insert succeeded")
	}
	// Non-document bodies (arrays, scalars) fail.
	if _, err := service.InsertDocument(ctx, collection, driver.DocumentPayload{Format: format, Data: []byte(`[1, 2]`)}); err == nil {
		t.Fatal("array insert succeeded")
	}
	if _, err := service.InsertDocument(ctx, collection, driver.DocumentPayload{Format: format, Data: []byte(`42`)}); err == nil {
		t.Fatal("scalar insert succeeded")
	}
	// Nothing was inserted.
	count, err := service.db.Collection(collection).CountDocuments(ctx, bson.D{})
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 0 {
		t.Fatalf("bad payloads mutated the collection: %d documents", count)
	}
}

// TestDocumentWrite_rejectsNonScalarIdentity guards the identity contract:
// a nested document or array cannot identify a row.
func TestDocumentWrite_rejectsNonScalarIdentity(t *testing.T) {
	service := openTestService(t)
	ctx := context.Background()
	format := driver.DocumentFormatMongoExtendedJSON

	doc := driver.DocumentPayload{Format: format, Data: []byte(`{"a": 1}`)}
	if _, err := service.DeleteDocument(ctx, "perk_document_write_bad", doc); err == nil || !strings.Contains(err.Error(), "scalar") {
		t.Fatalf("error = %v, want scalar-identity rejection", err)
	}
	arr := driver.DocumentPayload{Format: format, Data: []byte(`[1]`)}
	if _, err := service.DeleteDocument(ctx, "perk_document_write_bad", arr); err == nil {
		t.Fatal("array identity accepted")
	}
}
