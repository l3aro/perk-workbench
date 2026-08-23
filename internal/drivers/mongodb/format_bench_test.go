package mongodb

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func BenchmarkDocumentsResult(b *testing.B) {
	documents := []bson.D{
		{
			{Key: "_id", Value: bson.NewObjectID()},
			{Key: "name", Value: "Ada Lovelace"},
			{Key: "active", Value: true},
			{Key: "score", Value: int64(42)},
			{Key: "created", Value: time.Date(2026, time.August, 23, 12, 34, 56, 0, time.UTC)},
			{Key: "profile", Value: bson.D{
				{Key: "role", Value: "engineer"},
				{Key: "location", Value: bson.D{{Key: "city", Value: "London"}, {Key: "rank", Value: int32(1)}}},
			}},
			{Key: "tags", Value: bson.A{"sql", "go", bson.D{{Key: "level", Value: "advanced"}}}},
			{Key: "payload", Value: bson.Binary{Subtype: 0x80, Data: []byte{0x00, 0x01, 0x7f, 0xfe, 0xff}}},
			{Key: "pattern", Value: bson.Regex{Pattern: "^ada", Options: "im"}},
			{Key: "timestamp", Value: bson.Timestamp{T: 1735000000, I: 7}},
			{Key: "decimal", Value: bson.NewDecimal128(123, 456)},
			{Key: "missing", Value: bson.MinKey{}},
		},
		{
			{Key: "_id", Value: bson.NewObjectID()},
			{Key: "name", Value: "Grace Hopper"},
			{Key: "active", Value: false},
			{Key: "created", Value: bson.DateTime(time.Date(2025, time.March, 14, 9, 26, 53, 0, time.UTC).UnixMilli())},
			{Key: "profile", Value: bson.M{"role": "scientist", "skills": bson.A{"COBOL", "compiler"}}},
			{Key: "tags", Value: bson.A{"database", nil, int32(7)}},
			{Key: "payload", Value: []byte{0xde, 0xad, 0xbe, 0xef}},
			{Key: "code", Value: bson.CodeWithScope{Code: "function () { return true; }", Scope: bson.D{{Key: "enabled", Value: true}}}},
			{Key: "undefined", Value: bson.Undefined{}},
		},
	}

	b.ReportAllocs()
	var resultColumns int
	for b.Loop() {
		result := documentsResult(documents, true, 0)
		resultColumns = len(result.Columns)
	}
	if resultColumns == 0 {
		b.Fatal("documentsResult returned no columns")
	}
}
