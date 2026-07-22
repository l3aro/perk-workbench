package core

import (
	"context"
	"strings"

	sharedsql "github.com/l3aro/perk/internal/sql"
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
)

type Tab uint8

const (
	TabStructure Tab = iota
	TabBrowse
	TabSQL
	TabIndexes
)

type Query struct {
	RequestID uint64
	Context   context.Context
	Service   sharedsql.Service
	Statement string
}

type Workflow struct {
	State         State
	Focus         Focus
	Tab           Tab
	Target        string
	Status        string
	Database      sharedsql.Service
	SelectedTable string
	BrowsePage    int

	requestID       uint64
	activeRequestID uint64
	running         bool
	cancelRequested bool
	pendingQuit     bool
	cancel          context.CancelFunc
}

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

func (w *Workflow) RestoreQuery(ctx context.Context, requestID uint64, canceled bool) {
	queryContext, cancel := context.WithCancel(ctx)
	w.requestID, w.activeRequestID = requestID, requestID
	w.running, w.cancelRequested, w.cancel = true, canceled, cancel
	_ = queryContext
}

func (w *Workflow) CancelQuery() {
	if w.cancelRequested || !w.running {
		return
	}
	w.cancelRequested = true
	w.Status = "canceling query"
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
}

func (w *Workflow) Opened(target string, service sharedsql.Service, status string) {
	w.Target, w.Database, w.State, w.Status = target, service, StateReady, status
}

func (w *Workflow) Fail(status string) {
	w.State, w.Status = StateFailure, status
}

func (w *Workflow) RecoverToPicker(status string) {
	w.State, w.Status = StatePicking, status
}

func (w *Workflow) SelectTable(table string) {
	w.SelectedTable, w.BrowsePage, w.Tab, w.Focus = table, 0, TabStructure, FocusWorkspace
}

func (w *Workflow) ChangeBrowsePage(delta int) bool {
	next := w.BrowsePage + delta
	if next < 0 {
		return false
	}
	w.BrowsePage = next
	return true
}

func (w *Workflow) ToggleTab(forward bool) {
	if forward {
		w.Tab = (w.Tab + 1) % 4
		return
	}
	w.Tab = (w.Tab + 3) % 4
}
