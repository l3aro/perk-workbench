package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// This file validates the plugin test evidence document emitted by the
// real CLI against the canonical evidence JSON Schema
// (protocol/perk-v1/plugin-test-evidence.schema.json) with a small
// self-contained validator covering exactly the keyword subset the
// schema uses: type, properties, required, items, const, enum,
// pattern, minimum, $ref/$defs, and additionalProperties. The
// validator has teeth: every check appends to errs, and the negative
// control proves it rejects structurally invalid documents.

// miniSchemaError is one structural violation: the instance path and
// the violation text.
type miniSchemaError struct {
	path string
	text string
}

func (e miniSchemaError) Error() string { return e.path + ": " + e.text }

// validateAgainstSchema checks instance against the schema document
// (already parsed) and returns every violation found.
func validateAgainstSchema(schema, instance any) []error {
	root, ok := schema.(map[string]any)
	if !ok {
		return []error{fmt.Errorf("schema root is not an object")}
	}
	defs, _ := root["$defs"].(map[string]any)
	validator := &miniValidator{defs: defs}
	validator.check(schema, instance, "$")
	return validator.errs
}

type miniValidator struct {
	defs map[string]any
	errs []error
}

func (v *miniValidator) fail(path, format string, args ...any) {
	v.errs = append(v.errs, miniSchemaError{path: path, text: fmt.Sprintf(format, args...)})
}

func (v *miniValidator) check(node, instance any, path string) {
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	if ref, ok := obj["$ref"].(string); ok {
		def := strings.TrimPrefix(ref, "#/$defs/")
		target, ok := v.defs[def]
		if !ok {
			v.fail(path, "unresolved $ref %q", ref)
			return
		}
		v.check(target, instance, path)
		return
	}
	if typ, ok := obj["type"].(string); ok && !typeMatches(typ, instance) {
		v.fail(path, "want type %s, got %T", typ, instance)
	}
	if pattern, ok := obj["pattern"].(string); ok {
		value, isString := instance.(string)
		if !isString {
			v.fail(path, "pattern applies to strings, got %T", instance)
		} else if !regexp.MustCompile(pattern).MatchString(value) {
			v.fail(path, "value %q does not match pattern %q", value, pattern)
		}
	}
	if constant, ok := obj["const"]; ok && !reflect.DeepEqual(constant, instance) {
		v.fail(path, "want const %v, got %v", constant, instance)
	}
	if enum, ok := obj["enum"].([]any); ok {
		matched := false
		for _, item := range enum {
			if reflect.DeepEqual(item, instance) {
				matched = true
				break
			}
		}
		if !matched {
			v.fail(path, "%v is not one of the enum values", instance)
		}
	}
	if minimum, ok := obj["minimum"].(float64); ok {
		number, isNumber := instance.(float64)
		if !isNumber {
			v.fail(path, "minimum applies to numbers, got %T", instance)
		} else if number < minimum {
			v.fail(path, "%v is below the minimum %v", number, minimum)
		}
	}
	if properties, ok := obj["properties"].(map[string]any); ok {
		if instanceObject, ok := instance.(map[string]any); ok {
			for name, propertySchema := range properties {
				if value, present := instanceObject[name]; present {
					v.check(propertySchema, value, path+"."+name)
				}
			}
		}
	}
	if required, ok := obj["required"].([]any); ok {
		instanceObject, isObject := instance.(map[string]any)
		if isObject {
			for _, name := range required {
				if _, present := instanceObject[name.(string)]; !present {
					v.fail(path, "missing required property %q", name)
				}
			}
		} else {
			v.fail(path, "required applies to objects, got %T", instance)
		}
	}
	if items, ok := obj["items"].(map[string]any); ok {
		array, isArray := instance.([]any)
		if isArray {
			for i, item := range array {
				v.check(items, item, fmt.Sprintf("%s[%d]", path, i))
			}
		} else {
			v.fail(path, "items applies to arrays, got %T", instance)
		}
	}
	if additional, ok := obj["additionalProperties"].(bool); ok && !additional {
		if instanceObject, ok := instance.(map[string]any); ok {
			for name := range instanceObject {
				if _, declared := obj["properties"].(map[string]any)[name]; !declared {
					v.fail(path, "unexpected property %q", name)
				}
			}
		}
	}
}

func typeMatches(typ string, instance any) bool {
	switch typ {
	case "object":
		_, ok := instance.(map[string]any)
		return ok
	case "array":
		_, ok := instance.([]any)
		return ok
	case "string":
		_, ok := instance.(string)
		return ok
	case "boolean":
		_, ok := instance.(bool)
		return ok
	case "integer":
		number, ok := instance.(float64)
		return ok && number == math.Trunc(number)
	case "null":
		return instance == nil
	}
	return false
}

