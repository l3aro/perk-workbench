package plugin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// shim is the database.Shim face of one plugin: the capabilities the
// plugin advertised at handshake, plus the target builder and service
// opener bridged over the wire. The client pointer swaps atomically on
// Restart: sessions opened before the swap keep their old generation,
// sessions opened after it use the replacement.
type shim struct {
	client atomic.Pointer[Client]
	caps   database.Capabilities
	loader *Loader
}

// newShim builds a shim bound to client.
func newShim(client *Client, caps database.Capabilities, loader *Loader) *shim {
	s := &shim{caps: caps, loader: loader}
	s.client.Store(client)
	return s
}

func (s *shim) Capabilities() database.Capabilities { return s.caps }

// BuildTarget serializes connection-form values through the plugin,
// bounded by a background 5-second timeout (this interface has no
// context).
func (s *shim) BuildTarget(values database.FormValues) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), shortCallTimeout)
	defer cancel()
	var result buildTargetResult
	if err := s.client.Load().Call(ctx, methodBuildTarget, values, &result); err != nil {
		return "", false
	}
	return result.Target, result.OK
}

// Open opens a session in the current plugin generation, tracks it with
// the loader, and returns the capability-wrapped service. The client
// generation is pinned per call: a Restart swapping mid-open can never
// bind a session id to the wrong child.
func (s *shim) Open(ctx context.Context, target string) (sharedsql.Service, error) {
	client := s.client.Load()
	var result openResult
	if err := client.Call(ctx, methodOpen, openParams{Target: target}, &result); err != nil {
		return nil, err
	}
	proxy := &sessionProxy{
		client:    client,
		loader:    s.loader,
		sessionID: result.SessionID,
		info:      result.Info,
	}
	s.loader.trackSession(proxy)
	return wrapService(proxy, s.caps), nil
}

var _ sharedsql.Service = (*sessionProxy)(nil)

// sessionProxy is the in-process face of one plugin session; every
// sql.Service method is forwarded over the wire with the caller's
// context unchanged.
type sessionProxy struct {
	client    *Client
	loader    *Loader
	sessionID uint64
	info      sharedsql.DatabaseInfo
	closeOnce sync.Once
}

// Close is idempotent: the first call sends the perk/v1/close RPC under a
// background 5-second timeout and unregisters the session from the
// loader; later calls return nil.
func (p *sessionProxy) Close() error {
	var err error
	p.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), shortCallTimeout)
		defer cancel()
		err = p.client.Call(ctx, methodClose, sessionParams{SessionID: p.sessionID}, &struct{}{})
		p.loader.unregisterSession(p)
	})
	return err
}

// Info returns the session metadata cached from open; never an RPC.
func (p *sessionProxy) Info() sharedsql.DatabaseInfo { return p.info }

func (p *sessionProxy) call(ctx context.Context, method string, params any, result any) error {
	return p.client.Call(ctx, method, params, result)
}

func (p *sessionProxy) Execute(ctx context.Context, statement string) (sharedsql.Result, error) {
	var result sharedsql.Result
	err := p.call(ctx, methodExecute, statementParams{SessionID: p.sessionID, Statement: statement}, &result)
	if err != nil {
		return result, err
	}
	return result, checkStatementMetadata(methodExecute, result.Statement, result.StatementMetadata)
}

func (p *sessionProxy) ExecuteReadOnly(ctx context.Context, statement string) (sharedsql.Result, error) {
	var result sharedsql.Result
	err := p.call(ctx, methodExecuteReadOnly, statementParams{SessionID: p.sessionID, Statement: statement}, &result)
	if err != nil {
		return result, err
	}
	return result, checkStatementMetadata(methodExecuteReadOnly, result.Statement, result.StatementMetadata)
}

func (p *sessionProxy) Validate(ctx context.Context, statement string) error {
	return p.call(ctx, methodValidate, statementParams{SessionID: p.sessionID, Statement: statement}, &struct{}{})
}

func (p *sessionProxy) ListSchema(ctx context.Context) ([]sharedsql.SchemaObject, error) {
	var result []sharedsql.SchemaObject
	err := p.call(ctx, methodListSchema, sessionParams{SessionID: p.sessionID}, &result)
	if err != nil {
		return nil, err
	}
	return normalizeSchema(result), nil
}

