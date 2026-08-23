package mongodb

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

func TestFormatValue(t *testing.T) {
	id := bson.ObjectID{0x56, 0xd6, 0x10, 0x33, 0xa3, 0x78, 0xec, 0xcd, 0xe8, 0xa8, 0x35, 0x4f}
	for _, test := range []struct {
		value any
		want  string
	}{
		{value: nil, want: "null"},
		{value: "Bronx", want: "Bronx"},
		{value: true, want: "true"},
		{value: int32(42), want: "42"},
		{value: int64(25359), want: "25359"},
		{value: 3.5, want: "3.5"},
		{value: id, want: `ObjectId("56d61033a378eccde8a8354f")`},
		{value: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), want: "2024-01-02T03:04:05Z"},
		{value: bson.D{{Key: "borough", Value: "Bronx"}, {Key: "count", Value: int32(3)}},
			want: `{borough: "Bronx", count: 3}`},
		{value: bson.A{"a", int32(2)}, want: `["a", 2]`},
		{value: bson.M{"z": int32(1), "a": "x"}, want: `{a: "x", z: 1}`},
		{value: bson.Binary{Subtype: 0x80, Data: []byte{0xde, 0xad}}, want: "0xDEAD"},
		{value: []byte{0xde, 0xad}, want: "0xDEAD"},
	} {
		if got := formatValue(test.value); got != test.want {
			t.Errorf("formatValue(%#v) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestFormatCell_reusesCompactForValuesWithoutExtendedJSON(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "nil", value: nil},
		{name: "string", value: "Bronx"},
		{name: "number", value: int64(42)},
		{name: "object id", value: bson.ObjectID{1, 2, 3}},
		{name: "date", value: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cell := formatCell(test.value)
			if cell.compact != cell.full {
				t.Fatalf("formatCell(%#v) = compact %q, full %q; want the same value", test.value, cell.compact, cell.full)
			}
		})
	}
}

func TestFormatCell_formatsExtendedJSONOnlyForStructuredValues(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "document", value: bson.D{{Key: "name", Value: "Bronx"}}},
		{name: "array", value: bson.A{"Bronx"}},
		{name: "binary", value: bson.Binary{Subtype: 0x80, Data: []byte{0xde, 0xad}}},
		{name: "special", value: bson.Regex{Pattern: "^a.*", Options: "i"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cell := formatCell(test.value)
			if got, want := cell.compact, formatValue(test.value); got != want {
				t.Fatalf("compact value = %q, want %q", got, want)
			}
			if got, want := cell.full, formatViewValue(test.value); got != want {
				t.Fatalf("full value = %q, want %q", got, want)
			}
		})
	}
}

func TestDocumentColumns_idFirstThenSorted(t *testing.T) {
	docs := []bson.D{
		{{Key: "name", Value: "a"}, {Key: "_id", Value: "1"}},
		{{Key: "borough", Value: "b"}},
	}
	got := documentColumns(docs)
	want := []string{"_id", "borough", "name"}
	if len(got) != len(want) {
		t.Fatalf("documentColumns() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("documentColumns() = %v, want %v", got, want)
		}
	}
	if got := documentColumns(nil); len(got) != 0 {
		t.Fatalf("documentColumns(nil) = %v, want empty", got)
	}
}

