package sql

import (
	"encoding/json"
	"testing"
)

// TestStatementMetadata_omittedStaysOmitted guards the wire shape: a
// result without metadata must not carry a statement_metadata key, so
// older plugins and compiled-in drivers keep the prior JSON exactly.
func TestStatementMetadata_omittedStaysOmitted(t *testing.T) {
	for _, result := range []any{
		Result{Statement: "RENAME key user:2 user:3"},
		Result{},
		WriteResult{Statement: "SET key 1"},
	} {
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal %#v: %v", result, err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
		if _, present := object["statement_metadata"]; present {
			t.Fatalf("%s carries statement_metadata, want it omitted", data)
		}
	}
}

// TestStatementMetadata_presentObjectRoundTrips proves a present metadata
// object survives the wire as a full object with explicit false
// preserved: omission of the object and an explicit all-false object must
// not collapse into each other.
func TestStatementMetadata_presentObjectRoundTrips(t *testing.T) {
	metadata := StatementMetadata{Language: "redis", Replayable: false, Sensitive: false}
	for _, result := range []any{
		Result{Statement: "SET key 1", StatementMetadata: &metadata},
		WriteResult{Statement: "SET key 1", StatementMetadata: &metadata},
	} {
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
		raw, present := object["statement_metadata"]
		if !present {
			t.Fatalf("%s omits statement_metadata, want it present", data)
		}
		var back StatementMetadata
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("decode metadata %s: %v", raw, err)
		}
		if back != metadata {
			t.Fatalf("metadata round trip = %#v, want %#v (explicit false preserved)", back, metadata)
		}
	}
	// A JSON null metadata object unmarshals to a nil pointer, the same
	// as an omitted object.
	var result Result
	if err := json.Unmarshal([]byte(`{"statement":"x","statement_metadata":null}`), &result); err != nil {
		t.Fatalf("unmarshal null metadata: %v", err)
	}
	if result.StatementMetadata != nil {
		t.Fatalf("null metadata = %#v, want nil", result.StatementMetadata)
	}
	if err := json.Unmarshal([]byte(`{"statement":"x","statement_metadata":{"language":"redis","replayable":false,"sensitive":true}}`), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if want := (StatementMetadata{Language: "redis", Replayable: false, Sensitive: true}); *result.StatementMetadata != want {
		t.Fatalf("unmarshaled metadata = %#v, want %#v", *result.StatementMetadata, want)
	}
}
