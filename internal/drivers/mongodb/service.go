package mongodb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// sampleLimit is how many documents are inspected to infer a collection's
// fields for the structure tab. MongoDB is schemaless, so the structure tab
// reports the fields seen in a sample rather than a fixed schema.
const sampleLimit = 100

// Service is a MongoDB driver implementing the shared database contract.
// Collections map to tables; the structure tab shows sampled fields.
type Service struct {
	client *mongo.Client
	db     *mongo.Database
	info   sharedsql.DatabaseInfo
}

// Open connects to the MongoDB URI and selects the database from its path
// (default "test", matching the mongo shell).
func Open(ctx context.Context, target string) (*Service, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(target))
	if err != nil {
		return nil, fmt.Errorf("parsing MongoDB URI: %w", err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("connecting to MongoDB: %w", err)
	}
	service := &Service{client: client, db: client.Database(databaseName(target))}
	version, err := service.serverVersion(ctx)
	if err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("reading MongoDB version: %w", err)
	}
	service.info = sharedsql.DatabaseInfo{Product: "MongoDB", Version: version}
	return service, nil
}

func (s *Service) serverVersion(ctx context.Context) (string, error) {
	var info struct {
		Version string `bson:"version"`
	}
	if err := s.db.RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&info); err != nil {
		return "", err
	}
	return info.Version, nil
}

// databaseName extracts the database from a MongoDB URI path, defaulting to
// "test" like the mongo shell.
func databaseName(target string) string {
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "mongodb" && parsed.Scheme != "mongodb+srv") {
		return "test"
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if name == "" {
		return "test"
	}
	return name
}

func (s *Service) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}

func (s *Service) Info() sharedsql.DatabaseInfo { return s.info }

// ListSchema reports the connected database root and its collections.
// Other databases are not advertised: table operations carry only a
// collection name and would silently target the connected database.
func (s *Service) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	names, err := s.db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	objects := make([]sharedsql.SchemaObject, 0, len(names)+1)
	objects = append(objects, sharedsql.SchemaObject{Database: s.db.Name(), Type: "database", Name: s.db.Name()})
	for _, name := range names {
		objects = append(objects, sharedsql.SchemaObject{Database: s.db.Name(), Type: "collection", Name: name})
	}
	return objects, nil
}

func (s *Service) Execute(ctx context.Context, input string) (sharedsql.Result, error) {
	return s.execute(ctx, input, false)
}

func (s *Service) ExecuteReadOnly(ctx context.Context, input string) (sharedsql.Result, error) {
	return s.execute(ctx, input, true)
}

func (s *Service) execute(ctx context.Context, input string, readOnly bool) (sharedsql.Result, error) {
	statement, err := parseStatement(input)
	if err != nil {
		return sharedsql.Result{}, err
	}
	if readOnly && writeMethods[statement.method] {
		return sharedsql.Result{}, fmt.Errorf("read-only connection: %s is not allowed", statement.method)
	}
	started := time.Now()
	switch statement.method {
	case "show collections":
		names, err := s.db.ListCollectionNames(ctx, bson.D{})
		if err != nil {
			return sharedsql.Result{}, err
		}
		sort.Strings(names)
		docs := make([]bson.D, len(names))
		for i, name := range names {
			docs[i] = bson.D{{Key: "collection", Value: name}}
		}
		docs, truncated := keepRows(docs)
		result := documentsResult(docs, false, time.Since(started))
		result.Truncated = truncated
		return result, nil
	case "show dbs":
		names, err := s.client.ListDatabaseNames(ctx, bson.D{})
		if err != nil {
			return sharedsql.Result{}, err
		}
		sort.Strings(names)
		docs := make([]bson.D, len(names))
		for i, name := range names {
			docs[i] = bson.D{{Key: "database", Value: name}}
		}
		docs, truncated := keepRows(docs)
		result := documentsResult(docs, false, time.Since(started))
		result.Truncated = truncated
		return result, nil
	}
	return s.executeCommand(ctx, statement, started)
}

