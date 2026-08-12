package sql

import (
	"context"
)

// Row-level and document-level write capability lives in this file so the
// durable contract — the serializable capability descriptor plus tagged
// request/response DTOs — has one home. `Service` itself does not change:
// drivers that can support row/document writes implement these optional
// interfaces, and the workbench discovers them through
// WriteCapabilitiesProvider or in-process type assertions. The same shapes
// marshal losslessly, so a future out-of-process plugin shim can map the
// DTOs below onto these Go interfaces without changing the workbench.

// DocumentFormat tags a document payload's serialization so a store's
// dialect can evolve without breaking the contract.
type DocumentFormat string

const (
	// DocumentFormatMongoExtendedJSON is MongoDB relaxed extended JSON,
	// mongoexport-compatible — what the mongodb driver already renders.
	DocumentFormatMongoExtendedJSON DocumentFormat = "application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed"
)

// DocumentPayload is a tagged payload: the store's declared format plus
// bytes. It carries both document bodies and row identities.
type DocumentPayload struct {
	Format DocumentFormat `json:"format"`
	Data   []byte         `json:"data"`
}

// DocumentWriteCapability declares a document store's editor contract:
// Format is the only payload format the driver accepts, and Text reports
// whether whole-document text editing is safe. A store that cannot replace
// a document safely must not advertise Text; it may still expose delete
// when browse results supply document identities.
type DocumentWriteCapability struct {
	Format DocumentFormat `json:"format"`
	Text   bool           `json:"text"`
}

// WriteCapabilities is the serializable capability descriptor, the durable
// plugin boundary. Compiled-in drivers are discovered in-process; plugins
// advertise the same descriptor and are wrapped by a shim.
type WriteCapabilities struct {
	RowWriter bool                     `json:"row_writer"`
	Document  *DocumentWriteCapability `json:"document,omitempty"`
}

// WriteCapabilitiesProvider is implemented by drivers that can describe
// their write capabilities without the workbench type-asserting internals.
type WriteCapabilitiesProvider interface {
	WriteCapabilities() WriteCapabilities
}

// RowWriter addresses a store as rows with a primary key (SQL tables;
// future CQL-style stores). Key values identify the row for update/delete.
type RowWriter interface {
	InsertRow(context.Context, string, []RowValue) (Result, error)
	UpdateRow(context.Context, string, []RowValue, []RowValue) (Result, error)
	DeleteRow(context.Context, string, []RowValue) (Result, error)
}

// DocumentReader loads one complete document by identity.
type DocumentReader interface {
	ReadDocument(context.Context, string, DocumentPayload) (DocumentPayload, error)
}

// DocumentWriter addresses a document store (MongoDB; future DynamoDB).
// Documents travel as tagged payloads; a second document store brings its
// own format constant rather than a contract change. ReplaceDocument is
// whole-document replacement: the driver translates the payload into its
// native replace, so mutation dialect stays driver-side.
type DocumentWriter interface {
	InsertDocument(context.Context, string, DocumentPayload) (Result, error)
	ReplaceDocument(context.Context, string, DocumentPayload, DocumentPayload) (Result, error)
	DeleteDocument(context.Context, string, DocumentPayload) (Result, error)
}

// ValueKind tags one RowValue payload. The kind is always serialized, so
// false, zero, and empty payloads keep distinct representations.
type ValueKind string

const (
	ValueDefault   ValueKind = "default"
	ValueNull      ValueKind = "null"
	ValueString    ValueKind = "string"
	ValueBool      ValueKind = "bool"
	ValueInteger   ValueKind = "integer"
	ValueFloat     ValueKind = "float"
	ValueBytes     ValueKind = "bytes"
	ValueDecimal   ValueKind = "decimal"
	ValueTimestamp ValueKind = "timestamp"
	ValueArray     ValueKind = "array"
	ValueObject    ValueKind = "object"
)

// Value is one tagged cell payload. Exactly the payload matching Kind is
// meaningful: ValueDefault and ValueNull carry none, ValueString carries
// String, ValueDecimal/ValueTimestamp carry exact text, and recursive
// kinds carry Array/Object.
type Value struct {
	Kind      ValueKind    `json:"kind"`
	String    string       `json:"string,omitempty"`
	Bool      bool         `json:"bool,omitempty"`
	Integer   int64        `json:"integer,omitempty"`
	Float     float64      `json:"float,omitempty"`
	Bytes     []byte       `json:"bytes,omitempty"`
	Decimal   string       `json:"decimal,omitempty"`
	Timestamp string       `json:"timestamp,omitempty"` // RFC 3339
	Array     []Value      `json:"array,omitempty"`
	Object    []NamedValue `json:"object,omitempty"`
}

// NamedValue is one key/value pair of a ValueObject.
type NamedValue struct {
	Name  string `json:"name"`
	Value Value  `json:"value"`
}

// RowValue is one column of a row write: the column name plus its tagged
// value. Ordering is the caller's ordering; drivers preserve it when
// constructing parameter lists.
type RowValue struct {
	Name  string `json:"name"`
	Value Value  `json:"value"`
}

// Plugin wire DTOs. These are the durable boundary for a future
// out-of-process plugin shim: it marshals requests across the process and
// unmarshals responses, mapping them onto the Go interfaces above. The
// workbench never type-asserts across a process; compiled-in adapters
// return Result{RowsAffected: …} and never use WriteResult.

// RowWriteOperation names a row-write request.
type RowWriteOperation string

const (
	RowWriteInsert RowWriteOperation = "insert"
	RowWriteUpdate RowWriteOperation = "update"
	RowWriteDelete RowWriteOperation = "delete"
)

// RowWriteRequest is the wire form of a RowWriter call. Key carries the
// row identity for update/delete; Values carries the insert/update payload.
type RowWriteRequest struct {
	Operation RowWriteOperation `json:"operation"`
	Table     string            `json:"table"`
	Key       []RowValue        `json:"key,omitempty"`
	Values    []RowValue        `json:"values,omitempty"`
}

// WriteResult is the wire-only result envelope; compiled-in adapters return
// Result{RowsAffected: …} instead.
type WriteResult struct {
	RowsAffected int64 `json:"rows_affected"`
}

// RowWriteResponse is the wire response to a RowWriteRequest.
type RowWriteResponse struct {
	Result WriteResult `json:"result"`
}

// DocumentWriteOperation names a document-write request.
type DocumentWriteOperation string

const (
	DocumentWriteRead    DocumentWriteOperation = "read"
	DocumentWriteInsert  DocumentWriteOperation = "insert"
	DocumentWriteReplace DocumentWriteOperation = "replace"
	DocumentWriteDelete  DocumentWriteOperation = "delete"
)

// DocumentWriteRequest is the wire form of a DocumentReader/DocumentWriter
// call. ID carries the document identity for read/replace/delete; Document
// carries the body for insert/replace.
type DocumentWriteRequest struct {
	Operation  DocumentWriteOperation `json:"operation"`
	Collection string                 `json:"collection"`
	ID         *DocumentPayload       `json:"id,omitempty"`
	Document   *DocumentPayload       `json:"document,omitempty"`
}

// DocumentWriteResponse is the wire response to a DocumentWriteRequest;
// Document is set for read.
type DocumentWriteResponse struct {
	Result   WriteResult      `json:"result"`
	Document *DocumentPayload `json:"document,omitempty"`
}
