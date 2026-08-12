package workbench

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/workbench/notification"
)

// collectMessages executes a command tree to completion and appends every
// produced message, flattening any []tea.Cmd-typed message (tea.BatchMsg and
// the unexported sequenceMsg) at any depth.
func collectMessages(message tea.Msg, messages *[]tea.Msg) {
	value := reflect.ValueOf(message)
	if value.Kind() == reflect.Slice && value.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		for index := range value.Len() {
			collectMessages(value.Index(index).Interface().(tea.Cmd)(), messages)
		}
		return
	}
	if message != nil {
		*messages = append(*messages, message)
	}
}

// executeCommandAll runs a command to completion and returns every message
// it produces. Commands may batch; batches are flattened recursively.
func executeCommandAll(command tea.Cmd) []tea.Msg {
	if command == nil {
		return nil
	}
	var messages []tea.Msg
	collectMessages(command(), &messages)
	return messages
}

// driveCommand feeds every message a command produces back into the model,
// following returned commands for a bounded number of steps, and returns the
// final model. The bound prevents recurring or debounce command chains from
// spinning forever.
func driveCommand(model Model, command tea.Cmd) Model {
	for range 12 {
		if command == nil {
			return model
		}
		messages := executeCommandAll(command)
		if len(messages) == 0 {
			return model
		}
		command = nil
		for _, message := range messages {
			updated, next := model.Update(message)
			model = updated.(Model)
			if next != nil && command == nil {
				command = next
			}
		}
	}
	return model
}

// assertOnlyNotificationTick fails unless every message the command produces
// is the notification dismiss sentinel (i.e. the command is the popup tick
// that status-changing updates batch in, not a real request).
func assertOnlyNotificationTick(t *testing.T, command tea.Cmd) {
	t.Helper()
	for _, message := range executeCommandAll(command) {
		if _, tick := message.(notification.DismissMsg); !tick {
			t.Fatalf("command sent an unexpected message %T", message)
		}
	}
}
