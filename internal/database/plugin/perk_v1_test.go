package plugin

// TestPerkV1Contract pins the canonical machine-readable perk/v1
// contract (protocol/perk-v1/schema.json, fixtures/, manifest.json) to
// the authoritative Go implementation: every protocol method constant
// must have request/result schema coverage, every valid fixture must
// decode through the real envelopes and DTOs, and every parseable
// invalid fixture must be rejected by the host's own boundary
// validation. The checks are deliberately narrow and structural — no
// external JSON Schema validator, no assertions on schema text.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// perkV1Dir locates protocol/perk-v1 relative to this test file.
func perkV1Dir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "protocol", "perk-v1")
}

func readJSONFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func parseObject(t *testing.T, name string, data []byte) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("%s is not parseable JSON: %v", name, err)
	}
	return object
}

// schemaDefs returns the schema's $defs map, failing the test when it is
// absent or not an object.
func schemaDefs(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("schema $defs missing or not an object")
	}
	return defs
}

// checkRefs walks every $ref in the schema and asserts it resolves
// against $defs — the schema's internal coherence check.
func checkRefs(t *testing.T, defs map[string]any) {
	t.Helper()
	var walk func(node any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if ref, ok := value["$ref"].(string); ok {
				if !strings.HasPrefix(ref, "#/$defs/") {
					t.Fatalf("unsupported $ref %q; only #/$defs/... references are allowed", ref)
				}
				if _, ok := defs[strings.TrimPrefix(ref, "#/$defs/")]; !ok {
					t.Fatalf("$ref %q does not resolve in $defs", ref)
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(defs)
}

func defObject(t *testing.T, defs map[string]any, name string) map[string]any {
	t.Helper()
	def, ok := defs[name].(map[string]any)
	if !ok {
		t.Fatalf("$defs[%s] missing or not an object", name)
	}
	return def
}

func stringList(value any) []string {
	switch list := value.(type) {
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return list
	}
	return nil
}

func requiredOf(t *testing.T, def map[string]any) []string {
	t.Helper()
	required, ok := def["required"].([]any)
	if !ok {
		t.Fatalf("def has no required list")
	}
	names := make([]string, 0, len(required))
	for _, item := range required {
		name, ok := item.(string)
		if !ok {
			t.Fatalf("required entry is not a string")
		}
		names = append(names, name)
	}
	return names
}

func propsOf(t *testing.T, def map[string]any) map[string]any {
	t.Helper()
	props, ok := def["properties"].(map[string]any)
	if !ok {
		t.Fatalf("def has no properties object")
	}
	return props
}

func constOf(t *testing.T, def map[string]any, key string) any {
	t.Helper()
	constraint, ok := def[key].(map[string]any)
	if !ok {
		return nil
	}
	return constraint["const"]
}

// perkV1Methods is the Go-side mirror of the schema's $defs/methods
// registry: every perk/v1 method constant, its params $def, and its
// result $def ("" when the method answers null; cancel is a
// notification). Keep this table in sync with protocol.go's method
// constants — TestPerkV1Schema_methodCoverage asserts the schema agrees
// with it exactly.
var perkV1Methods = map[string]struct {
	params string
	result string // "" = void result
}{
	methodInitialize:                 {params: "initializeParams", result: "initializeResult"},
	methodBuildTarget:                {params: "formValues", result: "buildTargetResult"},
	methodOpen:                       {params: "openParams", result: "openResult"},
	methodClose:                      {params: "sessionParams"},
	methodCancel:                     {params: "cancelParams"}, // notification
	methodExecute:                    {params: "statementParams", result: "result"},
	methodExecuteReadOnly:            {params: "statementParams", result: "result"},
	methodValidate:                   {params: "statementParams"},
	methodListSchema:                 {params: "sessionParams", result: "schemaObjectList"},
	methodTableInfo:                  {params: "tableParams", result: "columnInfoList"},
	methodListIndexes:                {params: "tableParams", result: "indexInfoList"},
	methodCreateIndex:                {params: "indexChangeParams"},
	methodReplaceIndex:               {params: "replaceIndexParams"},
	methodDropIndex:                  {params: "dropParams"},
	methodListForeignKeys:            {params: "tableParams", result: "foreignKeyInfoList"},
	methodListReferencingForeignKeys: {params: "tableParams", result: "referencingForeignKeyInfoList"},
	methodListForeignKeysAll:         {params: "sessionParams", result: "foreignKeyInfoMap"},
	methodListIndexesAll:             {params: "sessionParams", result: "indexInfoMap"},
	methodCreateForeignKey:           {params: "foreignKeyChangeParams"},
	methodReplaceForeignKey:          {params: "replaceForeignKeyParams"},
	methodDropForeignKey:             {params: "dropParams"},
	methodAlterColumn:                {params: "columnChangeParams"},
	methodDropColumn:                 {params: "dropParams"},
	methodAddColumn:                  {params: "addColumnParams"},
	methodBrowseTable:                {params: "browseParams", result: "result"},
	methodRowWrite:                   {params: "rowWriteParams", result: "rowWriteResponse"},
	methodDocumentWrite:              {params: "documentWriteParams", result: "documentWriteResponse"},
}

func TestPerkV1Schema_structure(t *testing.T) {
	schema := parseObject(t, "schema.json", readJSONFile(t, filepath.Join(perkV1Dir(t), "schema.json")))
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("$schema = %v, want draft 2020-12", schema["$schema"])
	}
	if id, ok := schema["$id"].(string); !ok || id == "" {
		t.Fatalf("$id missing or blank")
	}
	if title, ok := schema["title"].(string); !ok || title == "" {
		t.Fatalf("title missing or blank")
	}
	if version, ok := schema["version"].(float64); !ok || int(version) != ProtocolVersion {
		t.Fatalf("schema version = %v, want the host ProtocolVersion %d", schema["version"], ProtocolVersion)
	}
	defs := schemaDefs(t, schema)
	checkRefs(t, defs)

	// Envelope invariants: jsonrpc const, numeric integer ids, exact
	// method, exactly one of result/error on responses.
	methodEnum := stringList(defObject(t, defs, "request")["properties"].(map[string]any)["method"].(map[string]any)["enum"])
	request := defObject(t, defs, "request")
	if !reflect.DeepEqual(requiredOf(t, request), []string{"jsonrpc", "id", "method", "params"}) {
		t.Fatalf("request required = %v", requiredOf(t, request))
	}
	if got := constOf(t, propsOf(t, request), "jsonrpc"); got != "2.0" {
		t.Fatalf("request jsonrpc const = %v, want \"2.0\"", got)
	}
	if ref, ok := propsOf(t, request)["id"].(map[string]any)["$ref"].(string); !ok || ref != "#/$defs/uint64" {
		t.Fatalf("request id = %v, want the uint64 def", propsOf(t, request)["id"])
	}
	notification := defObject(t, defs, "notification")
	if got := constOf(t, propsOf(t, notification), "method"); got != methodCancel {
		t.Fatalf("notification method const = %v, want %s", got, methodCancel)
	}
	success := defObject(t, defs, "success")
	if !reflect.DeepEqual(requiredOf(t, success), []string{"jsonrpc", "id", "result"}) {
		t.Fatalf("success required = %v", requiredOf(t, success))
	}
	if errField, ok := propsOf(t, success)["error"].(bool); !ok || errField {
		t.Fatalf("success must forbid the error member")
	}
	errDef := defObject(t, defs, "error")
	if !reflect.DeepEqual(requiredOf(t, errDef), []string{"jsonrpc", "id", "error"}) {
		t.Fatalf("error required = %v", requiredOf(t, errDef))
	}
	if resultField, ok := propsOf(t, errDef)["result"].(bool); !ok || resultField {
		t.Fatalf("error must forbid the result member")
	}
	// The request envelope's exact-method enum must cover every method
	// constant and nothing else.
	wantMethods := make([]string, 0, len(perkV1Methods))
	for method := range perkV1Methods {
		wantMethods = append(wantMethods, method)
	}
	if !reflect.DeepEqual(sorted(methodEnum), sorted(wantMethods)) {
		t.Fatalf("request method enum = %v, want the protocol method constants", methodEnum)
	}
}

func sorted(list []string) []string {
	out := append([]string(nil), list...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TestPerkV1Schema_queryCommandDef pins the canonical shape of a
// query_language command entry: name/usage/summary are required, bounded,
// and control-free in the schema itself, and queryLanguage references the
// def from an optional capped array.
func TestPerkV1Schema_queryCommandDef(t *testing.T) {
	schema := parseObject(t, "schema.json", readJSONFile(t, filepath.Join(perkV1Dir(t), "schema.json")))
	defs := schemaDefs(t, schema)
	ql := defObject(t, defs, "queryLanguage")
	commands, ok := propsOf(t, ql)["commands"].(map[string]any)
	if !ok {
		t.Fatal("queryLanguage has no commands property")
	}
	if commands["type"] != "array" {
		t.Fatalf("commands type = %v, want array", commands["type"])
	}
	if maxItems, ok := commands["maxItems"].(float64); !ok || int(maxItems) != sharedsql.MaxQueryCommands {
		t.Fatalf("commands maxItems = %v, want %d", commands["maxItems"], sharedsql.MaxQueryCommands)
	}
	itemRef, ok := commands["items"].(map[string]any)["$ref"].(string)
	if !ok || itemRef != "#/$defs/queryCommand" {
		t.Fatalf("commands items = %v, want the queryCommand def", commands["items"])
	}
	command := defObject(t, defs, "queryCommand")
	if !reflect.DeepEqual(requiredOf(t, command), []string{"name", "usage", "summary"}) {
		t.Fatalf("queryCommand required = %v", requiredOf(t, command))
	}
	for field, wantMax := range map[string]int{
		"name": sharedsql.MaxQueryCommandNameRunes, "usage": sharedsql.MaxQueryCommandUsageRunes,
		"summary": sharedsql.MaxQueryCommandSummaryRunes,
	} {
		prop, ok := propsOf(t, command)[field].(map[string]any)
		if !ok {
			t.Fatalf("queryCommand has no %s property", field)
		}
		if maxLength, ok := prop["maxLength"].(float64); !ok || int(maxLength) != wantMax {
			t.Fatalf("%s maxLength = %v, want %d", field, prop["maxLength"], wantMax)
		}
		pattern, ok := prop["pattern"].(string)
		if !ok || pattern == "" {
			t.Fatalf("%s has no pattern", field)
		}
		if field == "name" {
			// Names are ASCII letters/digits/underscores — the editor
			// token charset — which also makes lowercase uniqueness
			// total across the whole allowed input space.
			if pattern != "^[A-Za-z0-9_]+$" {
				t.Fatalf("name pattern = %q, want the ASCII token charset", pattern)
			}
			for _, value := range []string{"GET", "HGETALL", "SCAN2", "FOO_BAR"} {
				if !asciiTokenPatternMatches(pattern, value) {
					t.Fatalf("name pattern %q must accept %q", pattern, value)
				}
			}
			for _, r := range []rune{'\n', 'é', 'σ', ' ', '-', 'İ'} {
				if asciiTokenPatternMatches(pattern, string(r)) {
					t.Fatalf("name pattern %q must reject %q", pattern, r)
				}
			}
			continue
		}
		for _, r := range []rune{'\n', '\t', '\x00', '\x7f', '\u009f'} {
			if controlFreePatternMatches(pattern, string(r)) {
				t.Fatalf("%s pattern %q must reject control rune %q", field, pattern, r)
			}
		}
		if !controlFreePatternMatches(pattern, "GET key") {
			t.Fatalf("%s pattern %q must accept plain text", field, pattern)
		}
	}
}

// controlFreePatternMatches reports whether value matches the schema's
// control-free pattern: no ASCII C0/C1 control or DEL rune. It
// deliberately mirrors the JSON Schema regex semantics over runes
// instead of invoking an external validator.
func controlFreePatternMatches(pattern, value string) bool {
	if pattern != "^[^\\u0000-\\u001F\\u007F-\\u009F]*$" {
		return false
	}
	for _, r := range value {
		if (r >= 0x00 && r <= 0x1F) || (r >= 0x7F && r <= 0x9F) {
			return false
		}
	}
	return true
}

// asciiTokenPatternMatches reports whether value matches the schema's
// ASCII command-name pattern: one or more ASCII letters, digits, or
// underscores, mirroring the JSON Schema regex over runes.
func asciiTokenPatternMatches(pattern, value string) bool {
	if pattern != "^[A-Za-z0-9_]+$" {
		return false
	}
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func TestPerkV1Schema_methodCoverage(t *testing.T) {
	schema := parseObject(t, "schema.json", readJSONFile(t, filepath.Join(perkV1Dir(t), "schema.json")))
	defs := schemaDefs(t, schema)
	methods := defObject(t, defs, "methods")
	registry := propsOf(t, methods)

	enum := stringList(methods["propertyNames"].(map[string]any)["enum"])
	registryNames := make([]string, 0, len(registry))
	for name := range registry {
		registryNames = append(registryNames, name)
	}
	if !reflect.DeepEqual(sorted(enum), sorted(registryNames)) {
		t.Fatalf("methods propertyNames = %v, properties = %v", enum, registryNames)
	}
	if !reflect.DeepEqual(sorted(enum), sorted(methodNames())) {
		t.Fatalf("methods propertyNames = %v, want the protocol method constants", enum)
	}
	for method, spec := range perkV1Methods {
		entry, ok := registry[method].(map[string]any)
		if !ok {
			t.Fatalf("schema has no methods entry for %s", method)
		}
		entryProps := propsOf(t, entry)
		if method == methodCancel {
			if !reflect.DeepEqual(requiredOf(t, entry), []string{"params", "notification"}) {
				t.Fatalf("cancel entry required = %v", requiredOf(t, entry))
			}
			if got := constOf(t, entryProps, "notification"); got != true {
				t.Fatalf("cancel notification const = %v, want true", got)
			}
			if ref, ok := entryProps["params"].(map[string]any)["$ref"].(string); !ok || ref != "#/$defs/"+spec.params {
				t.Fatalf("cancel params = %v, want #/$defs/%s", entryProps["params"], spec.params)
			}
			continue
		}
		if !reflect.DeepEqual(requiredOf(t, entry), []string{"params", "result"}) {
			t.Fatalf("%s entry required = %v", method, requiredOf(t, entry))
		}
		if ref, ok := entryProps["params"].(map[string]any)["$ref"].(string); !ok || ref != "#/$defs/"+spec.params {
			t.Fatalf("%s params = %v, want #/$defs/%s", method, entryProps["params"], spec.params)
		}
		result := entryProps["result"]
		if spec.result == "" {
			nullSchema, ok := result.(map[string]any)
			if !ok || nullSchema["type"] != "null" {
				t.Fatalf("%s result = %v, want the null type", method, result)
			}
			continue
		}
		if ref, ok := result.(map[string]any)["$ref"].(string); !ok || ref != "#/$defs/"+spec.result {
			t.Fatalf("%s result = %v, want #/$defs/%s", method, result, spec.result)
		}
	}
}

func methodNames() []string {
	names := make([]string, 0, len(perkV1Methods))
	for method := range perkV1Methods {
		names = append(names, method)
	}
	return names
}

// perkV1Fixture is one manifest entry.
type perkV1Fixture struct {
	File   string `json:"file"`
	Valid  bool   `json:"valid"`
	Ref    string `json:"ref"`
	Method string `json:"method,omitempty"`
	Code   int    `json:"code,omitempty"`
	Kind   string `json:"kind,omitempty"`
	// Hint and SuggestedStatement are the advisory guidance an error
	// fixture must carry through rpcErrorToGoError, when the manifest
	// declares any.
	Hint               string `json:"hint,omitempty"`
	SuggestedStatement string `json:"suggested_statement,omitempty"`
	Reject             string `json:"reject,omitempty"`
}

type perkV1Manifest struct {
	Fixtures []perkV1Fixture `json:"fixtures"`
}

// TestPerkV1Manifest: the manifest parses, every entry names an existing
// fixture file that parses as JSON, refs resolve in the schema, methods
// are protocol methods, error frames carry the expected code, and
// invalid frames that the SDK answers carry the expected JSON-RPC code.
func TestPerkV1Manifest(t *testing.T) {
	dir := filepath.Join(perkV1Dir(t), "fixtures")
	schema := parseObject(t, "schema.json", readJSONFile(t, filepath.Join(perkV1Dir(t), "schema.json")))
	defs := schemaDefs(t, schema)
	var manifest perkV1Manifest
	if err := json.Unmarshal(readJSONFile(t, filepath.Join(dir, "manifest.json")), &manifest); err != nil {
		t.Fatalf("manifest does not parse: %v", err)
	}
	if len(manifest.Fixtures) == 0 {
		t.Fatal("manifest lists no fixtures")
	}
	envelopeRefs := map[string]bool{
		"#/$defs/request":      true,
		"#/$defs/notification": true,
		"#/$defs/success":      true,
		"#/$defs/error":        true,
	}
	for _, fixture := range manifest.Fixtures {
		if fixture.File == "" {
			t.Fatal("fixture entry with a blank file name")
		}
		frame := readJSONFile(t, filepath.Join(dir, fixture.File))
		if !json.Valid(frame) {
			t.Fatalf("%s is not parseable JSON", fixture.File)
		}
		var anyValue any
		if err := json.Unmarshal(frame, &anyValue); err != nil {
			t.Fatalf("%s does not parse: %v", fixture.File, err)
		}
		if !envelopeRefs[fixture.Ref] {
			t.Fatalf("%s ref %q is not an envelope $def", fixture.File, fixture.Ref)
		}
		if _, ok := defs[strings.TrimPrefix(fixture.Ref, "#/$defs/")]; !ok {
			t.Fatalf("%s ref %q does not resolve", fixture.File, fixture.Ref)
		}
		if fixture.Valid && fixture.Method != "" {
			// An invalid fixture may deliberately carry a non-protocol
			// method (unknown-method frames); a valid one must not.
			if _, ok := perkV1Methods[fixture.Method]; !ok {
				t.Fatalf("%s method %q is not a protocol method", fixture.File, fixture.Method)
			}
		}
		if fixture.Ref == "#/$defs/error" && fixture.Valid && fixture.Code == 0 {
			t.Fatalf("%s is a valid error frame without an expected code", fixture.File)
		}
	}
}

// TestPerkV1Fixtures drives every fixture through the authoritative Go
// envelopes and DTOs plus the host's own boundary validation: valid
// frames must be accepted, parseable invalid frames must be rejected
// with the rejection the manifest documents.
func TestPerkV1Fixtures(t *testing.T) {
	dir := filepath.Join(perkV1Dir(t), "fixtures")
	var manifest perkV1Manifest
	if err := json.Unmarshal(readJSONFile(t, filepath.Join(dir, "manifest.json")), &manifest); err != nil {
		t.Fatalf("manifest does not parse: %v", err)
	}
	for _, fixture := range manifest.Fixtures {
		fixture := fixture
		t.Run(fixture.File, func(t *testing.T) {
			frame := readJSONFile(t, filepath.Join(dir, fixture.File))
			err := verifyPerkV1Fixture(frame, fixture)
			if fixture.Valid {
				if err != nil {
					t.Fatalf("valid fixture rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("invalid fixture was accepted")
			}
			if fixture.Reject != "" && !strings.Contains(err.Error(), fixture.Reject) {
				t.Fatalf("rejection %q does not match the documented rejection %q", err.Error(), fixture.Reject)
			}
		})
	}
}

// verifyPerkV1Fixture runs one canonical frame through the authoritative
// Go envelope, the method's params/result DTOs, and the host's boundary
// validation, returning the first failure (nil when fully valid).
func verifyPerkV1Fixture(frame []byte, fixture perkV1Fixture) error {
	if !json.Valid(frame) {
		return fmt.Errorf("fixture is not parseable JSON")
	}
	switch fixture.Ref {
	case "#/$defs/request":
		return verifyPerkV1Request(frame, fixture)
	case "#/$defs/notification":
		return verifyPerkV1Notification(frame, fixture)
	case "#/$defs/success":
		return verifyPerkV1Success(frame, fixture)
	case "#/$defs/error":
		return verifyPerkV1Error(frame, fixture)
	}
	return fmt.Errorf("unexpected ref %q", fixture.Ref)
}

// verifyPerkV1Request decodes a request envelope and its method's params
// DTO, checks the envelope invariants (jsonrpc, integer id, exact
// method), and runs the host's params-side boundary validation.
func verifyPerkV1Request(frame []byte, fixture perkV1Fixture) error {
	var envelope request
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if envelope.JSONRPC != "2.0" {
		return fmt.Errorf("jsonrpc must be \"2.0\"")
	}
	spec, ok := perkV1Methods[envelope.Method]
	if !ok {
		return fmt.Errorf("unknown method %q", envelope.Method)
	}
	if fixture.Method != "" && envelope.Method != fixture.Method {
		return fmt.Errorf("method %q, manifest expects %q", envelope.Method, fixture.Method)
	}
	if spec.params == "" {
		return fmt.Errorf("method %q is not a request method", envelope.Method)
	}
	if err := decodeRequestParams(envelope.Method, envelope.Params); err != nil {
		return fmt.Errorf("params %s: %w", envelope.Method, err)
	}
	if err := validateRequestParams(envelope.Method, envelope.Params); err != nil {
		return fmt.Errorf("host %s: %w", envelope.Method, err)
	}
	return nil
}

func verifyPerkV1Notification(frame []byte, fixture perkV1Fixture) error {
	var envelope notification
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if envelope.JSONRPC != "2.0" {
		return fmt.Errorf("jsonrpc must be \"2.0\"")
	}
	if envelope.Method != methodCancel {
		return fmt.Errorf("notification method %q, want %s", envelope.Method, methodCancel)
	}
	var params cancelParams
	if err := json.Unmarshal(envelope.Params, &params); err != nil {
		return fmt.Errorf("params %s: %w", methodCancel, err)
	}
	return nil
}

// verifyPerkV1Success decodes a success response, its method's result
// DTO (null for void methods), and runs the host's result-side boundary
// validation (statement_metadata rules, initialize protocol version).
func verifyPerkV1Success(frame []byte, fixture perkV1Fixture) error {
	var envelope response
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if envelope.JSONRPC != "2.0" {
		return fmt.Errorf("jsonrpc must be \"2.0\"")
	}
	if envelope.Error != nil {
		return fmt.Errorf("success fixture carries an error")
	}
	spec, ok := perkV1Methods[fixture.Method]
	if !ok {
		return fmt.Errorf("unknown method %q", fixture.Method)
	}
	if spec.result == "" {
		if err := json.Unmarshal(envelope.Result, &struct{}{}); err != nil {
			return fmt.Errorf("result %s: %w", fixture.Method, err)
		}
	} else if err := decodeResult(fixture.Method, envelope.Result); err != nil {
		return fmt.Errorf("result %s: %w", fixture.Method, err)
	}
	if err := validateResult(fixture.Method, envelope.Result); err != nil {
		return fmt.Errorf("host %s: %w", fixture.Method, err)
	}
	return nil
}

// verifyPerkV1Error decodes an error response and maps it through the
// host's rpcErrorToGoError: the manifest's expected code must match, a
// canceled code must map exactly to context.Canceled, and other codes
// must surface as *Error with the manifest's normalized kind.
func verifyPerkV1Error(frame []byte, fixture perkV1Fixture) error {
	var envelope response
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if envelope.JSONRPC != "2.0" {
		return fmt.Errorf("jsonrpc must be \"2.0\"")
	}
	if envelope.Error == nil {
		return fmt.Errorf("error fixture carries no error")
	}
	if fixture.Code == 0 {
		return nil
	}
	if envelope.Error.Code != fixture.Code {
		return fmt.Errorf("error code %d, want %d", envelope.Error.Code, fixture.Code)
	}
	mapped := rpcErrorToGoError(methodExecute, "fixture", envelope.Error)
	if fixture.Code == RPCErrorCanceled {
		if mapped != context.Canceled {
			return fmt.Errorf("code -32800 must map to context.Canceled, got %v", mapped)
		}
		return nil
	}
	var pluginErr *Error
	if !errors.As(mapped, &pluginErr) || pluginErr.Code != fixture.Code {
		return fmt.Errorf("code %d must map to *Error, got %v", fixture.Code, mapped)
	}
	if fixture.Kind != "" && pluginErr.Kind != Kind(fixture.Kind) {
		return fmt.Errorf("kind %q, want %q", pluginErr.Kind, fixture.Kind)
	}
	if pluginErr.Hint != fixture.Hint {
		return fmt.Errorf("hint %q, want %q", pluginErr.Hint, fixture.Hint)
	}
	if pluginErr.SuggestedStatement != fixture.SuggestedStatement {
		return fmt.Errorf("suggested_statement %q, want %q", pluginErr.SuggestedStatement, fixture.SuggestedStatement)
	}
	return nil
}

// decodeRequestParams decodes one method's params into its authoritative
// wire DTO.
func decodeRequestParams(method string, raw json.RawMessage) error {
	switch method {
	case methodInitialize:
		return json.Unmarshal(raw, &initializeParams{})
	case methodBuildTarget:
		return json.Unmarshal(raw, &database.FormValues{})
	case methodOpen:
		return json.Unmarshal(raw, &openParams{})
	case methodClose, methodListSchema, methodListForeignKeysAll, methodListIndexesAll:
		return json.Unmarshal(raw, &sessionParams{})
	case methodExecute, methodExecuteReadOnly, methodValidate:
		return json.Unmarshal(raw, &statementParams{})
	case methodTableInfo, methodListIndexes, methodListForeignKeys, methodListReferencingForeignKeys:
		return json.Unmarshal(raw, &tableParams{})
	case methodCreateIndex:
		return json.Unmarshal(raw, &indexChangeParams{})
	case methodReplaceIndex:
		return json.Unmarshal(raw, &replaceIndexParams{})
	case methodDropIndex, methodDropForeignKey, methodDropColumn:
		return json.Unmarshal(raw, &dropParams{})
	case methodCreateForeignKey:
		return json.Unmarshal(raw, &foreignKeyChangeParams{})
	case methodReplaceForeignKey:
		return json.Unmarshal(raw, &replaceForeignKeyParams{})
	case methodAlterColumn:
		return json.Unmarshal(raw, &columnChangeParams{})
	case methodAddColumn:
		return json.Unmarshal(raw, &addColumnParams{})
	case methodBrowseTable:
		return json.Unmarshal(raw, &browseParams{})
	case methodRowWrite:
		return json.Unmarshal(raw, &rowWriteParams{})
	case methodDocumentWrite:
		return json.Unmarshal(raw, &documentWriteParams{})
	}
	return fmt.Errorf("no params DTO for %s", method)
}

// validateRequestParams applies the host's params-side boundary
// validation where it exists: index and foreign-key changes are checked
// with the shared validators.
func validateRequestParams(method string, raw json.RawMessage) error {
	switch method {
	case methodCreateIndex:
		var params indexChangeParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return err
		}
		return sharedsql.ValidateIndexChange(params.Change)
	case methodReplaceIndex:
		var params replaceIndexParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return err
		}
		return sharedsql.ValidateIndexChange(params.Change)
	case methodCreateForeignKey:
		var params foreignKeyChangeParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return err
		}
		return sharedsql.ValidateForeignKeyChange(params.Change)
	case methodReplaceForeignKey:
		var params replaceForeignKeyParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return err
		}
		return sharedsql.ValidateForeignKeyChange(params.Change)
	}
	return nil
}

// decodeResult decodes one method's success result into its
// authoritative wire DTO.
func decodeResult(method string, raw json.RawMessage) error {
	switch method {
	case methodInitialize:
		return json.Unmarshal(raw, &initializeResult{})
	case methodBuildTarget:
		return json.Unmarshal(raw, &buildTargetResult{})
	case methodOpen:
		return json.Unmarshal(raw, &openResult{})
	case methodExecute, methodExecuteReadOnly, methodBrowseTable:
		return json.Unmarshal(raw, &sharedsql.Result{})
	case methodListSchema:
		return json.Unmarshal(raw, &[]sharedsql.SchemaObject{})
	case methodTableInfo:
		return json.Unmarshal(raw, &[]sharedsql.ColumnInfo{})
	case methodListIndexes:
		return json.Unmarshal(raw, &[]sharedsql.IndexInfo{})
	case methodListForeignKeys:
		return json.Unmarshal(raw, &[]sharedsql.ForeignKeyInfo{})
	case methodListReferencingForeignKeys:
		return json.Unmarshal(raw, &[]sharedsql.ReferencingForeignKeyInfo{})
	case methodListForeignKeysAll:
		return json.Unmarshal(raw, &map[string][]sharedsql.ForeignKeyInfo{})
	case methodListIndexesAll:
		return json.Unmarshal(raw, &map[string][]sharedsql.IndexInfo{})
	case methodRowWrite:
		return json.Unmarshal(raw, &sharedsql.RowWriteResponse{})
	case methodDocumentWrite:
		return json.Unmarshal(raw, &sharedsql.DocumentWriteResponse{})
	}
	return fmt.Errorf("no result DTO for %s", method)
}

// validateResult applies the host's result-side boundary validation:
// the initialize protocol-version rule and the statement_metadata
// orphan rule on results and write results.
func validateResult(method string, raw json.RawMessage) error {
	switch method {
	case methodInitialize:
		var result initializeResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return err
		}
		if result.ProtocolVersion != ProtocolVersion {
			return fmt.Errorf("protocol version %d, want %d", result.ProtocolVersion, ProtocolVersion)
		}
	case methodExecute, methodExecuteReadOnly, methodBrowseTable:
		var result sharedsql.Result
		if err := json.Unmarshal(raw, &result); err != nil {
			return err
		}
		return checkStatementMetadata(method, result.Statement, result.StatementMetadata)
	case methodRowWrite:
		var response sharedsql.RowWriteResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			return err
		}
		_, err := resultFromWrite(method, response.Result)
		return err
	case methodDocumentWrite:
		var response sharedsql.DocumentWriteResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			return err
		}
		_, err := resultFromWrite(method, response.Result)
		return err
	}
	return nil
}
