package notification

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestStore_persistsAndExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := Entry{CreatedAt: time.Now(), Title: "Hello", Description: "world", Level: 1}
	id, err := store.Append("conn-a", entry, 0)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("Append returned ID 0, want a real row ID")
	}
	// An expired row must be pruned on the next load.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO notifications(connection_id, created_at, title, description, level) VALUES (?, ?, ?, ?, ?)`,
		"conn-a", time.Now().AddDate(0, 0, -2).UnixNano(), "expired", "old", 0); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("conn-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != entry.Title || got[0].ID != id {
		t.Fatalf("notifications = %#v, want %#v", got, []Entry{entry})
	}
}

func TestStore_scopesEntriesByConnection(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Append("conn-a", Entry{CreatedAt: time.Now(), Title: "for A", Level: 1}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("conn-b", Entry{CreatedAt: time.Now(), Title: "for B", Level: 1}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("conn-a", Entry{CreatedAt: time.Now(), Title: "for A again", Level: 1}, 0); err != nil {
		t.Fatal(err)
	}
	gotA, err := store.Load("conn-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotA) != 2 || gotA[0].Title != "for A again" || gotA[1].Title != "for A" {
		t.Fatalf("scope conn-a entries = %#v, want the two A entries newest first", gotA)
	}
	gotB, err := store.Load("conn-b", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotB) != 1 || gotB[0].Title != "for B" {
		t.Fatalf("scope conn-b entries = %#v, want only B's entry", gotB)
	}
}

func TestStore_loadLimitReturnsNewestEntries(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	createdAt := time.Now().Add(-time.Hour)
	for i := 1; i <= Limit+1; i++ {
		if _, err := store.Append("conn-a", Entry{
			CreatedAt:   createdAt,
			Title:       "entry-" + strconv.Itoa(i),
			Description: "history",
		}, 0); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Load("conn-a", Limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != Limit {
		t.Fatalf("limited entries = %d, want %d", len(got), Limit)
	}
	if got[0].Title != "entry-101" || got[Limit-1].Title != "entry-2" {
		t.Fatalf("limited entries range = %q..%q, want entry-101..entry-2", got[0].Title, got[Limit-1].Title)
	}
}

func TestStore_emptyScopeIsRejected(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got, err := store.Load("", 0); err != nil || len(got) != 0 {
		t.Fatalf("Load(\"\") = %#v/%v, want no entries and no error", got, err)
	}
	if _, err := store.Append("", Entry{Title: "unscoped"}, 0); err == nil {
		t.Fatal("Append(\"\") succeeded, want a scope error")
	}
}

func TestStore_migratesLegacyTableWithoutLevelColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the pre-level schema.
	if _, err := db.Exec(`CREATE TABLE notifications (
		id INTEGER PRIMARY KEY,
		connection_id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notifications(connection_id, created_at, title, description) VALUES (?, ?, 'legacy', 'old')`, "conn-a", time.Now().UnixNano()); err != nil {
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
	got, err := store.Load("conn-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "legacy" || got[0].Level != 0 {
		t.Fatalf("legacy entries = %#v, want one neutral-level entry", got)
	}
}
