package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/workbench/chat"
)

// toolWriteClient simulates a provider that returns write tool calls.
// It records all Complete requests for protocol assertions.
type toolWriteClient struct {
	round       int
	firstCalls  []ai.ToolCall
	secondRound func(req ai.Request) (ai.Response, error)
	requests    []ai.Request
}

func (c *toolWriteClient) AgentForPrompt(string) string { return "assistant" }
func (c *toolWriteClient) SupportsTools(string) bool    { return true }
func (c *toolWriteClient) GenerateTitle(context.Context, string) (string, error) {
	return "Cheap title", nil
}
func (c *toolWriteClient) Chat(_ context.Context, _ ai.Request) (ai.Response, error) {
	return ai.Response{Agent: "Assistant", Content: "Chat response"}, nil
}
func (c *toolWriteClient) ChatStream(_ context.Context, _ ai.Request) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent)
	close(ch)
	return ch, nil
}
func (c *toolWriteClient) Complete(_ context.Context, req ai.Request) (ai.Response, error) {
	c.requests = append(c.requests, req)
	c.round++
	if c.round == 1 {
		if len(c.firstCalls) == 0 {
			return ai.Response{Agent: "Assistant", Content: "final"}, nil
		}
		return ai.Response{Agent: "Assistant", ToolCalls: c.firstCalls}, nil
	}
	if c.secondRound != nil {
		return c.secondRound(req)
	}
	return ai.Response{Agent: "Assistant", Content: "done"}, nil
}

func TestChat_assistantWrite_approve(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	_, err = service.Execute(ctx, "CREATE TABLE test (id INTEGER PRIMARY KEY, val TEXT)")
	if err != nil {
		t.Fatalf("creating table: %v", err)
	}

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_r1", Name: "sql_read", Input: map[string]any{"query": "SELECT count(*) as cnt FROM test"}},
			{ID: "call_w1", Name: "sql_write", Input: map[string]any{"query": "INSERT INTO test (val) VALUES ('hello')"}},
			{ID: "call_i1", Name: "get_connection_info", Input: map[string]any{}},
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.applyLayout(140, 32)

	model.chat.component.Input.SetValue("insert a row")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// First response: chat.ToolStartMsg
	model, cmd = resolveChatCommand(model, cmd)

	// sql_read executes immediately
	model, cmd = resolveChatCommand(model, cmd)

	// sql_read result → advances to sql_write
	model, cmd = resolveChatCommand(model, cmd)

	// sql_write → chat.PendingWrite
	model, cmd = resolveChatCommand(model, cmd)
	if model.chat.component.ActiveRun().PendingWrite == nil {
		t.Fatal("expected chat.PendingWrite after sql_write call")
	}

	// Approve via Model.Update (dialog handling)
	updated, cmd = model.Update(tea.KeyPressMsg{Code: 'y'})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected write cmd after approval")
	}

	// Write result
	model, cmd = resolveChatCommand(model, cmd)

	// get_connection_info + next round → final answer
	model = driveToolRoundToCompletion(t, model, cmd)

	if model.chat.component.ActiveRun().Loading {
		t.Fatal("model should not be loading after completion")
	}

	// Protocol: all 3 tool results in order.
	if len(tc.requests) < 2 {
		t.Fatalf("expected >=2 Complete calls, got %d", len(tc.requests))
	}
	req2 := tc.requests[1]
	var toolIDs []string
	for _, m := range req2.Messages {
		if m.Role == ai.RoleTool {
			toolIDs = append(toolIDs, m.ToolID)
		}
	}
	wantIDs := []string{"call_r1", "call_w1", "call_i1"}
	if len(toolIDs) != 3 {
		t.Fatalf("got %d tool results, want 3: %v", len(toolIDs), toolIDs)
	}
	for i, id := range wantIDs {
		if toolIDs[i] != id {
			t.Errorf("tool result[%d] = %s, want %s", i, toolIDs[i], id)
		}
	}

	// Verify insert was committed.
	res, err := service.ExecuteReadOnly(ctx, "SELECT count(*) as cnt FROM test")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if len(res.Rows) != 1 || *res.Rows[0][0] != "1" {
		t.Fatalf("expected 1 row, got %v", res.Rows)
	}
}

