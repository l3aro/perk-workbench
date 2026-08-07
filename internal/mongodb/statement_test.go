package mongodb

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestParseStatement_valid(t *testing.T) {
	for _, test := range []struct {
		input string
		check func(t *testing.T, st statement)
	}{
		{input: "show collections", check: func(t *testing.T, st statement) {
			if st.method != "show collections" {
				t.Fatalf("method = %q, want show collections", st.method)
			}
		}},
		{input: "show dbs", check: func(t *testing.T, st statement) {
			if st.method != "show dbs" {
				t.Fatalf("method = %q, want show dbs", st.method)
			}
		}},
		{input: "db.restaurants.find()", check: func(t *testing.T, st statement) {
			if st.collection != "restaurants" || st.method != "find" || len(st.args) != 0 {
				t.Fatalf("got %+v", st)
			}
		}},
		{input: `db.restaurants.find({"borough": "Bronx"})`, check: func(t *testing.T, st statement) {
			filter, ok := st.args[0].(bson.D)
			if !ok || len(filter) != 1 || filter[0].Key != "borough" || filter[0].Value != "Bronx" {
				t.Fatalf("filter = %#v", st.args)
			}
		}},
		{input: `db.restaurants.find({"borough": "Bronx"}, {"name": 1}).limit(5).skip(2).sort({"name": -1})`, check: func(t *testing.T, st statement) {
			if st.limit != 5 || st.skip != 2 {
				t.Fatalf("limit/skip = %d/%d", st.limit, st.skip)
			}
			if st.sort == nil || len(st.sort) != 1 || st.sort[0].Value != int32(-1) {
				t.Fatalf("sort = %#v", st.sort)
			}
			if st.projection != nil {
				t.Fatalf("projection should come from the second argument, got %#v", st.projection)
			}
		}},
		{input: `db.restaurants.find().project({"name": 1})`, check: func(t *testing.T, st statement) {
			if st.projection == nil || len(st.projection) != 1 {
				t.Fatalf("projection = %#v", st.projection)
			}
		}},
		{input: `db.restaurants.findOne({"_id": ObjectId("56d61033a378eccde8a8354f")})`, check: func(t *testing.T, st statement) {
			filter, ok := st.args[0].(bson.D)
			if !ok || len(filter) != 1 || filter[0].Key != "_id" {
				t.Fatalf("filter = %#v", st.args)
			}
			id, ok := filter[0].Value.(bson.ObjectID)
			if !ok || id.Hex() != "56d61033a378eccde8a8354f" {
				t.Fatalf("_id = %#v", filter[0].Value)
			}
		}},
		{input: `db.restaurants.find({"grades": {"$elemMatch": {"grade": "A"}}})`, check: func(t *testing.T, st statement) {
			if len(st.args) != 1 {
				t.Fatalf("args = %#v", st.args)
			}
		}},
		{input: `db.restaurants.updateOne({"name": "Morris Park Bake Shop"}, {"$set": {"cuisine": "Bakery"}}, {"upsert": true})`, check: func(t *testing.T, st statement) {
			if !st.upsert || len(st.args) != 3 {
				t.Fatalf("st = %+v", st)
			}
		}},
		{input: `db.restaurants.aggregate([{"$match": {"borough": "Queens"}}, {"$count": "total"}])`, check: func(t *testing.T, st statement) {
			pipeline, ok := st.args[0].(bson.A)
			if !ok || len(pipeline) != 2 {
				t.Fatalf("pipeline = %#v", st.args)
			}
		}},
		{input: `db.restaurants.distinct("borough")`, check: func(t *testing.T, st statement) {
			if st.args[0] != "borough" {
				t.Fatalf("field = %#v", st.args[0])
			}
		}},
		{input: `db.restaurants.insertOne({"name": "Test"})`, check: func(t *testing.T, st statement) {
			if _, ok := st.args[0].(bson.D); !ok {
				t.Fatalf("doc = %#v", st.args[0])
			}
		}},
		{input: `db.restaurants.createIndex({"borough": 1})`, check: func(t *testing.T, st statement) {
			keys, ok := st.args[0].(bson.D)
			if !ok || len(keys) != 1 || keys[0].Key != "borough" {
				t.Fatalf("keys = %#v", st.args[0])
			}
		}},
		{input: `db.restaurants.createIndex({"borough": 1, "cuisine": -1}, {"name": "borough_cuisine", "unique": true})`, check: func(t *testing.T, st statement) {
			options, ok := st.args[1].(bson.D)
			if !ok || len(options) != 2 {
				t.Fatalf("options = %#v", st.args[1])
			}
		}},
		{input: `db["weird-name"].find()`, check: func(t *testing.T, st statement) {
			if st.collection != "" || st.method != "" {
				t.Fatalf("expected parse failure, got %+v", st)
			}
		}},
	} {
		t.Run(test.input, func(t *testing.T) {
			st, err := parseStatement(test.input)
			if err != nil {
				if strings.Contains(test.input, "weird-name") {
					return // bracket collection access is rejected
				}
				t.Fatalf("parseStatement(%q) error = %v", test.input, err)
			}
			test.check(t, st)
		})
	}
}

func TestParseStatement_invalid(t *testing.T) {
	for _, input := range []string{
		"",
		"   ",
		"SELECT * FROM restaurants",
		"db.restaurants",
		"db.restaurants.unknown()",
		"db.restaurants.find(",
		"db.restaurants.find({)",
		"db.restaurants.find({bad json)",
		"db.restaurants.countDocuments({}, {})",
		"db.restaurants.aggregate({})",
		"db.restaurants.insertOne()",
		"db.restaurants.insertOne([])",
		"db.restaurants.distinct(5)",
		"db.restaurants.find().sort(5)",
		"db.restaurants.find().bogus()",
		"db.restaurants.find().limit(-1)",
		"db.restaurants.countDocuments().limit(5)",
		"db.restaurants.updateOne({})",
		"db. restaurants.find()",
	} {
		t.Run(input, func(t *testing.T) {
			if st, err := parseStatement(input); err == nil {
				t.Fatalf("parseStatement(%q) = %+v, want error", input, st)
			}
		})
	}
}

func TestSplitArgs_ignoresCommasInsideStringsAndBrackets(t *testing.T) {
	parts := splitArgs(`{"a": [1, 2], "b": "x,y"}, {"c": 3}`)
	if len(parts) != 2 {
		t.Fatalf("parts = %#v", parts)
	}
	if got, want := strings.TrimSpace(parts[0]), `{"a": [1, 2], "b": "x,y"}`; got != want {
		t.Fatalf("part 0 = %q, want %q", got, want)
	}
}

func TestParseCall_handlesNestedParensAndStrings(t *testing.T) {
	name, args, tail, err := parseCall(`find({"regex": "a(b)c"}, {"x": 1})tail`)
	if err != nil {
		t.Fatal(err)
	}
	if name != "find" || args != `{"regex": "a(b)c"}, {"x": 1}` || tail != "tail" {
		t.Fatalf("name=%q args=%q tail=%q", name, args, tail)
	}
}
