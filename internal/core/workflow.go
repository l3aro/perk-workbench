package core

import (
	"context"
	"strings"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

const BrowsePageSize = 25

type State uint8

const (
	StateConnection State = iota
	StatePicking
	StateOpening
	StateReady
	StateFailure
)

type Focus uint8

const (
	FocusSchema Focus = iota
	FocusWorkspace
	FocusQueryLog
	FocusChat
)

type Tab uint8

const (
	TabSQL Tab = iota
	TabBrowse
	TabStructure
	TabIndexes
	TabForeignKeys
	TabDiagram
)

// WorkspaceTargetKind is the sidebar scope the workspace tabs serve.
type WorkspaceTargetKind uint8

const (
	WorkspaceNone WorkspaceTargetKind = iota
	WorkspaceDatabase
	WorkspaceSchema
	WorkspaceTable
)

// WorkspaceTarget is the workspace's active scope: no selection, a
// database, a PostgreSQL schema, or a qualified table/collection. The tab
// policy derives the visible tabs from it; SelectedTable mirrors the table
// kind for the existing table features.
type WorkspaceTarget struct {
	Kind     WorkspaceTargetKind
	Database string
	Schema   string
	Table    string
}

type Query struct {
	RequestID uint64
	Context   context.Context
	Service   sharedsql.Service
	Statement string
}

type Workflow struct {
	State           State
	Focus           Focus
	Tab             Tab
	Target          string
	Status          string
	ReadOnly        bool
	Database        sharedsql.Service
	SelectedTable   string
	WorkspaceTarget WorkspaceTarget
	BrowsePage      int

	requestID       uint64
	activeRequestID uint64
	running         bool
	cancelRequested bool
	pendingQuit     bool
	cancel          context.CancelFunc
	statusRevision  uint64
}

// StatusRevision returns a counter incremented on every Status write through
// the workflow methods, so callers can observe a status event even when the
// text is identical to the previous one.
func (w *Workflow) StatusRevision() uint64 { return w.statusRevision }

func New(target string) Workflow {
	workflow := Workflow{Target: target, Focus: FocusWorkspace, Tab: TabSQL}
	if target == "" {
		workflow.State = StateConnection
	} else {
		workflow.State = StateOpening
	}
	return workflow
}

func (w *Workflow) StartQuery(ctx context.Context, statement string) (Query, bool) {
	statement = strings.TrimSpace(statement)
	if statement == "" || w.running || w.Database == nil {
		return Query{}, false
	}
	w.requestID++
	w.activeRequestID = w.requestID
	w.running, w.cancelRequested = true, false
	queryContext, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.Status = "running query"
	w.statusRevision++
	return Query{RequestID: w.activeRequestID, Context: queryContext, Service: w.Database, Statement: statement}, true
}

func (w *Workflow) Running() bool { return w.running }

func (w *Workflow) StartQueryForTest(ctx context.Context) uint64 {
	query, ok := w.StartQuery(ctx, "SELECT 1")
	if !ok {
		return 0
	}
	return query.RequestID
}

func (w *Workflow) CancelQuery() {
	if w.cancelRequested || !w.running {
		return
	}
	w.cancelRequested = true
	w.Status = "canceling query"
	w.statusRevision++
	w.cancel()
}

func (w *Workflow) RequestQuit() { w.pendingQuit = true }

func (w *Workflow) MatchesQuery(requestID uint64) bool {
	return w.running && w.activeRequestID == requestID
}

func (w *Workflow) FinishQuery() (canceled, quit bool) {
	canceled, quit = w.cancelRequested, w.pendingQuit
	if w.cancel != nil {
		w.cancel()
	}
	w.running, w.cancelRequested, w.pendingQuit, w.cancel = false, false, false, nil
	return canceled, quit
}

func (w *Workflow) BeginOpening(target, status string) {
	w.Target, w.State, w.Status = target, StateOpening, status
	w.statusRevision++
}

func (w *Workflow) Opened(target string, service sharedsql.Service, status string) {
	w.Target, w.Database, w.State, w.Status = target, service, StateReady, status
	w.statusRevision++
}

func (w *Workflow) Fail(status string) {
	w.State, w.Status = StateFailure, status
	w.statusRevision++
}

func (w *Workflow) RecoverToPicker(status string) {
	w.State, w.Status = StatePicking, status
	w.statusRevision++
}

// SelectTable opens the qualified table in the workspace: the table target
// is set, SelectedTable retained for the existing table features, the
// browse page reset, and the workspace focused on the structure tab.
func (w *Workflow) SelectTable(table string) {
	w.SelectedTable, w.BrowsePage, w.Tab, w.Focus = table, 0, TabStructure, FocusWorkspace
	w.WorkspaceTarget = WorkspaceTarget{Kind: WorkspaceTable, Table: table}
}

// SelectDatabase opens a database scope: the table selection is cleared,
// the target set, and the workspace lands on the Browse tab.
func (w *Workflow) SelectDatabase(database string) {
	w.SelectedTable, w.BrowsePage, w.Tab, w.Focus = "", 0, TabBrowse, FocusWorkspace
	w.WorkspaceTarget = WorkspaceTarget{Kind: WorkspaceDatabase, Database: database}
}

// SelectSchema opens a schema scope: the table selection is cleared, the
// target set, and the workspace lands on the Browse tab.
func (w *Workflow) SelectSchema(database, schema string) {
	w.SelectedTable, w.BrowsePage, w.Tab, w.Focus = "", 0, TabBrowse, FocusWorkspace
	w.WorkspaceTarget = WorkspaceTarget{Kind: WorkspaceSchema, Database: database, Schema: schema}
}

func (w *Workflow) ChangeBrowsePage(delta int) bool {
	next := w.BrowsePage + delta
	if next < 0 {
		return false
	}
	w.BrowsePage = next
	return true
}
