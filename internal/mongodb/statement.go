// Package mongodb implements the shared database service contract against
// MongoDB. Connections open one database (the URI path, default "test"); the
// schema pane lists that database's collections, which map to tables.
//
// The query editor accepts mongosh-style statements instead of SQL:
//
//	db.<collection>.find([filter[, projection]])[.sort(doc)][.skip(n)][.limit(n)][.project(doc)]
//	db.<collection>.findOne([filter[, projection]])
//	db.<collection>.countDocuments([filter])
//	db.<collection>.estimatedDocumentCount()
//	db.<collection>.aggregate([pipeline])
//	db.<collection>.distinct(field[, filter])
//	db.<collection>.insertOne(doc)
//	db.<collection>.insertMany([docs])
//	db.<collection>.updateOne(filter, update[, {upsert: true}])
//	db.<collection>.updateMany(filter, update[, {upsert: true}])
//	db.<collection>.replaceOne(filter, doc[, {upsert: true}])
//	db.<collection>.deleteOne([filter])
//	db.<collection>.deleteMany([filter])
//	db.<collection>.drop()
//	db.<collection>.createIndex(keys[, options])
//	show collections
//	show dbs
//
// Arguments are JSON documents in MongoDB extended-JSON relaxed form, so
// dates and ObjectIds can be written as {"$date": ...} / {"$oid": "..."};
// ObjectId("...") is also accepted as a shorthand.
package mongodb

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// statement is one parsed mongosh-style command.
type statement struct {
	collection string
	method     string // find, findOne, ..., or "show collections"/"show dbs"
	args       []any  // parsed JSON arguments; absent arguments are omitted
	limit      int64
	skip       int64
	sort       bson.D
	projection bson.D
	upsert     bool
}

var readMethods = map[string]bool{
	"find": true, "findOne": true, "countDocuments": true,
	"estimatedDocumentCount": true, "aggregate": true, "distinct": true,
}

var writeMethods = map[string]bool{
	"insertOne": true, "insertMany": true, "updateOne": true, "updateMany": true,
	"replaceOne": true, "deleteOne": true, "deleteMany": true, "drop": true,
	"createIndex": true,
}

func parseStatement(input string) (statement, error) {
	text := strings.TrimSpace(input)
	if text == "" {
		return statement{}, errors.New("statement is empty")
	}
	if text == "show collections" || text == "show dbs" {
		return statement{method: text}, nil
	}
	rest, ok := strings.CutPrefix(text, "db.")
	if !ok {
		return statement{}, errors.New(`unsupported statement: expected db.<collection>.<method>(...) or "show collections"/"show dbs"`)
	}
	collection, rest, ok := strings.Cut(rest, ".")
	if !ok || strings.TrimSpace(collection) == "" {
		return statement{}, errors.New("missing collection name after db.")
	}
	if !validName(collection) {
		return statement{}, fmt.Errorf("invalid collection name %q", collection)
	}
	method, argsText, tail, err := parseCall(rest)
	if err != nil {
		return statement{}, err
	}
	if !readMethods[method] && !writeMethods[method] {
		return statement{}, fmt.Errorf("unknown method %q", method)
	}
	st := statement{collection: collection, method: method}
	if st.args, err = parseArgs(argsText, method); err != nil {
		return statement{}, err
	}
	// The optional third update argument carries the upsert flag.
	if len(st.args) == 3 && (method == "updateOne" || method == "updateMany" || method == "replaceOne") {
		options, ok := st.args[2].(bson.D)
		if !ok {
			return statement{}, fmt.Errorf("%s: third argument must be an options document", method)
		}
		for _, elem := range options {
			if elem.Key == "upsert" {
				if value, ok := elem.Value.(bool); ok {
					st.upsert = value
				}
			}
		}
	}
	for tail = strings.TrimSpace(tail); tail != ""; {
		name, inner, next, err := parseCall(strings.TrimPrefix(tail, "."))
		if err != nil {
			return statement{}, err
		}
		switch name {
		case "limit":
			if st.limit, err = parseInt(inner, "limit"); err != nil {
				return statement{}, err
			}
		case "skip":
			if st.skip, err = parseInt(inner, "skip"); err != nil {
				return statement{}, err
			}
		case "sort":
			if st.sort, err = decodeDoc(inner); err != nil {
				return statement{}, fmt.Errorf("sort: %w", err)
			}
		case "project":
			if st.projection, err = decodeDoc(inner); err != nil {
				return statement{}, fmt.Errorf("project: %w", err)
			}
		default:
			return statement{}, fmt.Errorf("unknown chain method %q", name)
		}
		tail = strings.TrimSpace(next)
	}
	if (st.limit != 0 || st.skip != 0 || st.sort != nil || st.projection != nil) &&
		st.method != "find" && st.method != "findOne" {
		return statement{}, fmt.Errorf("%s does not accept .sort/.skip/.limit/.project", st.method)
	}
	return st, nil
}

