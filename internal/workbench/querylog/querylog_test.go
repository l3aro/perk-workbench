package querylog

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

func TestStore_persistsAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := Entry{StartedAt: time.Now(), Statement: "SELECT current", Duration: time.Millisecond, Message: "completed", Status: "success"}
	if err := store.Append("conn-a", entry, 100); err != nil {
		t.Fatal(err)
	}
	// An expired row must be pruned on the next load.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO query_log(connection_id, started_at, statement, duration, message, status) VALUES (?, ?, ?, ?, ?, ?)`,
		"conn-a", time.Now().AddDate(0, 0, -2).UnixNano(), "SELECT expired", 0, "completed", "success"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("conn-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Statement != entry.Statement {
		t.Fatalf("query log = %#v, want %#v", got, []Entry{entry})
	}
}

func TestStore_scopesEntriesByConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	aEntry := Entry{StartedAt: time.Now(), Statement: "SELECT for A", Duration: time.Millisecond, Message: "completed", Status: "success"}
	bEntry := Entry{StartedAt: time.Now(), Statement: "SELECT for B", Duration: time.Millisecond, Message: "completed", Status: "success"}
	if err := store.Append("conn-a", aEntry, 100); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("conn-b", bEntry, 100); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("conn-a", Entry{StartedAt: time.Now(), Statement: "SELECT for A again", Duration: time.Millisecond, Message: "completed", Status: "success"}, 100); err != nil {
		t.Fatal(err)
	}

	gotA, err := store.Load("conn-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotA) != 2 || gotA[0].Statement != "SELECT for A again" || gotA[1].Statement != "SELECT for A" {
		t.Fatalf("scope conn-a entries = %#v, want the two A entries newest first", gotA)
	}
	gotB, err := store.Load("conn-b", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotB) != 1 || gotB[0].Statement != "SELECT for B" {
		t.Fatalf("scope conn-b entries = %#v, want only B's entry", gotB)
	}
}

func TestStore_emptyScopeIsRejected(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got, err := store.Load("", 100); err != nil || len(got) != 0 {
		t.Fatalf("Load(\"\") = %#v/%v, want no entries and no error", got, err)
	}
	if err := store.Append("", Entry{Statement: "SELECT unscoped"}, 100); err == nil {
		t.Fatal("Append(\"\") succeeded, want a scope error")
	}
}

func TestStore_migratesLegacyRowsIntoGeneratedScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the pre-scope schema: query_log without connection_id.
	if _, err := db.Exec(`CREATE TABLE query_log (id INTEGER PRIMARY KEY, started_at INTEGER NOT NULL, statement TEXT NOT NULL, duration INTEGER NOT NULL, message TEXT NOT NULL, status TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO query_log (started_at, statement, duration, message, status) VALUES (?, 'SELECT legacy', 0, 'completed', 'success')`, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// A fresh connection scope must not see the legacy row.
	got, err := store.Load("fresh-scope", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("fresh scope entries = %#v, want none", got)
	}

	// The legacy row landed in exactly one nonempty generated scope.
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT DISTINCT connection_id FROM query_log`)
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0] == "" {
		t.Fatalf("legacy scopes = %#v, want exactly one nonempty scope", scopes)
	}
	if !profile.ValidID(scopes[0]) {
		t.Fatalf("legacy scope = %q, want a generated UUIDv7", scopes[0])
	}
	if got, err := store.Load(scopes[0], 100); err != nil || len(got) != 1 || got[0].Statement != "SELECT legacy" {
		t.Fatalf("legacy scope entries = %#v/%v, want the legacy statement", got, err)
	}
}

func TestStore_importsSavedQueriesIntoQuarantineScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the legacy saved_queries table alongside a scoped query_log.
	if _, err := db.Exec(`CREATE TABLE saved_queries (id INTEGER PRIMARY KEY, statement TEXT NOT NULL, saved_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO saved_queries (statement, saved_at) VALUES (?, ?)`, "SELECT saved", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE query_log (
		id INTEGER PRIMARY KEY,
		connection_id TEXT NOT NULL DEFAULT '',
		started_at INTEGER NOT NULL,
		statement TEXT NOT NULL,
		duration INTEGER NOT NULL,
		message TEXT NOT NULL,
		status TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// The import lands in one generated quarantine scope, never a connection's.
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT DISTINCT connection_id FROM query_log`)
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || !profile.ValidID(scopes[0]) {
		t.Fatalf("import scopes = %#v, want one generated UUIDv7 scope", scopes)
	}
	got, err := store.Load(scopes[0], 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Statement != "SELECT saved" || got[0].Message != "saved query" {
		t.Fatalf("imported entries = %#v, want the saved query", got)
	}
	// The one-time import drops its source table.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'saved_queries'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("saved_queries table survived the import")
	}
}

