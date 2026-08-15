package sql

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestRowWriteValues_roundTripEveryKind proves the tagged value tree is
// unambiguous across a serialization boundary (the future plugin wire):
// every kind survives, including recursive array/object values.
func TestRowWriteValues_roundTripEveryKind(t *testing.T) {
	values := []Value{
		{Kind: ValueDefault},
		{Kind: ValueNull},
		{Kind: ValueString, String: "text"},
		{Kind: ValueBool, Bool: true},
		{Kind: ValueInteger, Integer: 42},
		{Kind: ValueFloat, Float: 1.5},
		{Kind: ValueBytes, Bytes: []byte{1, 2, 3}},
		{Kind: ValueDecimal, Decimal: "3.14159"},
		{Kind: ValueTimestamp, Timestamp: "2026-01-02T03:04:05Z"},
		{Kind: ValueArray, Array: []Value{
			{Kind: ValueString, String: "a"},
			{Kind: ValueInteger, Integer: 1},
		}},
		{Kind: ValueObject, Object: []NamedValue{
			{Name: "enabled", Value: Value{Kind: ValueBool, Bool: false}},
			{Name: "nested", Value: Value{Kind: ValueArray, Array: []Value{{Kind: ValueNull}}}},
		}},
	}
	for _, value := range values {
		t.Run(string(value.Kind), func(t *testing.T) {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back Value
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal %s: %v", data, err)
			}
			if back.Kind != value.Kind {
				t.Fatalf("kind = %q, want %q", back.Kind, value.Kind)
			}
			if !reflect.DeepEqual(back, value) {
				t.Fatalf("round trip = %#v, want %#v", back, value)
			}
		})
	}
}

// TestRowWriteValues_emptyPayloadsStayDistinct guards the tagged
// representation: false, zero, empty string, empty array, and empty object
// must not collapse into each other after (un)marshaling.
func TestRowWriteValues_emptyPayloadsStayDistinct(t *testing.T) {
	values := []Value{
		{Kind: ValueDefault},
		{Kind: ValueNull},
		{Kind: ValueBool},      // false
		{Kind: ValueInteger},   // 0
		{Kind: ValueFloat},     // 0
		{Kind: ValueString},    // ""
		{Kind: ValueBytes},     // empty
		{Kind: ValueDecimal},   // ""
		{Kind: ValueTimestamp}, // ""
		{Kind: ValueArray},     // empty array
		{Kind: ValueObject},    // empty object
	}
	encoded := make([]string, len(values))
	for index, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s: %v", value.Kind, err)
		}
		encoded[index] = string(data)
	}
	for index, want := range encoded {
		for other, got := range encoded {
			if index == other {
				continue
			}
			if got == want {
				t.Fatalf("value kinds %s and %s both encode as %q", values[index].Kind, values[other].Kind, got)
			}
		}
	}
	// Each empty payload also round-trips with its kind intact.
	for index, value := range values {
		var back Value
		if err := json.Unmarshal([]byte(encoded[index]), &back); err != nil {
			t.Fatalf("unmarshal %s: %v", value.Kind, err)
		}
		if back.Kind != value.Kind {
			t.Fatalf("kind = %q, want %q", back.Kind, value.Kind)
		}
	}
}

// TestRowWriteWireDTOs_roundTrip proves the plugin request/response DTOs
// survive serialization with every field intact.
func TestRowWriteWireDTOs_roundTrip(t *testing.T) {
	payload := DocumentPayload{Format: DocumentFormatMongoExtendedJSON, Data: []byte(`{"name":"x"}`)}
	value := RowValue{Name: "id", Value: Value{Kind: ValueString, String: "7"}}
	capabilities := WriteCapabilities{RowWriter: true, Document: &DocumentWriteCapability{Format: DocumentFormatMongoExtendedJSON, Text: true}}
	tests := []struct {
		name string
		any  any
	}{
		{name: "row write request insert", any: RowWriteRequest{Operation: RowWriteInsert, Table: "items", Values: []RowValue{value}}},
		{name: "row write request update", any: RowWriteRequest{Operation: RowWriteUpdate, Table: "items", Key: []RowValue{value}, Values: []RowValue{{Name: "name", Value: Value{Kind: ValueNull}}}}},
		{name: "row write request delete", any: RowWriteRequest{Operation: RowWriteDelete, Table: "items", Key: []RowValue{value}}},
		{name: "row write response", any: RowWriteResponse{Result: WriteResult{RowsAffected: 1}}},
		{name: "document write request read", any: DocumentWriteRequest{Operation: DocumentWriteRead, Collection: "things", ID: &payload}},
		{name: "document write request insert", any: DocumentWriteRequest{Operation: DocumentWriteInsert, Collection: "things", Document: &payload}},
		{name: "document write request replace", any: DocumentWriteRequest{Operation: DocumentWriteReplace, Collection: "things", ID: &payload, Document: &payload}},
		{name: "document write request delete", any: DocumentWriteRequest{Operation: DocumentWriteDelete, Collection: "things", ID: &payload}},
		{name: "document write response", any: DocumentWriteResponse{Result: WriteResult{RowsAffected: 1}, Document: &payload}},
		{name: "document payload", any: payload},
		{name: "write capabilities", any: capabilities},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(test.any)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			back := reflect.New(reflect.TypeOf(test.any)).Interface()
			if err := json.Unmarshal(data, back); err != nil {
				t.Fatalf("unmarshal %s: %v", data, err)
			}
			if !reflect.DeepEqual(reflect.ValueOf(back).Elem().Interface(), test.any) {
				t.Fatalf("round trip = %#v, want %#v", reflect.ValueOf(back).Elem().Interface(), test.any)
			}
		})
	}
	// A nil Document capability must serialize without a document key so
	// row-only services stay unambiguous.
	data, err := json.Marshal(WriteCapabilities{RowWriter: true})
	if err != nil {
		t.Fatalf("marshal row-only capabilities: %v", err)
	}
	if string(data) != `{"row_writer":true}` {
		t.Fatalf("row-only capabilities = %s, want {\"row_writer\":true}", data)
	}
}