func (s *Service) executeCommand(ctx context.Context, st statement, started time.Time) (sharedsql.Result, error) {
	collection := s.db.Collection(st.collection)
	switch st.method {
	case "find", "findOne":
		filter := any(bson.D{})
		if len(st.args) > 0 {
			filter = st.args[0]
		}
		findOptions := options.Find()
		if st.method == "findOne" {
			findOptions.SetLimit(1)
		}
		if st.skip > 0 {
			findOptions.SetSkip(st.skip)
		}
		if st.limit > 0 {
			findOptions.SetLimit(st.limit)
		}
		if st.sort != nil {
			findOptions.SetSort(st.sort)
		}
		if len(st.args) > 1 {
			findOptions.SetProjection(st.args[1])
		} else if st.projection != nil {
			findOptions.SetProjection(st.projection)
		}
		docs, err := findDocuments(ctx, collection, filter, findOptions)
		if err != nil {
			return sharedsql.Result{}, err
		}
		docs, truncated := keepRows(docs)
		result := documentsResult(docs, false, time.Since(started))
		result.Truncated = truncated
		return result, nil
	case "countDocuments":
		filter := any(bson.D{})
		if len(st.args) > 0 {
			filter = st.args[0]
		}
		count, err := collection.CountDocuments(ctx, filter)
		if err != nil {
			return sharedsql.Result{}, err
		}
		return countResult(count, time.Since(started)), nil
	case "estimatedDocumentCount":
		count, err := collection.EstimatedDocumentCount(ctx)
		if err != nil {
			return sharedsql.Result{}, err
		}
		return countResult(count, time.Since(started)), nil
	case "aggregate":
		pipeline := st.args[0].(bson.A)
		cursor, err := collection.Aggregate(ctx, pipeline)
		if err != nil {
			return sharedsql.Result{}, err
		}
		docs, err := drainCursor(ctx, cursor)
		if err != nil {
			return sharedsql.Result{}, err
		}
		docs, truncated := keepRows(docs)
		result := documentsResult(docs, false, time.Since(started))
		result.Truncated = truncated
		return result, nil
	case "distinct":
		field := st.args[0].(string)
		filter := any(bson.D{})
		if len(st.args) > 1 {
			filter = st.args[1]
		}
		distinctResult := collection.Distinct(ctx, field, filter)
		values, err := distinctValues(distinctResult)
		if err != nil {
			return sharedsql.Result{}, err
		}
		docs := make([]bson.D, len(values))
		for i, value := range values {
			docs[i] = bson.D{{Key: "value", Value: value}}
		}
		docs, truncated := keepRows(docs)
		result := documentsResult(docs, false, time.Since(started))
		result.Truncated = truncated
		return result, nil
	case "insertOne":
		result, err := collection.InsertOne(ctx, st.args[0])
		if err != nil {
			return sharedsql.Result{}, err
		}
		summary := summaryResult(bson.D{{Key: "insertedId", Value: result.InsertedID}}, time.Since(started))
		summary.RowsAffected = 1
		return summary, nil
	case "insertMany":
		result, err := collection.InsertMany(ctx, st.args[0])
		if err != nil {
			return sharedsql.Result{}, err
		}
		summary := summaryResult(bson.D{
			{Key: "insertedCount", Value: len(result.InsertedIDs)},
			{Key: "insertedIds", Value: bson.A(result.InsertedIDs)},
		}, time.Since(started))
		summary.RowsAffected = int64(len(result.InsertedIDs))
		return summary, nil
	case "updateOne", "updateMany", "replaceOne":
		update := st.args[1]
		var result *mongo.UpdateResult
		var err error
		switch st.method {
		case "updateOne":
			updateOptions := options.UpdateOne()
			if st.upsert {
				updateOptions.SetUpsert(true)
			}
			result, err = collection.UpdateOne(ctx, st.args[0], update, updateOptions)
		case "updateMany":
			updateOptions := options.UpdateMany()
			if st.upsert {
				updateOptions.SetUpsert(true)
			}
			result, err = collection.UpdateMany(ctx, st.args[0], update, updateOptions)
		case "replaceOne":
			updateOptions := options.Replace()
			if st.upsert {
				updateOptions.SetUpsert(true)
			}
			result, err = collection.ReplaceOne(ctx, st.args[0], update, updateOptions)
		}
		if err != nil {
			return sharedsql.Result{}, err
		}
		pairs := bson.D{
			{Key: "matchedCount", Value: result.MatchedCount},
			{Key: "modifiedCount", Value: result.ModifiedCount},
		}
		if result.UpsertedCount > 0 {
			pairs = append(pairs, bson.E{Key: "upsertedId", Value: result.UpsertedID})
		}
		summary := summaryResult(pairs, time.Since(started))
		summary.RowsAffected = result.MatchedCount
		return summary, nil
	case "deleteOne", "deleteMany":
		filter := any(bson.D{})
		if len(st.args) > 0 {
			filter = st.args[0]
		}
		var result *mongo.DeleteResult
		var err error
		if st.method == "deleteOne" {
			result, err = collection.DeleteOne(ctx, filter)
		} else {
			result, err = collection.DeleteMany(ctx, filter)
		}
		if err != nil {
			return sharedsql.Result{}, err
		}
		summary := summaryResult(bson.D{{Key: "deletedCount", Value: result.DeletedCount}}, time.Since(started))
		summary.RowsAffected = result.DeletedCount
		return summary, nil
	case "drop":
		if err := collection.Drop(ctx); err != nil {
			return sharedsql.Result{}, err
		}
		summary := summaryResult(bson.D{{Key: "dropped", Value: st.collection}}, time.Since(started))
		summary.RowsAffected = 1
		return summary, nil
	case "createIndex":
		keys := st.args[0].(bson.D)
		indexOptions := options.Index()
		if len(st.args) > 1 {
			for _, elem := range st.args[1].(bson.D) {
				switch elem.Key {
				case "name":
					if name, ok := elem.Value.(string); ok {
						indexOptions.SetName(name)
					}
				case "unique":
					if unique, ok := elem.Value.(bool); ok {
						indexOptions.SetUnique(unique)
					}
				}
			}
		}
		name, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: keys, Options: indexOptions})
		if err != nil {
			return sharedsql.Result{}, err
		}
		summary := summaryResult(bson.D{{Key: "createdIndex", Value: name}}, time.Since(started))
		summary.RowsAffected = 1
		return summary, nil
	}
	return sharedsql.Result{}, fmt.Errorf("unsupported method %q", st.method)
}

