package ai

import (
	"context"
	stdsql "database/sql"
	"path/filepath"
	"testing"
)

func TestHistoryPath_sharesDataDBWithQueryLog(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	path, err := HistoryPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(configDir, "perk-workbench", "data.db"); path != want {
		t.Fatalf("history path = %q, want %q", path, want)
	}
}

func TestHistory_savesAndLoadsConversationMessages(t *testing.T) {
	history, err := OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })

	conversation, err := history.NewConversation(context.Background(), "scope-a", "Schema advice")
	if err != nil {
		t.Fatal(err)
	}
	if conversation.ConnectionID != "scope-a" {
		t.Fatalf("conversation connection scope = %q, want scope-a", conversation.ConnectionID)
	}
	if err := history.AppendMessage(context.Background(), "scope-a", conversation.ID, Message{Role: RoleUser, Content: "Explain this table"}); err != nil {
		t.Fatal(err)
	}
	if err := history.AppendMessage(context.Background(), "scope-a", conversation.ID, Message{Role: RoleAssistant, Agent: "Assistant", Content: "It stores projects."}); err != nil {
		t.Fatal(err)
	}

	messages, err := history.Messages(context.Background(), "scope-a", conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].Agent != "Assistant" {
		t.Fatalf("messages = %#v, want persisted user and assistant messages", messages)
	}
}

func TestHistory_listsAndDeletesConversations(t *testing.T) {
	history, err := OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })

	conversation, err := history.NewConversation(context.Background(), "scope-a", "Schema advice")
	if err != nil {
		t.Fatal(err)
	}
	conversations, err := history.Conversations(context.Background(), "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 || conversations[0].ID != conversation.ID {
		t.Fatalf("conversations = %#v", conversations)
	}
	if err := history.DeleteConversation(context.Background(), "scope-a", conversation.ID); err != nil {
		t.Fatal(err)
	}
	conversations, err = history.Conversations(context.Background(), "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 0 {
		t.Fatalf("conversations = %#v, want empty", conversations)
	}
}

func TestHistory_scopesConversationsAndMessages(t *testing.T) {
	history, err := OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	ctx := context.Background()

	a, err := history.NewConversation(ctx, "scope-a", "A chat")
	if err != nil {
		t.Fatal(err)
	}
	b, err := history.NewConversation(ctx, "scope-b", "B chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := history.AppendMessage(ctx, "scope-a", a.ID, Message{Role: RoleUser, Content: "for A"}); err != nil {
		t.Fatal(err)
	}
	if err := history.AppendMessage(ctx, "scope-b", b.ID, Message{Role: RoleUser, Content: "for B"}); err != nil {
		t.Fatal(err)
	}

	// Conversations list only the requested scope.
	gotA, err := history.Conversations(ctx, "scope-a")
	if err != nil || len(gotA) != 1 || gotA[0].ID != a.ID {
		t.Fatalf("scope-a conversations = %#v, err %v, want only A", gotA, err)
	}
	gotB, err := history.Conversations(ctx, "scope-b")
	if err != nil || len(gotB) != 1 || gotB[0].ID != b.ID {
		t.Fatalf("scope-b conversations = %#v, err %v, want only B", gotB, err)
	}

	// Messages cannot be read through the wrong scope.
	if messages, err := history.Messages(ctx, "scope-b", a.ID); err != nil || len(messages) != 0 {
		t.Fatalf("scope-b messages for A's conversation = %#v, err %v, want none", messages, err)
	}

	// AppendMessage through the wrong scope is a no-op, not a cross-scope write.
	if err := history.AppendMessage(ctx, "scope-b", a.ID, Message{Role: RoleUser, Content: "hijack"}); err != nil {
		t.Fatal(err)
	}
	messages, err := history.Messages(ctx, "scope-a", a.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("A messages after wrong-scope append = %#v, err %v, want 1", messages, err)
	}

	// RenameConversation through the wrong scope must not rename.
	if err := history.RenameConversation(ctx, "scope-b", a.ID, "hijacked"); err != nil {
		t.Fatal(err)
	}
	gotA, _ = history.Conversations(ctx, "scope-a")
	if gotA[0].Title != "A chat" {
		t.Fatalf("A title after wrong-scope rename = %q, want A chat", gotA[0].Title)
	}

	// DeleteConversation through the wrong scope must not delete.
	if err := history.DeleteConversation(ctx, "scope-b", a.ID); err != nil {
		t.Fatal(err)
	}
	if conversations, err := history.Conversations(ctx, "scope-a"); err != nil || len(conversations) != 1 {
		t.Fatalf("scope-a conversations after wrong-scope delete = %#v, err %v, want 1", conversations, err)
	}

	// Clear must not touch the other scope.
	if err := history.Clear(ctx, "scope-a"); err != nil {
		t.Fatal(err)
	}
	gotB, err = history.Conversations(ctx, "scope-b")
	if err != nil || len(gotB) != 1 {
		t.Fatalf("scope-b conversations after scope-a clear = %#v, err %v, want 1", gotB, err)
	}
	if messages, err := history.Messages(ctx, "scope-b", b.ID); err != nil || len(messages) != 1 {
		t.Fatalf("B messages after scope-a clear = %#v, err %v, want 1", messages, err)
	}

	// The correct-scope delete still works.
	if err := history.DeleteConversation(ctx, "scope-b", b.ID); err != nil {
		t.Fatal(err)
	}
	if conversations, err := history.Conversations(ctx, "scope-b"); err != nil || len(conversations) != 0 {
		t.Fatalf("scope-b conversations after delete = %#v, err %v, want empty", conversations, err)
	}
}

func TestHistory_migratesLegacyConversations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversations.db")
	db, err := stdsql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the pre-scope schema: conversations without connection_id.
	if _, err := db.Exec(`CREATE TABLE conversations (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE messages (conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, role TEXT NOT NULL, agent TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO conversations (id, title, created_at, updated_at) VALUES ('legacy-1', 'old chat', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages (conversation_id, role, agent, content, created_at) VALUES ('legacy-1', 'user', 'assistant', 'old question', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	history, err := OpenHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })

	// A fresh connection scope must not see any legacy row.
	if conversations, err := history.Conversations(context.Background(), "fresh-scope"); err != nil || len(conversations) != 0 {
		t.Fatalf("fresh scope conversations = %#v, err %v, want none", conversations, err)
	}

	// All legacy rows share one nonempty generated scope.
	db, err = stdsql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT DISTINCT connection_id FROM conversations`)
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
	if len(scopes) != 1 || scopes[0] == "" {
		t.Fatalf("legacy scopes = %#v, want exactly one nonempty scope", scopes)
	}

	// The legacy conversation and its message remain readable behind that scope.
	conversations, err := history.Conversations(context.Background(), scopes[0])
	if err != nil || len(conversations) != 1 || conversations[0].ID != "legacy-1" {
		t.Fatalf("legacy conversations = %#v, err %v, want legacy-1", conversations, err)
	}
	messages, err := history.Messages(context.Background(), scopes[0], "legacy-1")
	if err != nil || len(messages) != 1 || messages[0].Content != "old question" {
		t.Fatalf("legacy messages = %#v, err %v, want old question", messages, err)
	}

	// Reopening must not reassign the migrated scope.
	history.Close()
	history, err = OpenHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	db2, err := stdsql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var still string
	if err := db2.QueryRow(`SELECT DISTINCT connection_id FROM conversations`).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if still != scopes[0] {
		t.Fatalf("scope after reopen = %q, want %q", still, scopes[0])
	}
}