func TestChat_assistantWrite_decline(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	_, err = service.Execute(ctx, "CREATE TABLE decline_test (id INTEGER PRIMARY KEY, val TEXT)")
	if err != nil {
		t.Fatalf("creating table: %v", err)
	}

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_w2", Name: "sql_write", Input: map[string]any{"query": "INSERT INTO decline_test (val) VALUES ('world')"}},
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.applyLayout(140, 32)

	model.chat.component.Input.SetValue("insert")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	model, cmd = resolveChatCommand(model, cmd)

	model, cmd = resolveChatCommand(model, cmd)
	if model.chat.component.ActiveRun().PendingWrite == nil {
		t.Fatal("expected chat.PendingWrite")
	}

	// Decline via direct message.
	declineMsg := chat.WriteResultMsg{
		Gen: model.chat.component.ActiveRun().Gen, CallID: "call_w2", CallName: "sql_write",
		Err: "execution canceled by user", Declined: true,
	}
	updated, _ = model.Update(declineMsg)
	model = updated.(Model)

	if model.chat.component.ActiveRun().Loading {
		t.Fatal("model should not be loading after decline")
	}

	// Verify cancel message in history.
	last := model.chat.component.ActiveRun().Messages[len(model.chat.component.ActiveRun().Messages)-1]
	if !strings.Contains(last.Content, "execution canceled by user") {
		t.Fatalf("last tool result = %q, want cancel message", last.Content)
	}

	// Table should be empty.
	res, err := service.ExecuteReadOnly(ctx, "SELECT count(*) as cnt FROM decline_test")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if len(res.Rows) == 1 && res.Rows[0][0] != nil && *res.Rows[0][0] != "0" {
		t.Fatal("table should be empty after decline")
	}
}

func TestChat_assistantWrite_failedThenCorrected(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	_, err = service.Execute(ctx, "CREATE TABLE fix_test (id INTEGER PRIMARY KEY, val TEXT)")
	if err != nil {
		t.Fatalf("creating table: %v", err)
	}
	_, err = service.Execute(ctx, "INSERT INTO fix_test (id, val) VALUES (1, 'original')")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	var secondDone bool
	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_f1", Name: "sql_write", Input: map[string]any{"query": "UPDATE nonexistent_tbl SET val = 'x'"}},
		},
		secondRound: func(req ai.Request) (ai.Response, error) {
			if secondDone {
				return ai.Response{Agent: "Assistant", Content: "done fixing"}, nil
			}
			secondDone = true
			return ai.Response{Agent: "Assistant", ToolCalls: []ai.ToolCall{{
				ID: "call_f2", Name: "sql_write", Input: map[string]any{"query": "UPDATE fix_test SET val = 'fixed' WHERE id = 1"}},
			}}, nil
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.applyLayout(140, 32)

	model.chat.component.Input.SetValue("update")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	model, cmd = resolveChatCommand(model, cmd)

	model, cmd = resolveChatCommand(model, cmd)
	if model.chat.component.ActiveRun().PendingWrite == nil {
		t.Fatal("expected chat.PendingWrite for first write")
	}

	// Approve first (failing) write.
	updated, cmd = model.Update(tea.KeyPressMsg{Code: 'y'})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected write cmd after approval")
	}

	model, cmd = resolveChatCommand(model, cmd)

	// Second round: corrected write.
	// Drive tool round: chat.ToolStartMsg → continue → sql_write → chat.PendingWrite
	model, cmd = resolveChatCommand(model, cmd)

	model, cmd = resolveChatCommand(model, cmd)

	if model.chat.component.ActiveRun().PendingWrite == nil {
		t.Fatal("expected chat.PendingWrite for corrected write")
	}

	// Approve second write.
	updated, cmd = model.Update(tea.KeyPressMsg{Code: 'y'})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected write cmd")
	}

	model, cmd = resolveChatCommand(model, cmd)

	// Final answer.
	model = driveToolRoundToCompletion(t, model, cmd)

	if model.chat.component.ActiveRun().Loading {
		t.Fatal("model should not be loading")
	}

	// Verify corrected update succeeded.
	res, err := service.ExecuteReadOnly(ctx, "SELECT val FROM fix_test WHERE id = 1")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if len(res.Rows) != 1 || *res.Rows[0][0] != "fixed" {
		t.Fatalf("expected val='fixed', got %v", res.Rows)
	}
}

// TestChat_assistantWrite_escapeExitsInsertFirst guards the Escape precedence
// while the write confirmation is up: the first Escape must exit insert mode
// (keeping the confirmation and the agent run intact), and only the second
// Escape — now in normal mode — declines the write and interrupts the agent.
func TestChat_assistantWrite_escapeExitsInsertFirst(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	_, err = service.Execute(ctx, "CREATE TABLE esc_test (id INTEGER PRIMARY KEY, val TEXT)")
	if err != nil {
		t.Fatalf("creating table: %v", err)
	}

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_esc", Name: "sql_write", Input: map[string]any{"query": "INSERT INTO esc_test (val) VALUES ('nope')"}},
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.applyLayout(140, 32)

	model.chat.component.Input.SetValue("insert")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Drive the tool round until the write awaits confirmation.
	for model.chat.component.ActiveRun().PendingWrite == nil && cmd != nil {
		model, cmd = resolveChatCommand(model, cmd)
	}
	if model.chat.component.ActiveRun().PendingWrite == nil {
		t.Fatal("expected chat.PendingWrite after sql_write call")
	}
	if model.chat.component.ChatMode != chat.ModeInsert {
		t.Fatal("test setup: expected insert mode while awaiting write confirmation")
	}

	// First Escape: exit insert mode, keep confirmation and agent run.
	updated, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	assertOnlyNotificationTick(t, cmd)
	if model.chat.component.ChatMode != chat.ModeNormal {
		t.Fatalf("chat mode = %d, want normal after first escape", model.chat.component.ChatMode)
	}
	if model.chat.component.ActiveRun().PendingWrite == nil {
		t.Fatal("first Escape should keep the write confirmation up")
	}
	if model.chat.component.ActiveRun().RoundState == nil {
		t.Fatal("first Escape should NOT interrupt the agent run")
	}
	if !model.chat.component.ActiveRun().Loading {
		t.Fatal("first Escape should NOT stop loading")
	}

	// Second Escape: now in normal mode — declines the write, interrupts the run.
	updated, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("second Escape should produce the declined write result")
	}
	model, _ = resolveChatCommand(model, cmd)

	if model.chat.component.ActiveRun().PendingWrite != nil {
		t.Fatal("second Escape should clear the write confirmation")
	}
	if model.chat.component.ActiveRun().RoundState != nil {
		t.Fatal("second Escape should interrupt the agent run")
	}
	if model.chat.component.ActiveRun().Loading {
		t.Fatal("model should not be loading after second escape")
	}

	// The write must not have been executed.
	res, err := service.ExecuteReadOnly(ctx, "SELECT count(*) as cnt FROM esc_test")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if len(res.Rows) != 1 || *res.Rows[0][0] != "0" {
		t.Fatalf("expected 0 rows, got %v", res.Rows)
	}
}