func TestStore_sharedDBWaitsForConcurrentWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := Entry{StartedAt: time.Now(), Statement: "SELECT concurrent", Duration: time.Millisecond, Message: "completed", Status: "success"}
	blocker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	conn, err := blocker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	saved := make(chan error, 1)
	go func() {
		close(started)
		saved <- store.Append("conn-a", entry, 100)
	}()
	<-started
	time.Sleep(200 * time.Millisecond) // let the append block on the write lock
	if _, err := conn.ExecContext(context.Background(), `COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err := <-saved; err != nil {
		t.Fatalf("Append failed under concurrent writer: %v", err)
	}
	if got, err := store.Load("conn-a", 100); err != nil || len(got) != 1 || got[0].Statement != entry.Statement {
		t.Fatalf("query log = %#v/%v, want concurrent entry", got, err)
	}
}

func TestStore_appendsCapAtLimit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const limit = 3
	for i := 0; i < 5; i++ {
		if err := store.Append("conn-a", Entry{StartedAt: time.Now(), Statement: "SELECT n", Status: "success"}, limit); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Load("conn-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != limit {
		t.Fatalf("stored entries = %d, want the cap %d", len(got), limit)
	}
}

// TestStore_migratesMetadataColumns seeds a pre-metadata schema (the
// scoped query_log without language/replayable/sensitive) and proves Open
// adds the columns idempotently and existing rows read back with the
// legacy defaults: replayable, not sensitive, no language.
func TestStore_migratesMetadataColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE query_log (
		id INTEGER PRIMARY KEY,
		connection_id TEXT NOT NULL DEFAULT '',
		started_at INTEGER NOT NULL,
		statement TEXT NOT NULL,
		duration INTEGER NOT NULL,
		message TEXT NOT NULL,
		status TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO query_log (connection_id, started_at, statement, duration, message, status) VALUES (?, ?, 'SELECT legacy', 0, 'completed', 'success')`, "conn-a", time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.Load("conn-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %#v, want the legacy row", got)
	}
	entry := got[0]
	if entry.Statement != "SELECT legacy" || !entry.Replayable || entry.Sensitive || entry.Language != "" {
		t.Fatalf("legacy entry = %#v, want replayable, not sensitive, no language", entry)
	}

	// Reopening is idempotent: the second Open must not fail on existing
	// columns and must keep the row's values.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err = store.Load("conn-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Statement != "SELECT legacy" {
		t.Fatalf("entries after reopen = %#v, want the legacy row", got)
	}
}

// TestStore_metadataRoundTrip proves language, replayable, and sensitive
// survive persistence: explicit non-replayable and sensitive values are
// stored, not collapsed into the legacy defaults.
func TestStore_metadataRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := Entry{
		StartedAt:  time.Now(),
		Statement:  "SET key 1",
		Duration:   time.Millisecond,
		Message:    "completed",
		Status:     "success",
		Language:   "redis",
		Replayable: false,
		Sensitive:  true,
	}
	if err := store.Append("conn-a", entry, 100); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("conn-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %#v, want the metadata entry", got)
	}
	if got[0].Language != entry.Language || got[0].Replayable != entry.Replayable || got[0].Sensitive != entry.Sensitive {
		t.Fatalf("loaded entry = %#v, want language %q, replayable %t, sensitive %t", got[0], entry.Language, entry.Replayable, entry.Sensitive)
	}
}
