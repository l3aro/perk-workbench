package mongodb

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

// formatValue renders a BSON value for a result cell: strings stay bare and
// nested documents use mongosh-style {key: value} syntax.
func formatValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int32, int64, float64:
		return strconv.FormatFloat(bsonNumber(v), 'g', -1, 64)
	case bson.ObjectID:
		return `ObjectId("` + v.Hex() + `")`
	case time.Time:
		return v.Format(time.RFC3339)
	case bson.DateTime:
		return v.Time().Format(time.RFC3339)
	case bson.Decimal128:
		return v.String()
	case bson.Binary:
		return "0x" + hex(v.Data)
	case []byte:
		return "0x" + hex(v)
	case bson.Regex:
		return "/" + v.Pattern + "/" + v.Options
	case bson.Timestamp:
		return fmt.Sprintf("Timestamp(%d, %d)", v.T, v.I)
	case bson.JavaScript:
		return string(v)
	case bson.CodeWithScope:
		scope := ""
		switch s := v.Scope.(type) {
		case bson.D:
			scope = " " + formatDoc(s)
		case bson.M:
			scope = " " + formatDoc(sortedDoc(s))
		}
		return string(v.Code) + scope
	case bson.DBPointer:
		return fmt.Sprintf("DBPointer(%q, %s)", v.DB, formatValue(v.Pointer))
	case bson.Undefined:
		return "undefined"
	case bson.MinKey:
		return "MinKey()"
	case bson.MaxKey:
		return "MaxKey()"
	case bson.Symbol:
		return string(v)
	case bson.D:
		return formatDoc(v)
	case bson.A:
		return formatArray(v)
	case bson.M:
		return formatDoc(sortedDoc(v))
	default:
		return stringify(value)
	}
}

// formatViewValue renders the full cell value for the viewer and the copy
// action. Objects, arrays, binary, and BSON constructor values (regex,
// timestamp, code, $minKey, ...) become valid extended JSON so inspected or
// copied values paste straight into mongosh, mongoimport, or jq. Scalars,
// ObjectIds, and dates stay in their human-readable forms: ObjectId("...")
// is directly usable in mongosh queries and this workbench's own editor,
// and RFC3339 dates need no wrapper.
func formatViewValue(value any) string {
	switch value.(type) {
	case bson.D, bson.M, map[string]any:
		return stringify(value) // top-level documents are writable as-is
	case bson.A, []any, []byte, bson.Binary, bson.Regex, bson.Timestamp,
		bson.JavaScript, bson.CodeWithScope, bson.DBPointer, bson.Undefined,
		bson.MinKey, bson.MaxKey, bson.Symbol:
		// The extended-JSON writer rejects arrays, binary, and constructor
		// types at top level, so they are written inside a wrapper document
		// and the wrapper is stripped, yielding the writer's exact nested
		// representation (mongoexport's format).
		if json, ok := wrappedExtJSON(value); ok {
			return json
		}
	}
	return formatValue(value)
}

type formattedCell struct {
	compact string
	full    string
}

// formatCell computes the compact cell text once. Values that need a valid
// extended-JSON representation for viewing or copying are formatted again;
// scalar values keep the compact representation for both result fields.
func formatCell(value any) formattedCell {
	compact := formatValue(value)
	full := compact
	switch value.(type) {
	case bson.D, bson.M, map[string]any,
		bson.A, []any, []byte, bson.Binary, bson.Regex, bson.Timestamp,
		bson.JavaScript, bson.CodeWithScope, bson.DBPointer, bson.Undefined,
		bson.MinKey, bson.MaxKey, bson.Symbol:
		full = formatViewValue(value)
	}
	return formattedCell{compact: compact, full: full}
}

// wrappedExtJSON renders a value as extended JSON by wrapping it in a
// one-field document (which the writer accepts) and stripping the wrapper.
func wrappedExtJSON(value any) (string, bool) {
	data, err := bson.MarshalExtJSON(bson.D{{Key: "x", Value: value}}, false, false)
	if err != nil {
		return "", false
	}
	text := string(data)
	colon := strings.IndexByte(text, ':')
	if colon < 0 || len(text) < 2 || text[len(text)-1] != '}' {
		return "", false
	}
	return strings.TrimSpace(text[colon+1 : len(text)-1]), true
}

// formatJSONValue renders a nested value: strings are quoted so the parent
// document reads as JSON-like text.
func formatJSONValue(value any) string {
	if text, ok := value.(string); ok {
		return strconv.Quote(text)
	}
	return formatValue(value)
}

func formatDoc(doc bson.D) string {
	var builder strings.Builder
	builder.WriteByte('{')
	for i, elem := range doc {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(elem.Key)
		builder.WriteString(": ")
		builder.WriteString(formatJSONValue(elem.Value))
	}
	builder.WriteByte('}')
	return builder.String()
}