func TestFormatViewValue_rendersObjectsAsExtendedJSON(t *testing.T) {
	id := bson.ObjectID{0x56, 0xd6, 0x10, 0x33, 0xa3, 0x78, 0xec, 0xcd, 0xe8, 0xa8, 0x35, 0x4f}
	doc := bson.D{
		{Key: "_id", Value: id},
		{Key: "address", Value: bson.D{{Key: "building", Value: "1007"}, {Key: "coord", Value: bson.A{float64(-73.85), float64(40.84)}}}},
	}
	got := formatViewValue(doc)
	// Valid extended JSON that round-trips to the same document.
	var parsed bson.D
	if err := bson.UnmarshalExtJSON([]byte(got), false, &parsed); err != nil {
		t.Fatalf("formatViewValue output is not valid JSON: %v\n%s", err, got)
	}
	if got == formatValue(doc) {
		t.Fatalf("view value equals compact display value %q", got)
	}
	// Scalars keep their plain-text form.
	if got := formatViewValue("Bronx"); got != "Bronx" {
		t.Fatalf("formatViewValue(string) = %q, want plain text", got)
	}
	if got := formatViewValue(int64(42)); got != "42" {
		t.Fatalf("formatViewValue(int) = %q, want plain text", got)
	}
	// Binary cells render as the mongoexport-compatible $binary form,
	// carrying the real subtype.
	binary := formatViewValue(bson.Binary{Subtype: 0x80, Data: []byte{0xde, 0xad}})
	want := `{"$binary":{"base64":"3q0=","subType":"80"}}`
	if binary != want {
		t.Fatalf("formatViewValue(binary) = %q, want %q", binary, want)
	}
	if got := formatViewValue([]byte{0xde, 0xad}); got != `{"$binary":{"base64":"3q0=","subType":"00"}}` {
		t.Fatalf("formatViewValue([]byte) = %q", got)
	}
	// Top-level arrays go through the same wrapper path and round-trip.
	array := formatViewValue(bson.A{int32(1), "two", bson.D{{Key: "n", Value: float64(3)}}})
	wantArray := `[1,"two",{"n":3.0}]`
	if array != wantArray {
		t.Fatalf("formatViewValue(array) = %q, want %q", array, wantArray)
	}
	var parsedArray bson.A
	if err := bson.UnmarshalExtJSON([]byte(array), false, &parsedArray); err != nil {
		t.Fatalf("array view value does not parse: %v\n%s", err, array)
	}
	// The driver's ext-JSON reader only resolves $binary inside a document;
	// a nested round-trip proves the emitted form is spec-correct.
	var nested bson.D
	if err := bson.UnmarshalExtJSON([]byte(`{"data": `+binary+`}`), false, &nested); err != nil {
		t.Fatalf("nested $binary does not parse: %v", err)
	}
	decoded, ok := nested[0].Value.(bson.Binary)
	if !ok || len(decoded.Data) != 2 || decoded.Data[0] != 0xde || decoded.Data[1] != 0xad {
		t.Fatalf("binary round-trip = %#v, want [0xde 0xad]", nested[0].Value)
	}
	// ObjectId and dates stay in their readable, copy-usable forms.
	if got := formatViewValue(id); got != `ObjectId("56d61033a378eccde8a8354f")` {
		t.Fatalf("formatViewValue(objectId) = %q, want ObjectId(...)", got)
	}
	if got := formatViewValue(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)); got != "2024-01-02T03:04:05Z" {
		t.Fatalf("formatViewValue(date) = %q, want RFC3339", got)
	}
}

func TestKeepRows_capsAtMaxRows(t *testing.T) {
	docs := make([]bson.D, maxRows+10)
	for i := range docs {
		docs[i] = bson.D{{Key: "n", Value: i}}
	}
	kept, truncated := keepRows(docs)
	if len(kept) != maxRows || !truncated {
		t.Fatalf("keepRows(%d) = %d rows, truncated=%t; want %d with truncation", len(docs), len(kept), truncated, maxRows)
	}
	small := docs[:5]
	kept, truncated = keepRows(small)
	if len(kept) != 5 || truncated {
		t.Fatalf("keepRows(small) = %d rows, truncated=%t; want all rows without truncation", len(kept), truncated)
	}
}

func TestDocumentsResult_capsDisplayAndPreservesFullValue(t *testing.T) {
	long := string(make([]byte, 0))
	for i := 0; i < 400; i++ {
		long += "x"
	}
	docs := []bson.D{{{Key: "text", Value: long}}}
	result := documentsResult(docs, false, time.Millisecond)
	if result.Rows[0][0] == nil || len(*result.Rows[0][0]) >= 400 {
		t.Fatalf("display cell not capped: %q", *result.Rows[0][0])
	}
	if result.UntruncatedRows[0][0] == nil || len(*result.UntruncatedRows[0][0]) != 400 {
		t.Fatalf("raw cell lost: %q", *result.UntruncatedRows[0][0])
	}
	if result.ColumnTypes[0] != "string" {
		t.Fatalf("column type = %q, want string", result.ColumnTypes[0])
	}
}