// normalizeSchema adapts a perk/v1 list_schema response to the stricter
// internal representation: the sidebar tree renders roots only from
// Type == "database", while plugins may return flat objects (the wire
// shape docs/plugins.md permits). For every distinct non-empty Database
// without an explicit Type == "database" object for that same Database,
// one root {Database: database, Type: "database", Name: database,
// RowCount: nil} is synthesized. Synthetic roots are prepended in
// first-seen database order; every plugin-provided object is preserved
// with its relative order, explicit roots never duplicated or replaced,
// and objects with an empty Database left untouched.
func normalizeSchema(objects []sharedsql.SchemaObject) []sharedsql.SchemaObject {
	explicitRoot := make(map[string]bool)
	for _, object := range objects {
		if object.Database != "" && object.Type == "database" {
			explicitRoot[object.Database] = true
		}
	}
	var roots []sharedsql.SchemaObject
	seen := make(map[string]bool)
	for _, object := range objects {
		database := object.Database
		if database == "" || explicitRoot[database] || seen[database] {
			continue
		}
		seen[database] = true
		roots = append(roots, sharedsql.SchemaObject{Database: database, Type: "database", Name: database})
	}
	if len(roots) == 0 {
		return objects
	}
	normalized := make([]sharedsql.SchemaObject, 0, len(roots)+len(objects))
	normalized = append(normalized, roots...)
	normalized = append(normalized, objects...)
	return normalized
}

func (p *sessionProxy) TableInfo(ctx context.Context, table string) ([]sharedsql.ColumnInfo, error) {
	var result []sharedsql.ColumnInfo
	err := p.call(ctx, methodTableInfo, tableParams{SessionID: p.sessionID, Table: table}, &result)
	return result, err
}

func (p *sessionProxy) ListIndexes(ctx context.Context, table string) ([]sharedsql.IndexInfo, error) {
	var result []sharedsql.IndexInfo
	err := p.call(ctx, methodListIndexes, tableParams{SessionID: p.sessionID, Table: table}, &result)
	return result, err
}

func (p *sessionProxy) CreateIndex(ctx context.Context, table string, change sharedsql.IndexChange) error {
	return p.call(ctx, methodCreateIndex, indexChangeParams{SessionID: p.sessionID, Table: table, Change: change}, &struct{}{})
}

func (p *sessionProxy) ReplaceIndex(ctx context.Context, table, oldName string, change sharedsql.IndexChange) error {
	return p.call(ctx, methodReplaceIndex, replaceIndexParams{SessionID: p.sessionID, Table: table, OldName: oldName, Change: change}, &struct{}{})
}

func (p *sessionProxy) DropIndex(ctx context.Context, table, name string) error {
	return p.call(ctx, methodDropIndex, dropParams{SessionID: p.sessionID, Table: table, Name: name}, &struct{}{})
}

func (p *sessionProxy) ListForeignKeys(ctx context.Context, table string) ([]sharedsql.ForeignKeyInfo, error) {
	var result []sharedsql.ForeignKeyInfo
	err := p.call(ctx, methodListForeignKeys, tableParams{SessionID: p.sessionID, Table: table}, &result)
	return result, err
}

func (p *sessionProxy) ListReferencingForeignKeys(ctx context.Context, table string) ([]sharedsql.ReferencingForeignKeyInfo, error) {
	var result []sharedsql.ReferencingForeignKeyInfo
	err := p.call(ctx, methodListReferencingForeignKeys, tableParams{SessionID: p.sessionID, Table: table}, &result)
	return result, err
}

func (p *sessionProxy) ListForeignKeysAll(ctx context.Context) (map[string][]sharedsql.ForeignKeyInfo, error) {
	var result map[string][]sharedsql.ForeignKeyInfo
	err := p.call(ctx, methodListForeignKeysAll, sessionParams{SessionID: p.sessionID}, &result)
	return result, err
}

func (p *sessionProxy) ListIndexesAll(ctx context.Context) (map[string][]sharedsql.IndexInfo, error) {
	var result map[string][]sharedsql.IndexInfo
	err := p.call(ctx, methodListIndexesAll, sessionParams{SessionID: p.sessionID}, &result)
	return result, err
}

func (p *sessionProxy) CreateForeignKey(ctx context.Context, table string, change sharedsql.ForeignKeyChange) error {
	return p.call(ctx, methodCreateForeignKey, foreignKeyChangeParams{SessionID: p.sessionID, Table: table, Change: change}, &struct{}{})
}