func findDocuments(ctx context.Context, collection *mongo.Collection, filter any, findOptions *options.FindOptionsBuilder) ([]bson.D, error) {
	cursor, err := collection.Find(ctx, filter, findOptions)
	if err != nil {
		return nil, err
	}
	return drainCursor(ctx, cursor)
}

// drainCursor collects up to MaxRows+1 documents; the extra document lets
// callers detect the display cap or a following page.
func drainCursor(ctx context.Context, cursor *mongo.Cursor) ([]bson.D, error) {
	defer cursor.Close(context.Background())
	docs := make([]bson.D, 0, 16)
	for cursor.Next(ctx) {
		var doc bson.D
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
		if len(docs) == sharedsql.MaxRows+1 {
			break // display cap reached; caller decides how many rows to keep
		}
	}
	return docs, cursor.Err()
}

// keepRows applies the 500-row display cap, reporting whether rows were cut.
func keepRows(docs []bson.D) ([]bson.D, bool) {
	if len(docs) <= sharedsql.MaxRows {
		return docs, false
	}
	return docs[:sharedsql.MaxRows], true
}

func countResult(count int64, duration time.Duration) sharedsql.Result {
	return summaryResult(bson.D{{Key: "count", Value: count}}, duration)
}

func distinctValues(result *mongo.DistinctResult) (bson.A, error) {
	var values bson.A
	if err := result.Decode(&values); err != nil {
		return nil, err
	}
	return values, nil
}

// Validate parses the statement without executing it, so syntax and JSON
// errors surface while typing without any side effects.
func (s *Service) Validate(ctx context.Context, statement string) error {
	_, err := parseStatement(statement)
	return err
}

// BrowseTable queries a collection with the shared filter/sort/paging
// options. The Columns projection is ignored: documents carry their own
// fields, so restricting to sampled columns would hide data.
func (s *Service) BrowseTable(ctx context.Context, name string, browse sharedsql.BrowseOptions) (sharedsql.Result, error) {
	collection := s.db.Collection(name)
	started := time.Now()
	findOptions := options.Find()
	if len(browse.Sorts) > 0 {
		sort := make(bson.D, len(browse.Sorts))
		for i, option := range browse.Sorts {
			direction := 1
			if option.Descending {
				direction = -1
			}
			sort[i] = bson.E{Key: option.Column, Value: direction}
		}
		findOptions.SetSort(sort)
	}
	if browse.Offset > 0 {
		findOptions.SetSkip(int64(browse.Offset))
	}
	limit := browse.Limit
	if limit <= 0 {
		limit = sharedsql.MaxRows
	}
	if limit > sharedsql.MaxRows {
		limit = sharedsql.MaxRows
	}
	findOptions.SetLimit(int64(limit) + 1) // one extra document to detect the next page
	cursor, err := collection.Find(ctx, browseFilter(browse.Filters), findOptions)
	if err != nil {
		return sharedsql.Result{}, err
	}
	docs, err := drainCursor(ctx, cursor)
	if err != nil {
		return sharedsql.Result{}, err
	}
	hasMore := len(docs) > limit
	if hasMore {
		docs = docs[:limit]
	}
	result := documentsResult(docs, hasMore, time.Since(started))
	return result, nil
}