func TestExoticBSONTypes_displayAndView(t *testing.T) {
	pointer := bson.ObjectID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	cases := []struct {
		name     string
		value    any
		display  string
		typeName string
	}{
		{name: "regex", value: bson.Regex{Pattern: "^a.*", Options: "i"}, display: "/^a.*/i", typeName: "regex"},
		{name: "timestamp", value: bson.Timestamp{T: 1700000000, I: 42}, display: "Timestamp(1700000000, 42)", typeName: "timestamp"},
		{name: "javascript", value: bson.JavaScript("return 1"), display: "return 1", typeName: "javascript"},
		{name: "codeWithScope", value: bson.CodeWithScope{Code: "x", Scope: bson.D{{Key: "v", Value: int32(1)}}}, display: `x {v: 1}`, typeName: "codeWithScope"},
		{name: "dbPointer", value: bson.DBPointer{DB: "other", Pointer: pointer}, display: `DBPointer("other", ObjectId("0102030405060708090a0b0c"))`, typeName: "dbPointer"},
		{name: "undefined", value: bson.Undefined{}, display: "undefined", typeName: "undefined"},
		{name: "minKey", value: bson.MinKey{}, display: "MinKey()", typeName: "minKey"},
		{name: "maxKey", value: bson.MaxKey{}, display: "MaxKey()", typeName: "maxKey"},
		{name: "symbol", value: bson.Symbol("sym"), display: "sym", typeName: "symbol"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := formatValue(test.value); got != test.display {
				t.Errorf("formatValue = %q, want %q", got, test.display)
			}
			if got := bsonTypeName(test.value); got != test.typeName {
				t.Errorf("bsonTypeName = %q, want %q", got, test.typeName)
			}
			// View value must be valid extended JSON that round-trips
			// through the driver when nested (the driver's top-level
			// constructor quirk is bypassed by the wrapper).
			view := formatViewValue(test.value)
			var doc bson.D
			if err := bson.UnmarshalExtJSON([]byte(`{"v": `+view+`}`), false, &doc); err != nil {
				t.Fatalf("view value is not valid JSON: %v\n%s", err, view)
			}
			if got, want := doc[0].Value, test.value; !reflect.DeepEqual(got, want) {
				t.Errorf("view round-trip = %#v, want %#v", got, want)
			}
		})
	}
}

func TestDocumentsResult_binaryCell(t *testing.T) {
	docs := []bson.D{{{Key: "data", Value: bson.Binary{Subtype: 0x80, Data: []byte{0xde, 0xad}}}}}
	result := documentsResult(docs, false, time.Millisecond)
	if result.ColumnTypes[0] != "binary" {
		t.Fatalf("column type = %q, want binary", result.ColumnTypes[0])
	}
	if got := *result.Rows[0][0]; got != "0xDEAD" {
		t.Fatalf("display cell = %q, want hex", got)
	}
	if got := *result.UntruncatedRows[0][0]; got != `{"$binary":{"base64":"3q0=","subType":"80"}}` {
		t.Fatalf("raw cell = %q, want $binary JSON", got)
	}
}

func TestBrowseFilter(t *testing.T) {
	query := browseFilter([]driver.BrowseFilter{
		{Column: "borough", Operator: browseFilterEqual, Value: "Bronx"},
		{Column: "score", Operator: browseFilterGreaterEqual, Value: "7"},
		{Column: "name", Operator: browseFilterLike, Value: "Morris%"},
		{Column: "cuisine", Operator: browseFilterIsNull},
		{Column: "owner", Operator: browseFilterPattern, Value: "rez_*_"},
		{Column: "style", Operator: browseFilterNotPattern, Value: "x?"},
	})
	if query["borough"] != "Bronx" {
		t.Errorf("borough = %#v", query["borough"])
	}
	if query["score"].(bson.M)["$gte"] != int64(7) {
		t.Errorf("score = %#v, want numeric comparison", query["score"])
	}
	if query["name"].(bson.M)["$regex"] != "Morris.*" {
		t.Errorf("name = %#v", query["name"])
	}
	if query["cuisine"] != nil {
		t.Errorf("cuisine = %#v, want null", query["cuisine"])
	}
	if query["owner"].(bson.M)["$regex"] != "^rez_.*_$" {
		t.Errorf("owner = %#v, want anchored glob regex", query["owner"])
	}
	if query["style"].(bson.M)["$not"].(bson.M)["$regex"] != "^x.$" {
		t.Errorf("style = %#v, want negated glob regex", query["style"])
	}
}

func TestLikeRegex_escapesSpecialCharacters(t *testing.T) {
	for _, test := range []struct {
		pattern string
		want    string
	}{
		{pattern: "A%", want: "A.*"},
		{pattern: "A_B", want: "A.B"},
		{pattern: "a.b", want: `a\.b`},
	} {
		if got := likeRegex(test.pattern); got != test.want {
			t.Errorf("likeRegex(%q) = %q, want %q", test.pattern, got, test.want)
		}
	}
}

func TestDatabaseName(t *testing.T) {
	for _, test := range []struct {
		uri  string
		want string
	}{
		{uri: "mongodb://mongo:27017/restaurants", want: "restaurants"},
		{uri: "mongodb://mongo:27017/", want: "test"},
		{uri: "mongodb://mongo:27017", want: "test"},
		{uri: "not a uri", want: "test"},
	} {
		if got := databaseName(test.uri); got != test.want {
			t.Errorf("databaseName(%q) = %q, want %q", test.uri, got, test.want)
		}
	}
}