func (p *sessionProxy) ReplaceForeignKey(ctx context.Context, table, oldName string, change sharedsql.ForeignKeyChange) error {
	return p.call(ctx, methodReplaceForeignKey, replaceForeignKeyParams{SessionID: p.sessionID, Table: table, OldName: oldName, Change: change}, &struct{}{})
}

func (p *sessionProxy) DropForeignKey(ctx context.Context, table, name string) error {
	return p.call(ctx, methodDropForeignKey, dropParams{SessionID: p.sessionID, Table: table, Name: name}, &struct{}{})
}

func (p *sessionProxy) AlterColumn(ctx context.Context, table string, change sharedsql.ColumnChange) error {
	return p.call(ctx, methodAlterColumn, columnChangeParams{SessionID: p.sessionID, Table: table, Change: change}, &struct{}{})
}

func (p *sessionProxy) DropColumn(ctx context.Context, table, name string) error {
	return p.call(ctx, methodDropColumn, dropParams{SessionID: p.sessionID, Table: table, Name: name}, &struct{}{})
}

func (p *sessionProxy) AddColumn(ctx context.Context, table string, def sharedsql.ColumnDef) error {
	return p.call(ctx, methodAddColumn, addColumnParams{SessionID: p.sessionID, Table: table, Def: def}, &struct{}{})
}

func (p *sessionProxy) BrowseTable(ctx context.Context, table string, options sharedsql.BrowseOptions) (sharedsql.Result, error) {
	var result sharedsql.Result
	err := p.call(ctx, methodBrowseTable, browseParams{SessionID: p.sessionID, Table: table, Options: options}, &result)
	if err != nil {
		return result, err
	}
	return result, checkStatementMetadata(methodBrowseTable, result.Statement, result.StatementMetadata)
}

// checkStatementMetadata rejects orphan statement metadata at the plugin
// boundary: metadata is meaningful only when the accompanying statement
// is nonblank, so a result carrying metadata without a statement is a
// result-shape violation — an operation error, never terminal.
func checkStatementMetadata(method, statement string, metadata *sharedsql.StatementMetadata) error {
	if metadata != nil && strings.TrimSpace(statement) == "" {
		return fmt.Errorf("%s: statement_metadata requires a nonblank statement", method)
	}
	return nil
}

// resultFromWrite maps a wire WriteResult onto a shared Result, carrying
// the optional native statement and its metadata. Orphan metadata
// (metadata without a statement) is rejected like any other result-shape
// violation.
func resultFromWrite(method string, write sharedsql.WriteResult) (sharedsql.Result, error) {
	if err := checkStatementMetadata(method, write.Statement, write.StatementMetadata); err != nil {
		return sharedsql.Result{}, err
	}
	return sharedsql.Result{
		RowsAffected:      write.RowsAffected,
		Statement:         write.Statement,
		StatementMetadata: write.StatementMetadata,
	}, nil
}

// wrapService layers the capability wrappers over the session proxy so
// optional-interface discovery — sql.RowWriter,
// sql.DocumentReader/DocumentWriter, WriteCapabilitiesProvider,
// sql.WorkspaceViewProvider — mirrors the plugin's advertised
// capabilities. With no write capabilities and no workspace views the
// raw proxy is returned (it still satisfies sql.Service). When both
// write capabilities are present, the document layer wraps the row
// layer so the returned service satisfies every interface at once; the
// workspace-view layer wraps whatever write shape preceded it.
func wrapService(proxy *sessionProxy, caps database.Capabilities) sharedsql.Service {
	row := caps.WriteCapabilities.RowWriter
	document := caps.WriteCapabilities.Document != nil
	var service sharedsql.Service
	switch {
	case row && document:
		service = &documentRowWriter{rowWriter: &rowWriter{Service: proxy, proxy: proxy, caps: caps}}
	case row:
		service = &rowWriter{Service: proxy, proxy: proxy, caps: caps}
	case document:
		service = &documentWriter{Service: proxy, proxy: proxy, caps: caps}
	default:
		service = proxy
	}
	if caps.Workspace != nil && len(caps.Workspace.CustomViews) > 0 {
		service = &workspaceViewer{Service: service, proxy: proxy}
	}
	return service
}

