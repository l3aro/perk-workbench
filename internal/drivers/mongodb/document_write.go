package mongodb

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

// WriteCapabilities reports MongoDB's document-write capability: relaxed
// extended JSON, editable as whole-document text.
func (s *Service) WriteCapabilities() driver.WriteCapabilities {
	return driver.WriteCapabilities{
		Document: &driver.DocumentWriteCapability{
			Format: driver.DocumentFormatMongoExtendedJSON,
			Text:   true,
		},
	}
}

// ReadDocument loads the complete document identified by id. The id payload
// is the extended-JSON encoding of the _id value (a scalar), not a full
// document.
func (s *Service) ReadDocument(ctx context.Context, collection string, id driver.DocumentPayload) (driver.DocumentPayload, error) {
	identity, err := parseIdentity(id)
	if err != nil {
		return driver.DocumentPayload{}, err
	}
	var doc bson.D
	if err := s.db.Collection(collection).FindOne(ctx, bson.D{{Key: "_id", Value: identity}}).Decode(&doc); err != nil {
		return driver.DocumentPayload{}, err
	}
	data, err := bson.MarshalExtJSON(doc, false, false)
	if err != nil {
		return driver.DocumentPayload{}, err
	}
	return driver.DocumentPayload{Format: driver.DocumentFormatMongoExtendedJSON, Data: data}, nil
}

// InsertDocument inserts one document from its extended-JSON payload.
// Non-document bodies (arrays, scalars, malformed text) fail without
// mutating data.
func (s *Service) InsertDocument(ctx context.Context, collection string, doc driver.DocumentPayload) (driver.Result, error) {
	if doc.Format != driver.DocumentFormatMongoExtendedJSON {
		return driver.Result{}, fmt.Errorf("unsupported document format %s", doc.Format)
	}
	document, err := decodeDocument(doc)
	if err != nil {
		return driver.Result{}, err
	}
	if _, err := s.db.Collection(collection).InsertOne(ctx, document); err != nil {
		return driver.Result{}, err
	}
	return driver.Result{RowsAffected: 1}, nil
}

// ReplaceDocument is whole-document replacement: the replacement must be a
// document whose _id is absent or BSON-equal to the requested identity; the
// requested _id is injected when absent. RowsAffected reports MatchedCount.
func (s *Service) ReplaceDocument(ctx context.Context, collection string, id driver.DocumentPayload, doc driver.DocumentPayload) (driver.Result, error) {
	identity, err := parseIdentity(id)
	if err != nil {
		return driver.Result{}, err
	}
	replacement, err := decodeDocument(doc)
	if err != nil {
		return driver.Result{}, err
	}
	if existing, found := docValue(replacement, "_id"); found {
		if !bsonEqual(existing, identity) {
			return driver.Result{}, errors.New("replacement _id does not match the requested identity")
		}
	} else {
		replacement = append(bson.D{{Key: "_id", Value: identity}}, replacement...)
	}
	result, err := s.db.Collection(collection).ReplaceOne(ctx, bson.D{{Key: "_id", Value: identity}}, replacement)
	if err != nil {
		return driver.Result{}, err
	}
	return driver.Result{RowsAffected: result.MatchedCount}, nil
}

// DeleteDocument removes the document identified by id.
func (s *Service) DeleteDocument(ctx context.Context, collection string, id driver.DocumentPayload) (driver.Result, error) {
	identity, err := parseIdentity(id)
	if err != nil {
		return driver.Result{}, err
	}
	result, err := s.db.Collection(collection).DeleteOne(ctx, bson.D{{Key: "_id", Value: identity}})
	if err != nil {
		return driver.Result{}, err
	}
	return driver.Result{RowsAffected: result.DeletedCount}, nil
}

// decodeDocument parses an extended-JSON document body, rejecting a
// mismatched format first. Arrays and scalars are rejected: UnmarshalExtJSON
// would otherwise coerce them into positional documents.
func decodeDocument(payload driver.DocumentPayload) (bson.D, error) {
	if payload.Format != driver.DocumentFormatMongoExtendedJSON {
		return nil, fmt.Errorf("unsupported document format %s", payload.Format)
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON(payload.Data, false, &doc); err != nil {
		return nil, fmt.Errorf("invalid document: %w", err)
	}
	return doc, nil
}

// parseIdentity decodes an extended-JSON scalar _id value. Identities are
// encoded as the bare wrapper content (wrappedExtJSON), so they are wrapped
// back into a one-key document for decoding; alias forms like
// {"$oid": "..."} then decode to their native BSON scalar. Nested documents
// and arrays are rejected as identities.
func parseIdentity(payload driver.DocumentPayload) (any, error) {
	if payload.Format != driver.DocumentFormatMongoExtendedJSON {
		return nil, fmt.Errorf("unsupported document format %s", payload.Format)
	}
	wrapped := make([]byte, 0, len(payload.Data)+10)
	wrapped = append(wrapped, `{"_id":`...)
	wrapped = append(wrapped, payload.Data...)
	wrapped = append(wrapped, '}')
	var doc bson.D
	if err := bson.UnmarshalExtJSON(wrapped, false, &doc); err != nil {
		return nil, fmt.Errorf("invalid document identity: %w", err)
	}
	value, ok := docValue(doc, "_id")
	if !ok {
		return nil, errors.New("document identity is empty")
	}
	switch value.(type) {
	case bson.D, bson.M, map[string]any, bson.A, []any:
		return nil, errors.New("document identity must be a scalar _id value")
	}
	return value, nil
}

// bsonEqual compares two decoded BSON values by Go equality, which
// preserves BSON type distinctions (int32 vs int64 vs float64).
func bsonEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}
