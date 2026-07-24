package workbench

import "testing"

func TestDetailValue_prettyPrintsValidJSONObject(t *testing.T) {
	if got, want := detailValue(`{"customer":{"name":"Ada"},"ids":[1,2]}`), "{\n  \"customer\": {\n    \"name\": \"Ada\"\n  },\n  \"ids\": [\n    1,\n    2\n  ]\n}"; got != want {
		t.Fatalf("detail value = %q, want %q", got, want)
	}
}

func TestDetailValue_preservesInvalidJSON(t *testing.T) {
	if got, want := detailValue(`{"customer":}`), `{"customer":}`; got != want {
		t.Fatalf("detail value = %q, want %q", got, want)
	}
}