// browseFilter converts shared browse filters into a MongoDB query document.
func browseFilter(filters []sharedsql.BrowseFilter) bson.M {
	if len(filters) == 0 {
		return bson.M{}
	}
	query := make(bson.M, len(filters))
	for _, filter := range filters {
		column := filter.Column
		switch filter.Operator {
		case sharedsql.BrowseFilterEqual:
			query[column] = filterValue(filter.Value)
		case sharedsql.BrowseFilterNotEqual:
			query[column] = bson.M{"$ne": filterValue(filter.Value)}
		case sharedsql.BrowseFilterLess:
			query[column] = bson.M{"$lt": filterValue(filter.Value)}
		case sharedsql.BrowseFilterLessEqual:
			query[column] = bson.M{"$lte": filterValue(filter.Value)}
		case sharedsql.BrowseFilterGreater:
			query[column] = bson.M{"$gt": filterValue(filter.Value)}
		case sharedsql.BrowseFilterGreaterEqual:
			query[column] = bson.M{"$gte": filterValue(filter.Value)}
		case sharedsql.BrowseFilterIsNull:
			query[column] = nil
		case sharedsql.BrowseFilterIsNotNull:
			query[column] = bson.M{"$ne": nil}
		case sharedsql.BrowseFilterLike:
			query[column] = bson.M{"$regex": likeRegex(filter.Value)}
		case sharedsql.BrowseFilterNotLike:
			query[column] = bson.M{"$not": bson.M{"$regex": likeRegex(filter.Value)}}
		case sharedsql.BrowseFilterPattern:
			query[column] = bson.M{"$regex": sharedsql.GlobToRegex(filter.Value)}
		case sharedsql.BrowseFilterNotPattern:
			query[column] = bson.M{"$not": bson.M{"$regex": sharedsql.GlobToRegex(filter.Value)}}
		}
	}
	return query
}

// filterValue keeps numbers numeric so numeric fields compare correctly;
// anything else stays a string.
func filterValue(value string) any {
	if number, err := parseInt(value, "filter value"); err == nil {
		return number
	}
	if number, err := parseFloat(value); err == nil {
		return number
	}
	return value
}

func parseFloat(value string) (float64, error) {
	var number float64
	_, err := fmt.Sscan(value, &number)
	return number, err
}