var (
	_ sharedsql.Service                   = (*rowWriter)(nil)
	_ sharedsql.RowWriter                 = (*rowWriter)(nil)
	_ sharedsql.WriteCapabilitiesProvider = (*rowWriter)(nil)
	_ sharedsql.Service                   = (*documentWriter)(nil)
	_ sharedsql.DocumentReader            = (*documentWriter)(nil)
	_ sharedsql.DocumentWriter            = (*documentWriter)(nil)
	_ sharedsql.WriteCapabilitiesProvider = (*documentWriter)(nil)
	_ sharedsql.Service                   = (*documentRowWriter)(nil)
	_ sharedsql.RowWriter                 = (*documentRowWriter)(nil)
	_ sharedsql.DocumentReader            = (*documentRowWriter)(nil)
	_ sharedsql.DocumentWriter            = (*documentRowWriter)(nil)
	_ sharedsql.WriteCapabilitiesProvider = (*documentRowWriter)(nil)
	_ sharedsql.Service                   = (*workspaceViewer)(nil)
	_ sharedsql.WorkspaceViewProvider     = (*workspaceViewer)(nil)
)

// workspaceViewer layers sql.WorkspaceViewProvider over a session for
// plugins that advertise custom workspace views. A plugin that does not
// advertise custom views never gets the wrapper: its sessions expose
// only sql.Service, so the workbench never sends a perk/v1/workspace_view
// request it cannot answer. A plugin that advertises custom views but
// fails to answer the method gets an operation error (method not found),
// surfaced in the view's error state — the advertisement mismatch is
// never silent.
type workspaceViewer struct {
	sharedsql.Service
	proxy *sessionProxy
}

func (w *workspaceViewer) WorkspaceView(ctx context.Context, request sharedsql.WorkspaceViewRequest) (sharedsql.Result, error) {
	var result sharedsql.Result
	err := w.proxy.call(ctx, methodWorkspaceView, workspaceViewParams{
		SessionID: w.proxy.sessionID,
		ViewID:    request.ViewID,
		Target:    request.Target,
	}, &result)
	if err != nil {
		return result, err
	}
	return result, checkStatementMetadata(methodWorkspaceView, result.Statement, result.StatementMetadata)
}

// rowWriter layers sql.RowWriter over a session for row-write-capable
// plugins.
type rowWriter struct {
	sharedsql.Service
	proxy *sessionProxy
	caps  database.Capabilities
}

func (w *rowWriter) WriteCapabilities() sharedsql.WriteCapabilities {
	return w.caps.WriteCapabilities
}

func (w *rowWriter) InsertRow(ctx context.Context, table string, values []sharedsql.RowValue) (sharedsql.Result, error) {
	var response sharedsql.RowWriteResponse
	err := w.proxy.call(ctx, methodRowWrite, rowWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.RowWriteRequest{Operation: sharedsql.RowWriteInsert, Table: table, Values: values},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodRowWrite, response.Result)
}

func (w *rowWriter) UpdateRow(ctx context.Context, table string, key, values []sharedsql.RowValue) (sharedsql.Result, error) {
	var response sharedsql.RowWriteResponse
	err := w.proxy.call(ctx, methodRowWrite, rowWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.RowWriteRequest{Operation: sharedsql.RowWriteUpdate, Table: table, Key: key, Values: values},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodRowWrite, response.Result)
}

func (w *rowWriter) DeleteRow(ctx context.Context, table string, key []sharedsql.RowValue) (sharedsql.Result, error) {
	var response sharedsql.RowWriteResponse
	err := w.proxy.call(ctx, methodRowWrite, rowWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.RowWriteRequest{Operation: sharedsql.RowWriteDelete, Table: table, Key: key},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodRowWrite, response.Result)
}

// documentWriter layers sql.DocumentReader and sql.DocumentWriter over a
// session for document-capable plugins.
type documentWriter struct {
	sharedsql.Service
	proxy *sessionProxy
	caps  database.Capabilities
}

func (w *documentWriter) WriteCapabilities() sharedsql.WriteCapabilities {
	return w.caps.WriteCapabilities
}