func formatArray(array bson.A) string {
	items := make([]string, len(array))
	for i, value := range array {
		items[i] = formatJSONValue(value)
	}
	return "[" + strings.Join(items, ", ") + "]"
}

// sortedDoc renders a map-typed value with stable key order.
func sortedDoc(doc bson.M) bson.D {
	keys := make([]string, 0, len(doc))
	for key := range doc {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(bson.D, len(keys))
	for i, key := range keys {
		out[i] = bson.E{Key: key, Value: doc[key]}
	}
	return out
}

func bsonNumber(value any) float64 {
	switch v := value.(type) {
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	}
	return 0
}

func stringify(value any) string {
	data, err := bson.MarshalExtJSON(value, false, false)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func hex(bytes []byte) string {
	const digits = "0123456789ABCDEF"
	var builder strings.Builder
	for _, b := range bytes {
		builder.WriteByte(digits[b>>4])
		builder.WriteByte(digits[b&0xf])
	}
	return builder.String()
}

// bsonTypeName maps a BSON value to a display type name.
func bsonTypeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "bool"
	case int32:
		return "int"
	case int64:
		return "long"
	case float64:
		return "double"
	case bson.ObjectID:
		return "objectId"
	case time.Time, bson.DateTime:
		return "date"
	case bson.Decimal128:
		return "decimal"
	case bson.Binary, []byte:
		return "binary"
	case bson.Regex:
		return "regex"
	case bson.Timestamp:
		return "timestamp"
	case bson.JavaScript:
		return "javascript"
	case bson.CodeWithScope:
		return "codeWithScope"
	case bson.DBPointer:
		return "dbPointer"
	case bson.Undefined:
		return "undefined"
	case bson.MinKey:
		return "minKey"
	case bson.MaxKey:
		return "maxKey"
	case bson.Symbol:
		return "symbol"
	case bson.D, bson.M, map[string]any:
		return "object"
	case bson.A, []any:
		return "array"
	default:
		return "unknown"
	}
}

// documentColumns returns the union of top-level keys across documents,
// _id first, then alphabetically.
func documentColumns(docs []bson.D) []string {
	seen := make(map[string]bool)
	for _, doc := range docs {
		for _, elem := range doc {
			seen[elem.Key] = true
		}
	}
	columns := make([]string, 0, len(seen))
	for key := range seen {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	// Move _id to the front, keeping the remainder sorted.
	for i := 1; i < len(columns); i++ {
		if columns[i] == "_id" {
			id := columns[i]
			copy(columns[1:], columns[:i])
			columns[0] = id
			break
		}
	}
	return columns
}

// documentsResult converts decoded documents into the shared result shape:
// display cells use compact mongosh-style formatting and are sanitized and
// capped at MaxRunes; full cells render objects, arrays, and binary as
// extended JSON for the cell viewer and the copy action.
func documentsResult(docs []bson.D, hasMore bool, duration time.Duration) driver.Result {
	columns := documentColumns(docs)
	result := driver.Result{
		Columns:         columns,
		ColumnTypes:     make([]string, len(columns)),
		Rows:            make([][]*string, len(docs)),
		UntruncatedRows: make([][]*string, len(docs)),
		DocumentIDs:     make([]driver.DocumentPayload, len(docs)),
		HasMore:         hasMore,
		DurationNS:      int64(duration),
	}
	for index, column := range columns {
		for _, doc := range docs {
			if value, ok := docValue(doc, column); ok {
				result.ColumnTypes[index] = bsonTypeName(value)
				break
			}
		}
	}
	for rowIndex, doc := range docs {
		result.Rows[rowIndex] = make([]*string, len(columns))
		result.UntruncatedRows[rowIndex] = make([]*string, len(columns))
		for columnIndex, column := range columns {
			value, ok := docValue(doc, column)
			if !ok {
				continue
			}
			formatted := formatCell(value)
			display := sanitizeDisplay(formatted.compact, maxRunes)
			result.Rows[rowIndex][columnIndex] = &display
			full := formatted.full
			result.UntruncatedRows[rowIndex][columnIndex] = &full
		}
		// Stable document identity: the _id scalar as relaxed extended
		// JSON, independent of the display cell (ObjectIds render as
		// ObjectId("..."), which is not the declared payload format).
		if id, ok := docValue(doc, "_id"); ok {
			if json, ok := wrappedExtJSON(id); ok {
				result.DocumentIDs[rowIndex] = driver.DocumentPayload{
					Format: driver.DocumentFormatMongoExtendedJSON,
					Data:   []byte(json),
				}
			}
		}
	}
	return result
}

// summaryResult renders command metrics (insertedId, deletedCount, ...) as a
// two-column field/value table.
func summaryResult(pairs bson.D, duration time.Duration) driver.Result {
	return documentsResult([]bson.D{pairs}, false, duration)
}

func docValue(doc bson.D, key string) (any, bool) {
	for _, elem := range doc {
		if elem.Key == key {
			return elem.Value, true
		}
	}
	return nil, false
}