// TestWriteStatement_wireCompatibility proves the optional statement
// field is backward compatible on the wire: Result and WriteResult
// serialize the statement key exactly when non-empty — including nested
// under the write response `result` — and omit it entirely when empty,
// so compiled-in drivers and older plugins keep the identical prior JSON.
func TestWriteStatement_wireCompatibility(t *testing.T) {
	const native = "RENAME key user:2 user:3"

	// Exact prior wire shapes: an empty statement changes nothing.
	for _, test := range []struct {
		name string
		any  any
		want string
	}{
		{name: "result", any: Result{RowsAffected: 1}, want: `{"columns":null,"column_types":null,"rows":null,"untruncated_rows":null,"rows_affected":1,"has_more":false,"duration_ns":0,"truncated":false,"document_ids":null}`},
		{name: "write result", any: WriteResult{RowsAffected: 1}, want: `{"rows_affected":1}`},
		{name: "row write response", any: RowWriteResponse{Result: WriteResult{RowsAffected: 1}}, want: `{"result":{"rows_affected":1}}`},
		{name: "document write response", any: DocumentWriteResponse{Result: WriteResult{RowsAffected: 1}}, want: `{"result":{"rows_affected":1}}`},
	} {
		t.Run(test.name+" empty", func(t *testing.T) {
			data, err := json.Marshal(test.any)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != test.want {
				t.Fatalf("wire = %s, want the exact prior shape %s", data, test.want)
			}
		})
	}

	// Non-empty statements are serialized and survive a round trip.
	for _, test := range []struct {
		name string
		any  any
		want string
	}{
		{name: "result", any: Result{RowsAffected: 1, Statement: native}, want: `{"columns":null,"column_types":null,"rows":null,"untruncated_rows":null,"rows_affected":1,"has_more":false,"duration_ns":0,"truncated":false,"document_ids":null,"statement":"RENAME key user:2 user:3"}`},
		{name: "write result", any: WriteResult{RowsAffected: 1, Statement: native}, want: `{"rows_affected":1,"statement":"RENAME key user:2 user:3"}`},
		{name: "row write response", any: RowWriteResponse{Result: WriteResult{RowsAffected: 1, Statement: native}}, want: `{"result":{"rows_affected":1,"statement":"RENAME key user:2 user:3"}}`},
		{name: "document write response", any: DocumentWriteResponse{Result: WriteResult{RowsAffected: 1, Statement: native}}, want: `{"result":{"rows_affected":1,"statement":"RENAME key user:2 user:3"}}`},
	} {
		t.Run(test.name+" present", func(t *testing.T) {
			data, err := json.Marshal(test.any)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != test.want {
				t.Fatalf("wire = %s, want %s", data, test.want)
			}
			back := reflect.New(reflect.TypeOf(test.any)).Interface()
			if err := json.Unmarshal(data, back); err != nil {
				t.Fatalf("unmarshal %s: %v", data, err)
			}
			if !reflect.DeepEqual(reflect.ValueOf(back).Elem().Interface(), test.any) {
				t.Fatalf("round trip = %#v, want %#v", reflect.ValueOf(back).Elem().Interface(), test.any)
			}
		})
	}

	// A response from an older plugin (no statement key) decodes with an
	// empty statement and keeps the prior rows_affected.
	var legacy RowWriteResponse
	if err := json.Unmarshal([]byte(`{"result":{"rows_affected":2}}`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy response: %v", err)
	}
	if legacy.Result.RowsAffected != 2 || legacy.Result.Statement != "" {
		t.Fatalf("legacy decode = %+v, want rows_affected 2 and no statement", legacy.Result)
	}
}

// TestDocumentFormatMongoExtendedJSON_identity guards the wire-visible
// format constant: it is a stable media type, not a display string.
func TestDocumentFormatMongoExtendedJSON_identity(t *testing.T) {
	const want = "application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed"
	if got := string(DocumentFormatMongoExtendedJSON); got != want {
		t.Fatalf("format = %q, want %q", got, want)
	}
}