func (w *documentWriter) ReadDocument(ctx context.Context, collection string, id sharedsql.DocumentPayload) (sharedsql.DocumentPayload, error) {
	var response sharedsql.DocumentWriteResponse
	err := w.proxy.call(ctx, methodDocumentWrite, documentWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.DocumentWriteRequest{Operation: sharedsql.DocumentWriteRead, Collection: collection, ID: &id},
	}, &response)
	if err != nil {
		return sharedsql.DocumentPayload{}, err
	}
	if response.Document == nil {
		return sharedsql.DocumentPayload{}, fmt.Errorf("perk/v1/document_write: read returned no document")
	}
	return *response.Document, nil
}

func (w *documentWriter) InsertDocument(ctx context.Context, collection string, document sharedsql.DocumentPayload) (sharedsql.Result, error) {
	var response sharedsql.DocumentWriteResponse
	err := w.proxy.call(ctx, methodDocumentWrite, documentWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.DocumentWriteRequest{Operation: sharedsql.DocumentWriteInsert, Collection: collection, Document: &document},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodDocumentWrite, response.Result)
}

func (w *documentWriter) ReplaceDocument(ctx context.Context, collection string, id, document sharedsql.DocumentPayload) (sharedsql.Result, error) {
	var response sharedsql.DocumentWriteResponse
	err := w.proxy.call(ctx, methodDocumentWrite, documentWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.DocumentWriteRequest{Operation: sharedsql.DocumentWriteReplace, Collection: collection, ID: &id, Document: &document},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodDocumentWrite, response.Result)
}

func (w *documentWriter) DeleteDocument(ctx context.Context, collection string, id sharedsql.DocumentPayload) (sharedsql.Result, error) {
	var response sharedsql.DocumentWriteResponse
	err := w.proxy.call(ctx, methodDocumentWrite, documentWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.DocumentWriteRequest{Operation: sharedsql.DocumentWriteDelete, Collection: collection, ID: &id},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodDocumentWrite, response.Result)
}

// documentRowWriter is the combined wrapper for plugins advertising both
// row and document writes: the embedded rowWriter layer provides
// sql.Service, sql.RowWriter, and WriteCapabilitiesProvider, and this
// type adds sql.DocumentReader and sql.DocumentWriter, so one service
// satisfies every optional interface at once.
type documentRowWriter struct {
	*rowWriter
}

func (w *documentRowWriter) ReadDocument(ctx context.Context, collection string, id sharedsql.DocumentPayload) (sharedsql.DocumentPayload, error) {
	var response sharedsql.DocumentWriteResponse
	err := w.proxy.call(ctx, methodDocumentWrite, documentWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.DocumentWriteRequest{Operation: sharedsql.DocumentWriteRead, Collection: collection, ID: &id},
	}, &response)
	if err != nil {
		return sharedsql.DocumentPayload{}, err
	}
	if response.Document == nil {
		return sharedsql.DocumentPayload{}, fmt.Errorf("perk/v1/document_write: read returned no document")
	}
	return *response.Document, nil
}

func (w *documentRowWriter) InsertDocument(ctx context.Context, collection string, document sharedsql.DocumentPayload) (sharedsql.Result, error) {
	var response sharedsql.DocumentWriteResponse
	err := w.proxy.call(ctx, methodDocumentWrite, documentWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.DocumentWriteRequest{Operation: sharedsql.DocumentWriteInsert, Collection: collection, Document: &document},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodDocumentWrite, response.Result)
}

func (w *documentRowWriter) ReplaceDocument(ctx context.Context, collection string, id, document sharedsql.DocumentPayload) (sharedsql.Result, error) {
	var response sharedsql.DocumentWriteResponse
	err := w.proxy.call(ctx, methodDocumentWrite, documentWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.DocumentWriteRequest{Operation: sharedsql.DocumentWriteReplace, Collection: collection, ID: &id, Document: &document},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodDocumentWrite, response.Result)
}

func (w *documentRowWriter) DeleteDocument(ctx context.Context, collection string, id sharedsql.DocumentPayload) (sharedsql.Result, error) {
	var response sharedsql.DocumentWriteResponse
	err := w.proxy.call(ctx, methodDocumentWrite, documentWriteParams{
		SessionID: w.proxy.sessionID,
		Request:   sharedsql.DocumentWriteRequest{Operation: sharedsql.DocumentWriteDelete, Collection: collection, ID: &id},
	}, &response)
	if err != nil {
		return sharedsql.Result{}, err
	}
	return resultFromWrite(methodDocumentWrite, response.Result)
}
