package ai

import (
	"context"
	"crypto/rand"
	stdsql "database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Conversation struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Message struct {
	Role      Role
	Agent     string
	Content   string
	ToolCalls []ToolCall
	ToolID    string
	ToolName  string
	CreatedAt time.Time
}

type History struct{ db *stdsql.DB }

func HistoryPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk-workbench", "data.db"), nil
}

func OpenHistory(path string) (*History, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating conversation directory: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=rwc&_pragma=busy_timeout(5000)"}).String()
	db, err := stdsql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening conversation history: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS conversations (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing conversation history: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS messages (conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, role TEXT NOT NULL, agent TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing conversation history: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("protecting conversation history: %w", err)
	}
	return &History{db: db}, nil
}

func (h *History) Close() error { return h.db.Close() }

// RenameConversation updates the title of a conversation, leaving timestamps
// untouched so renames never reorder the history list.
func (h *History) RenameConversation(ctx context.Context, conversationID, title string) error {
	if _, err := h.db.ExecContext(ctx, `UPDATE conversations SET title = ? WHERE id = ?`, title, conversationID); err != nil {
		return fmt.Errorf("renaming conversation: %w", err)
	}
	return nil
}

func (h *History) NewConversation(ctx context.Context, title string) (Conversation, error) {
	id, err := randomID()
	if err != nil {
		return Conversation{}, err
	}
	now := time.Now().UTC()
	conversation := Conversation{ID: id, Title: title, CreatedAt: now, UpdatedAt: now}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO conversations (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`, conversation.ID, conversation.Title, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return Conversation{}, fmt.Errorf("saving conversation: %w", err)
	}
	return conversation, nil
}

func (h *History) AppendMessage(ctx context.Context, conversationID string, message Message) error {
	now := time.Now().UTC()
	if _, err := h.db.ExecContext(ctx, `INSERT INTO messages (conversation_id, role, agent, content, created_at) VALUES (?, ?, ?, ?, ?)`, conversationID, message.Role, message.Agent, message.Content, now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("saving conversation message: %w", err)
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now.Format(time.RFC3339Nano), conversationID); err != nil {
		return fmt.Errorf("updating conversation: %w", err)
	}
	return nil
}

func (h *History) Messages(ctx context.Context, conversationID string) ([]Message, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT role, agent, content, created_at FROM messages WHERE conversation_id = ? ORDER BY rowid`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("loading conversation messages: %w", err)
	}
	defer rows.Close()
	messages := []Message{}
	for rows.Next() {
		var message Message
		var createdAt string
		if err := rows.Scan(&message.Role, &message.Agent, &message.Content, &createdAt); err != nil {
			return nil, fmt.Errorf("reading conversation message: %w", err)
		}
		message.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parsing conversation timestamp: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading conversation messages: %w", err)
	}
	return messages, nil
}

func (h *History) Conversations(ctx context.Context) ([]Conversation, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, title, created_at, updated_at FROM conversations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("loading conversations: %w", err)
	}
	defer rows.Close()
	conversations := []Conversation{}
	for rows.Next() {
		var conversation Conversation
		var createdAt, updatedAt string
		if err := rows.Scan(&conversation.ID, &conversation.Title, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("reading conversation: %w", err)
		}
		conversation.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parsing conversation timestamp: %w", err)
		}
		conversation.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing conversation timestamp: %w", err)
		}
		conversations = append(conversations, conversation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading conversations: %w", err)
	}
	return conversations, nil
}

func (h *History) DeleteConversation(ctx context.Context, conversationID string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("deleting conversation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id = ?`, conversationID); err != nil {
		return fmt.Errorf("deleting conversation messages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, conversationID); err != nil {
		return fmt.Errorf("deleting conversation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("deleting conversation: %w", err)
	}
	return nil
}

func (h *History) Clear(ctx context.Context) error {
	if _, err := h.db.ExecContext(ctx, `DELETE FROM messages`); err != nil {
		return fmt.Errorf("clearing conversation messages: %w", err)
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM conversations`); err != nil {
		return fmt.Errorf("clearing conversations: %w", err)
	}
	return nil
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("creating conversation ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
