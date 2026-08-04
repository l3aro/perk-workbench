package ai

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Conversation struct {
	ID           string
	ConnectionID string
	Title        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS conversations (id TEXT PRIMARY KEY, connection_id TEXT NOT NULL DEFAULT '', title TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing conversation history: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS messages (conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, role TEXT NOT NULL, agent TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing conversation history: %w", err)
	}
	if err := migrateConversations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrating conversation history: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("protecting conversation history: %w", err)
	}
	return &History{db: db}, nil
}

// migrateConversations adds the connection_id scope column to legacy
// databases and quarantines every pre-scope row behind one generated scope so
// nothing from before connection scoping can surface in a new connection.
func migrateConversations(db *stdsql.DB) error {
	hasColumn, err := tableHasColumn(db, "conversations", "connection_id")
	if err != nil {
		return err
	}
	if !hasColumn {
		if _, err := db.Exec(`ALTER TABLE conversations ADD COLUMN connection_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	var legacy int
	if err := db.QueryRow(`SELECT COUNT(*) FROM conversations WHERE connection_id = ''`).Scan(&legacy); err != nil {
		return err
	}
	if legacy > 0 {
		scope, err := randomID()
		if err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE conversations SET connection_id = ? WHERE connection_id = ''`, scope); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS conversations_connection_updated ON conversations (connection_id, updated_at DESC)`)
	return err
}

func tableHasColumn(db *stdsql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue stdsql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (h *History) Close() error { return h.db.Close() }

// RenameConversation updates the title of a conversation, leaving timestamps
// untouched so renames never reorder the history list.
func (h *History) RenameConversation(ctx context.Context, connectionID, conversationID, title string) error {
	if _, err := h.db.ExecContext(ctx, `UPDATE conversations SET title = ? WHERE id = ? AND connection_id = ?`, title, conversationID, connectionID); err != nil {
		return fmt.Errorf("renaming conversation: %w", err)
	}
	return nil
}

func (h *History) NewConversation(ctx context.Context, connectionID, title string) (Conversation, error) {
	id, err := randomID()
	if err != nil {
		return Conversation{}, err
	}
	now := time.Now().UTC()
	conversation := Conversation{ID: id, ConnectionID: connectionID, Title: title, CreatedAt: now, UpdatedAt: now}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO conversations (id, connection_id, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, conversation.ID, conversation.ConnectionID, conversation.Title, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return Conversation{}, fmt.Errorf("saving conversation: %w", err)
	}
	return conversation, nil
}

// AppendMessage inserts only through a scope-checked parent conversation, so
// a wrong connection scope is a silent no-op instead of a cross-scope write.
func (h *History) AppendMessage(ctx context.Context, connectionID, conversationID string, message Message) error {
	now := time.Now().UTC()
	// One transaction: a message insert plus its conversation touch previously
	// ran as two implicit transactions (two fsyncs) per message.
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning message save: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (conversation_id, role, agent, content, created_at)
		SELECT ?, ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM conversations WHERE id = ? AND connection_id = ?)`,
		conversationID, message.Role, message.Agent, message.Content, now.Format(time.RFC3339Nano), conversationID, connectionID); err != nil {
		return fmt.Errorf("saving conversation message: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ? AND connection_id = ?`, now.Format(time.RFC3339Nano), conversationID, connectionID); err != nil {
		return fmt.Errorf("updating conversation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing conversation message: %w", err)
	}
	return nil
}

func (h *History) Messages(ctx context.Context, connectionID, conversationID string) ([]Message, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT m.role, m.agent, m.content, m.created_at
		FROM messages m
		WHERE m.conversation_id = ? AND EXISTS (
			SELECT 1 FROM conversations c WHERE c.id = m.conversation_id AND c.connection_id = ?
		) ORDER BY m.rowid`, conversationID, connectionID)
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

func (h *History) Conversations(ctx context.Context, connectionID string) ([]Conversation, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, connection_id, title, created_at, updated_at FROM conversations WHERE connection_id = ? ORDER BY updated_at DESC`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("loading conversations: %w", err)
	}
	defer rows.Close()
	conversations := []Conversation{}
	for rows.Next() {
		var conversation Conversation
		var createdAt, updatedAt string
		if err := rows.Scan(&conversation.ID, &conversation.ConnectionID, &conversation.Title, &createdAt, &updatedAt); err != nil {
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

func (h *History) DeleteConversation(ctx context.Context, connectionID, conversationID string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("deleting conversation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id = ? AND EXISTS (
		SELECT 1 FROM conversations WHERE id = ? AND connection_id = ?
	)`, conversationID, conversationID, connectionID); err != nil {
		return fmt.Errorf("deleting conversation messages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE id = ? AND connection_id = ?`, conversationID, connectionID); err != nil {
		return fmt.Errorf("deleting conversation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("deleting conversation: %w", err)
	}
	return nil
}

func (h *History) Clear(ctx context.Context, connectionID string) error {
	if _, err := h.db.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id IN (
		SELECT id FROM conversations WHERE connection_id = ?
	)`, connectionID); err != nil {
		return fmt.Errorf("clearing conversation messages: %w", err)
	}
	if _, err := h.db.ExecContext(ctx, `DELETE FROM conversations WHERE connection_id = ?`, connectionID); err != nil {
		return fmt.Errorf("clearing conversations: %w", err)
	}
	return nil
}

func randomID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("creating conversation ID: %w", err)
	}
	return id.String(), nil
}