// parseCall splits "name(args)tail" into its parts. The argument scan is
// quote- and bracket-aware so nested JSON never ends it early.
func parseCall(text string) (name, args, tail string, err error) {
	open := strings.IndexByte(text, '(')
	if open <= 0 {
		return "", "", "", errors.New("expected '(' after method name")
	}
	name = strings.TrimSpace(text[:open])
	if !validName(name) {
		return "", "", "", fmt.Errorf("invalid method name %q", name)
	}
	depth := 0
	quoted := byte(0)
	for i := open; i < len(text); i++ {
		switch {
		case quoted != 0:
			if text[i] == '\\' && i+1 < len(text) {
				i++
			} else if text[i] == quoted {
				quoted = 0
			}
		case text[i] == '"' || text[i] == '\'':
			quoted = text[i]
		case text[i] == '(' || text[i] == '[' || text[i] == '{':
			depth++
		case text[i] == ')' || text[i] == ']' || text[i] == '}':
			depth--
			if depth == 0 {
				return name, text[open+1 : i], text[i+1:], nil
			}
		}
	}
	return "", "", "", errors.New("unbalanced parentheses")
}

// parseInt parses the body of .limit(n)/.skip(n).
func parseInt(text, what string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", what)
	}
	return value, nil
}

// parseArgs converts the comma-separated argument list of a method call into
// typed values, validating arity and argument kinds per method.
func parseArgs(text, method string) ([]any, error) {
	var args []any
	if strings.TrimSpace(text) != "" {
		for _, part := range splitArgs(text) {
			value, err := parseArg(part)
			if err != nil {
				return nil, err
			}
			args = append(args, value)
		}
	}
	want := map[string][2]int{
		"find": {0, 2}, "findOne": {0, 2}, "countDocuments": {0, 1},
		"estimatedDocumentCount": {0, 0}, "aggregate": {1, 1}, "distinct": {1, 2},
		"insertOne": {1, 1}, "insertMany": {1, 1}, "updateOne": {2, 3},
		"updateMany": {2, 3}, "replaceOne": {2, 3}, "deleteOne": {0, 1},
		"deleteMany": {0, 1}, "drop": {0, 0}, "createIndex": {1, 2},
	}[method]
	if len(args) < want[0] || len(args) > want[1] {
		return nil, fmt.Errorf("%s expects %d to %d arguments, got %d", method, want[0], want[1], len(args))
	}
	switch method {
	case "distinct":
		if _, ok := args[0].(string); !ok {
			return nil, fmt.Errorf("distinct: first argument must be a field name string")
		}
	case "createIndex":
		if _, ok := args[0].(bson.D); !ok {
			return nil, fmt.Errorf("createIndex: first argument must be a keys document")
		}
	case "aggregate", "insertMany":
		if _, ok := args[0].(bson.A); !ok {
			return nil, fmt.Errorf("%s: argument must be a JSON array", method)
		}
	case "insertOne":
		if _, ok := args[0].(bson.D); !ok {
			return nil, fmt.Errorf("insertOne: argument must be a document")
		}
	case "updateOne", "updateMany", "replaceOne":
		if _, ok := args[0].(bson.D); !ok {
			return nil, fmt.Errorf("%s: first argument must be a filter document", method)
		}
		if _, ok := args[1].(bson.D); !ok {
			return nil, fmt.Errorf("%s: second argument must be a document", method)
		}
	}
	return args, nil
}

// splitArgs splits on top-level commas, ignoring those inside strings,
// objects, and arrays.
func splitArgs(text string) []string {
	var parts []string
	depth := 0
	quoted := byte(0)
	start := 0
	for i := 0; i < len(text); i++ {
		switch {
		case quoted != 0:
			if text[i] == '\\' && i+1 < len(text) {
				i++
			} else if text[i] == quoted {
				quoted = 0
			}
		case text[i] == '"' || text[i] == '\'':
			quoted = text[i]
		case text[i] == '(' || text[i] == '[' || text[i] == '{':
			depth++
		case text[i] == ')' || text[i] == ']' || text[i] == '}':
			depth--
		case text[i] == ',' && depth == 0:
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	return append(parts, text[start:])
}

var (
	objectIDPattern = regexp.MustCompile(`ObjectId\("([0-9a-fA-F]{24})"\)|ObjectId\('([0-9a-fA-F]{24})'\)`)
)

// parseArg parses one JSON argument. ObjectId("...") is rewritten to its
// extended-JSON form so mongosh-style queries paste cleanly.
func parseArg(text string) (any, error) {
	text = strings.TrimSpace(text)
	replaced := objectIDPattern.ReplaceAllStringFunc(text, func(match string) string {
		return `{"$oid":"` + match[10:len(match)-2] + `"}`
	})
	var value any
	if err := bson.UnmarshalExtJSON([]byte(replaced), false, &value); err != nil {
		return nil, fmt.Errorf("invalid JSON argument: %w", err)
	}
	return value, nil
}

// decodeDoc parses a document argument, preserving key order.
func decodeDoc(text string) (bson.D, error) {
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(text), false, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < 0x21 || r > 0x7e || r == '(' || r == ')' || r == '.' || r == ' ' {
			return false
		}
	}
	return true
}