// loadEvidenceSchema reads and parses the canonical evidence schema
// from the repository tree — no copies.
func loadEvidenceSchema(t *testing.T) any {
	t.Helper()
	path := filepath.Join("..", "..", "protocol", "perk-v1", "plugin-test-evidence.schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("evidence schema does not parse: %v", err)
	}
	return schema
}

// parseDocument parses one emitted evidence document.
func parseDocument(t *testing.T, stdout string) any {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout %q is not one JSON document: %v", stdout, err)
	}
	return doc
}

// assertValid asserts the document validates structurally against the
// evidence schema with zero violations.
func assertValid(t *testing.T, schema, doc any) {
	t.Helper()
	errs := validateAgainstSchema(schema, doc)
	if len(errs) > 0 {
		t.Fatalf("emitted evidence document fails the evidence schema:\n%s", formatErrors(errs))
	}
}

func formatErrors(errs []error) string {
	var out strings.Builder
	for _, err := range errs {
		fmt.Fprintln(&out, "  -", err)
	}
	return out.String()
}

// TestEvidenceDocumentValidatesAgainstSchema: the real emitted
// documents — a passing run, a failing run, and a resolve failure —
// all validate structurally against the canonical evidence JSON
// Schema, so the evidence is machine-checkable by construction.
func TestEvidenceDocumentValidatesAgainstSchema(t *testing.T) {
	schema := loadEvidenceSchema(t)

	// A passing plugin.
	helper := setupPluginTestHelper(t, nil)
	status, stdout, stderr := runCLI(t, "plugin", "test", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("plugin test = %d, stderr %q, want 0", status, stderr)
	}
	assertValid(t, schema, parseDocument(t, stdout))

	// A failing plugin (fabricated response ids).
	broken := setupPluginTestHelper(t, map[string]string{"PERK_PLUGIN_BEHAVIOR": "wrong_id"})
	status, stdout, stderr = runCLI(t, "plugin", "test", "--json", broken)
	if status != 1 || stderr != "" {
		t.Fatalf("broken plugin test = %d, stderr %q, want 1", status, stderr)
	}
	assertValid(t, schema, parseDocument(t, stdout))

	// A resolve failure — no executable at all.
	missing := filepath.Join(t.TempDir(), "no-such-plugin")
	status, stdout, stderr = runCLI(t, "plugin", "test", "--json", missing)
	if status != 1 || stderr != "" {
		t.Fatalf("resolve failure = %d, stderr %q, want 1", status, stderr)
	}
	assertValid(t, schema, parseDocument(t, stdout))
}

// TestMiniValidatorHasTeeth: the mini validator is not a rubber stamp —
// a missing required field, a wrong type, a bad pattern, and an
// unknown enum value are all rejected.
func TestMiniValidatorHasTeeth(t *testing.T) {
	schema := loadEvidenceSchema(t)

	helper := setupPluginTestHelper(t, nil)
	status, stdout, stderr := runCLI(t, "plugin", "test", "--json", helper)
	if status != 0 || stderr != "" {
		t.Fatalf("plugin test = %d, stderr %q", status, stderr)
	}
	doc := parseDocument(t, stdout).(map[string]any)

	mutate := func(apply func(map[string]any)) map[string]any {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		var mutated map[string]any
		if err := json.Unmarshal(raw, &mutated); err != nil {
			t.Fatal(err)
		}
		apply(mutated)
		return mutated
	}

	// Missing required field.
	dropped := mutate(func(m map[string]any) { delete(m, "contract_sha256") })
	if errs := validateAgainstSchema(schema, dropped); len(errs) == 0 {
		t.Fatal("dropped required field still validates")
	}
	// Wrong type.
	badType := mutate(func(m map[string]any) { m["passed"] = "16" })
	if errs := validateAgainstSchema(schema, badType); len(errs) == 0 {
		t.Fatal("wrong type still validates")
	}
	// Bad digest pattern.
	badPattern := mutate(func(m map[string]any) { m["contract_sha256"] = "not-a-digest" })
	if errs := validateAgainstSchema(schema, badPattern); len(errs) == 0 {
		t.Fatal("bad digest pattern still validates")
	}
	// Unknown category enum value.
	badEnum := mutate(func(m map[string]any) {
		cases := m["cases"].([]any)
		first := cases[0].(map[string]any)
		first["category"] = "mystery"
	})
	if errs := validateAgainstSchema(schema, badEnum); len(errs) == 0 {
		t.Fatal("unknown failure category still validates")
	}
	// Unknown evidence version.
	badConst := mutate(func(m map[string]any) { m["evidence_version"] = 2 })
	if errs := validateAgainstSchema(schema, badConst); len(errs) == 0 {
		t.Fatal("wrong evidence version still validates")
	}
}
