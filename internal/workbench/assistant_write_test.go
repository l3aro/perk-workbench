package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/ai"
	"github.com/l3aro/perk-workbench/internal/sqlite"
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
	service, err := sqlite.Open(ctx, ":memory:")
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
	model.databaseInfo = service.Info()
	model.Target = ":memory:"

	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_r1", Name: "sql_read", Input: map[string]any{"query": "SELECT count(*) as cnt FROM test"}},
			{ID: "call_w1", Name: "sql_write", Input: map[string]any{"query": "INSERT INTO test (val) VALUES ('hello')"}},
			{ID: "call_i1", Name: "get_connection_info", Input: map[string]any{}},
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.layout(140, 32)

	model.chat.input.SetValue("insert a row")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// First response: assistantToolStartMsg
	msg := cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	// sql_read executes immediately
	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	// sql_read result → advances to sql_write
	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	// sql_write → pendingWrite
	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)
	if model.chat.pendingWrite == nil {
		t.Fatal("expected pendingWrite after sql_write call")
	}

	// Approve via Model.Update (dialog handling)
	updated, cmd = model.Update(tea.KeyPressMsg{Code: 'y'})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected write cmd after approval")
	}

	// Write result
	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	// get_connection_info + next round → final answer
	model = driveToolRoundToCompletion(t, model, cmd)

	if model.chat.loading {
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
	service, err := sqlite.Open(ctx, ":memory:")
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
	model.databaseInfo = service.Info()
	model.Target = ":memory:"

	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_w2", Name: "sql_write", Input: map[string]any{"query": "INSERT INTO decline_test (val) VALUES ('world')"}},
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.layout(140, 32)

	model.chat.input.SetValue("insert")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	msg := cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)
	if model.chat.pendingWrite == nil {
		t.Fatal("expected pendingWrite")
	}

	// Decline via direct message.
	declineMsg := assistantWriteResultMsg{
		gen: model.chat.gen, callID: "call_w2", callName: "sql_write",
		err: "execution canceled by user", declined: true,
	}
	updated, _ = model.Update(declineMsg)
	model = updated.(Model)

	if model.chat.loading {
		t.Fatal("model should not be loading after decline")
	}

	// Verify cancel message in history.
	last := model.chat.messages[len(model.chat.messages)-1]
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
	service, err := sqlite.Open(ctx, ":memory:")
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
	model.databaseInfo = service.Info()
	model.Target = ":memory:"

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
	model.layout(140, 32)

	model.chat.input.SetValue("update")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	msg := cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)
	if model.chat.pendingWrite == nil {
		t.Fatal("expected pendingWrite for first write")
	}

	// Approve first (failing) write.
	updated, cmd = model.Update(tea.KeyPressMsg{Code: 'y'})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected write cmd after approval")
	}

	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	// Second round: corrected write.
	// Drive tool round: assistantToolStartMsg → continue → sql_write → pendingWrite
	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	if model.chat.pendingWrite == nil {
		t.Fatal("expected pendingWrite for corrected write")
	}

	// Approve second write.
	updated, cmd = model.Update(tea.KeyPressMsg{Code: 'y'})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected write cmd")
	}

	msg = cmd()
	updated, cmd = model.Update(msg)
	model = updated.(Model)

	// Final answer.
	model = driveToolRoundToCompletion(t, model, cmd)

	if model.chat.loading {
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

func TestChat_assistantWrite_readOnly(t *testing.T) {
	ctx := context.Background()
	service, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("opening test service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	model := New("", ctx, nil, true)
	model.State = stateReady
	model.Database = service
	model.databaseInfo = service.Info()
	model.Target = ":memory:"
	model.ReadOnly = true

	tools := model.databaseTools()
	for _, td := range tools {
		if td.Name == "sql_write" {
			t.Fatal("read-only model should not expose sql_write")
		}
	}

	if commandAvailable("ai.yolo_writes.toggle", commandDef{scope: scopeGlobal}, model) {
		t.Error("ai.yolo_writes.toggle should be unavailable for read-only")
	}

	model.SetAI(&toolChatClient{}, nil)
	if commandAvailable("ai.yolo_writes.toggle", commandDef{scope: scopeGlobal}, model) {
		t.Error("ai.yolo_writes.toggle still unavailable for read-only with AI")
	}
}

func TestChat_assistantWrite_yolo(t *testing.T) {
	ctx := context.Background()
	service, err := sqlite.Open(ctx, ":memory:")
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
	model.databaseInfo = service.Info()
	model.Target = ":memory:"

	tc := &toolWriteClient{
		firstCalls: []ai.ToolCall{
			{ID: "call_y1", Name: "sql_write", Input: map[string]any{"query": "INSERT INTO yolo_test (val) VALUES ('yolo')"}},
		},
	}
	model.SetAI(tc, nil)
	model.Focus = focusChat
	model.layout(140, 32)

	// Enable YOLO via palette command path.
	updated2, _ := model.handlePaletteCommand("ai.yolo_writes.toggle")
	model = updated2.(Model)
	if !model.chat.yoloWrites {
		t.Fatal("yoloWrites should be true after toggle")
	}
	if !strings.Contains(model.chatContentView(), "YOLO") {
		t.Error("chat pane missing YOLO indicator")
	}
	if strings.Contains(model.footer(), "YOLO") {
		t.Error("footer should not contain YOLO indicator")
	}

	// CommandAvailable should report it.
	if !commandAvailable("ai.yolo_writes.toggle", commandDef{scope: scopeGlobal}, model) {
		t.Error("ai.yolo_writes.toggle should be available for writable model")
	}

	model.chat.input.SetValue("yolo insert")
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'i'})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	model = driveToolRoundToCompletion(t, model, cmd)

	if model.chat.loading {
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
	if model.chat.yoloWrites {
		t.Error("yoloWrites should be false after disconnect")
	}
}
