package notification

import (
	"fmt"
	"testing"
	"time"
)

func TestModelShowCapsNewestEntries(t *testing.T) {
	m := New()
	for i := 1; i <= Limit+1; i++ {
		updated, _, _ := m.Show(StatusEntry(fmt.Sprintf("entry-%d", i)), false, time.Minute)
		m = updated
	}
	if len(m.Entries) != Limit {
		t.Fatalf("live entries = %d, want %d", len(m.Entries), Limit)
	}
	if got, want := m.Entries[0].Description, "entry-101"; got != want {
		t.Fatalf("newest entry = %q, want %q", got, want)
	}
	if got, want := m.Entries[Limit-1].Description, "entry-2"; got != want {
		t.Fatalf("oldest retained entry = %q, want %q", got, want)
	}
}

func TestModelApplyPersistedMatchesToken(t *testing.T) {
	m, _, token := New().Show(StatusEntry("save me"), true, time.Minute)
	if token == 0 {
		t.Fatal("persistent Show returned zero token")
	}
	if m.Popup == nil || m.Popup.ID != 0 {
		t.Fatalf("popup before persistence = %#v, want an unsaved popup", m.Popup)
	}
	m.ApplyPersisted(token, 42)
	if got := m.Entries[0].ID; got != 42 {
		t.Fatalf("entry ID = %d, want 42", got)
	}
	if got := m.Popup.ID; got != 42 {
		t.Fatalf("popup ID = %d, want 42", got)
	}

	m.ApplyPersisted(token+1, 99)
	if got := m.Entries[0].ID; got != 42 {
		t.Fatalf("stale token changed entry ID to %d", got)
	}
	if got := m.Popup.ID; got != 42 {
		t.Fatalf("stale token changed popup ID to %d", got)
	}
}

func TestModelShowWithoutPersistenceReturnsZeroToken(t *testing.T) {
	m, _, token := New().Show(StatusEntry("transient"), false, time.Minute)
	if token != 0 {
		t.Fatalf("transient Show token = %d, want zero", token)
	}
	if m.Entries[0].persistToken != 0 {
		t.Fatalf("transient entry token = %d, want zero", m.Entries[0].persistToken)
	}
}