// likeRegex converts an SQL LIKE pattern to a MongoDB regular expression.
func likeRegex(pattern string) string {
	var builder strings.Builder
	for _, r := range pattern {
		switch r {
		case '%':
			builder.WriteString(".*")
		case '_':
			builder.WriteString(".")
		default:
			if strings.ContainsRune(`.+*?^$()[]{}|\-`, r) {
				builder.WriteByte('\\')
			}
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// TableInfo reports the fields observed in a document sample. _id is the
// implicit primary key; every other field is nullable and untyped beyond
// the BSON type seen in the sample.
func (s *Service) TableInfo(ctx context.Context, name string) ([]sharedsql.ColumnInfo, error) {
	collection := s.db.Collection(name)
	cursor, err := collection.Find(ctx, bson.D{}, options.Find().SetLimit(sampleLimit))
	if err != nil {
		return nil, err
	}
	types := make(map[string]string)
	var order []string
	for cursor.Next(ctx) {
		var doc bson.D
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		for _, elem := range doc {
			if _, seen := types[elem.Key]; seen {
				continue
			}
			types[elem.Key] = bsonTypeName(elem.Value)
			order = append(order, elem.Key)
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	sort.Strings(order)
	columns := make([]sharedsql.ColumnInfo, 0, len(order))
	for _, name := range order {
		column := sharedsql.ColumnInfo{Name: name, Type: types[name], Nullable: true}
		if name == "_id" {
			column.Nullable = false
			column.PrimaryKey = 1
			column.Indexes = []sharedsql.IndexKind{sharedsql.IndexPrimaryKey}
		}
		columns = append(columns, column)
	}
	return columns, nil
}

// ListIndexes reports the real MongoDB indexes of a collection.
func (s *Service) ListIndexes(ctx context.Context, table string) ([]sharedsql.IndexInfo, error) {
	specifications, err := s.db.Collection(table).Indexes().ListSpecifications(ctx)
	if err != nil {
		return nil, err
	}
	indexes := make([]sharedsql.IndexInfo, 0, len(specifications))
	for _, specification := range specifications {
		index := sharedsql.IndexInfo{Name: specification.Name}
		if specification.KeysDocument != nil {
			if elements, err := specification.KeysDocument.Elements(); err == nil {
				for _, element := range elements {
					index.Columns = append(index.Columns, element.Key())
				}
			}
		}
		switch {
		case specification.Name == "_id_":
			index.PrimaryKey = true
		case specification.Unique != nil && *specification.Unique:
			index.Unique = true
		}
		indexes = append(indexes, index)
	}
	return indexes, nil
}

func (s *Service) CreateIndex(ctx context.Context, table string, change sharedsql.IndexChange) error {
	if change.PrimaryKey {
		return errors.New("MongoDB uses the implicit _id primary key; create a unique index instead")
	}
	keys := make(bson.D, len(change.Columns))
	for i, column := range change.Columns {
		keys[i] = bson.E{Key: column, Value: 1}
	}
	indexOptions := options.Index().SetUnique(change.Unique)
	if strings.TrimSpace(change.Name) != "" {
		indexOptions.SetName(change.Name)
	}
	_, err := s.db.Collection(table).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: keys, Options: indexOptions})
	return err
}

func (s *Service) ReplaceIndex(ctx context.Context, table, previous string, change sharedsql.IndexChange) error {
	if err := s.DropIndex(ctx, table, previous); err != nil {
		return err
	}
	return s.CreateIndex(ctx, table, change)
}

func (s *Service) DropIndex(ctx context.Context, table, name string) error {
	if name == "_id_" {
		return errors.New("cannot drop the _id index")
	}
	return s.db.Collection(table).Indexes().DropOne(ctx, name)
}

// MongoDB has no foreign keys: the list queries return empty, and creating
// or dropping one is an error.

func (s *Service) ListForeignKeys(ctx context.Context, table string) ([]sharedsql.ForeignKeyInfo, error) {
	return nil, nil
}

func (s *Service) ListReferencingForeignKeys(ctx context.Context, table string) ([]sharedsql.ReferencingForeignKeyInfo, error) {
	return nil, nil
}

func (s *Service) ListForeignKeysAll(ctx context.Context) (map[string][]sharedsql.ForeignKeyInfo, error) {
	return map[string][]sharedsql.ForeignKeyInfo{}, nil
}

// ListIndexesAll returns every collection's indexes, keyed by collection
// name. The driver API has no bulk index listing, so each collection is
// queried once.
func (s *Service) ListIndexesAll(ctx context.Context) (map[string][]sharedsql.IndexInfo, error) {
	names, err := s.db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	indexes := make(map[string][]sharedsql.IndexInfo, len(names))
	for _, name := range names {
		collectionIndexes, err := s.ListIndexes(ctx, name)
		if err != nil {
			return nil, err
		}
		indexes[name] = collectionIndexes
	}
	return indexes, nil
}

func (s *Service) CreateForeignKey(ctx context.Context, table string, change sharedsql.ForeignKeyChange) error {
	return errors.New("MongoDB does not support foreign keys")
}

func (s *Service) ReplaceForeignKey(ctx context.Context, table, previous string, change sharedsql.ForeignKeyChange) error {
	return errors.New("MongoDB does not support foreign keys")
}

func (s *Service) DropForeignKey(ctx context.Context, table, previous string) error {
	return errors.New("MongoDB does not support foreign keys")
}

// MongoDB is schemaless: column DDL has no target. Editing documents is the
// equivalent operation.

func (s *Service) AlterColumn(ctx context.Context, table string, change sharedsql.ColumnChange) error {
	return errors.New("MongoDB collections have no fixed columns; edit documents directly")
}

func (s *Service) AddColumn(ctx context.Context, table string, column sharedsql.ColumnDef) error {
	return errors.New("MongoDB collections have no fixed columns; add fields in documents")
}

func (s *Service) DropColumn(ctx context.Context, table, name string) error {
	return errors.New("MongoDB collections have no fixed columns; remove fields in documents")
}
