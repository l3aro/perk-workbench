package ai

import (
	"context"
	"path/filepath"
	"testing"
)

func TestHistory_savesAndLoadsConversationMessages(t *testing.T) {
	history, err := OpenHistory(filepath.Join(t.TempDir(), "conversations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })

	conversation, err := history.NewConversation(context.Background(), "Schema advice")
	if err != nil {
		t.Fatal(err)
	}
	if err := history.AppendMessage(context.Background(), conversation.ID, Message{Role: RoleUser, Content: "Explain this table"}); err != nil {
		t.Fatal(err)
	}
	if err := history.AppendMessage(context.Background(), conversation.ID, Message{Role: RoleAssistant, Agent: "Assistant", Content: "It stores projects."}); err != nil {
		t.Fatal(err)
	}

	messages, err := history.Messages(context.Background(), conversation.ID)
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

	conversation, err := history.NewConversation(context.Background(), "Schema advice")
	if err != nil {
		t.Fatal(err)
	}
	conversations, err := history.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 || conversations[0].ID != conversation.ID {
		t.Fatalf("conversations = %#v", conversations)
	}
	if err := history.DeleteConversation(context.Background(), conversation.ID); err != nil {
		t.Fatal(err)
	}
	conversations, err = history.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 0 {
		t.Fatalf("conversations = %#v, want empty", conversations)
	}
}