func TestChat_assistantWrite_readOnly(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil, true)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"
	model.ReadOnly = true
	model.chat.component.ReadOnly = true

	tools := model.chat.component.DatabaseTools(model.chatLayout())
	for _, td := range tools {
		if td.Name == "sql_write" {
			t.Fatal("read-only model should not expose sql_write")
		}
	}
}

// TestChat_assistantWrite_confirmationRenders guards the View overlay gate:
// a pending sql_write confirmation must render as a dialog, otherwise the
// chat pane shows an eternal "thinking..." while input is swallowed by the
// invisible dialog.
func TestChat_assistantWrite_confirmationRenders(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	_, err = service.Execute(ctx, "CREATE TABLE confirm_view_test (id INTEGER PRIMARY KEY, val TEXT)")
	if err != nil {
		t.Fatalf("creating table: %v", err)
	}

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	const statement = "INSERT INTO confirm_view_test (val) VALUES ('hello')"
	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_v1", Name: "sql_write", Input: map[string]any{"query": statement}},
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.applyLayout(140, 32)

	model.chat.component.Input.SetValue("insert a row")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Drive the tool round until the write awaits confirmation.
	for model.chat.component.ActiveRun().PendingWrite == nil && cmd != nil {
		model, cmd = resolveChatCommand(model, cmd)
	}
	if model.chat.component.ActiveRun().PendingWrite == nil {
		t.Fatal("expected chat.PendingWrite after sql_write call")
	}

	view := ansi.Strip(model.View().Content)
	if !strings.Contains(view, "Run assistant SQL write?") {
		t.Fatalf("confirmation dialog not rendered: %q", view)
	}
	if !strings.Contains(view, statement) {
		t.Fatalf("confirmation dialog missing statement: %q", view)
	}
	if !strings.Contains(view, "Yes") || !strings.Contains(view, "No") {
		t.Fatalf("confirmation dialog missing options: %q", view)
	}
}

func TestChat_assistantWrite_yolo(t *testing.T) {
	ctx := context.Background()
	service, err := openTestSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	_, err = service.Execute(ctx, "CREATE TABLE yolo_test (id INTEGER PRIMARY KEY, val TEXT)")
	if err != nil {
		t.Fatalf("creating table: %v", err)
	}

	model := New("", ctx, nil, false)
	model.State = stateReady
	model.Database = service
	model.chat.component.Executor = chatExecutor{service: service}
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.chat.component.Target = ":memory:"

	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_y1", Name: "sql_write", Input: map[string]any{"query": "INSERT INTO yolo_test (val) VALUES ('yolo')"}},
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.applyLayout(140, 32)

	// Enable YOLO via the AI slash command.
	model.chat.component.Input.SetValue("/yolo-on")
	updated2, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated2.(Model)
	updated2, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated2.(Model)
	assertOnlyNotificationTick(t, cmd)
	if !model.chat.component.YoloWrites {
		t.Fatal("yoloWrites should be true after command")
	}
	if !strings.Contains(model.chatContentView(), "YOLO") {
		t.Error("chat pane missing YOLO indicator")
	}
	if strings.Contains(model.footer(), "YOLO") {
		t.Error("footer should not contain YOLO indicator")
	}

	model.chat.component.Input.SetValue("yolo insert")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	model = driveToolRoundToCompletion(t, model, cmd)

	if model.chat.component.ActiveRun().Loading {
		t.Fatal("model should not be loading")
	}

	res, err := service.ExecuteReadOnly(ctx, "SELECT count(*) as cnt FROM yolo_test")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if len(res.Rows) != 1 || *res.Rows[0][0] != "1" {
		t.Fatalf("expected 1 row, got %v", res.Rows)
	}

	model.disconnect()
	if model.chat.component.YoloWrites {
		t.Error("yoloWrites should be false after disconnect")
	}
}
