package plugin

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// TestClient_terminalConditions drives each protocol violation the host
// must treat as terminal: the failing call returns an error, the client
// becomes unusable (later calls fail immediately), every pending call is
// released, and Close stays safe and idempotent.
func TestClient_terminalConditions(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		behavior string
	}{
		{name: "wrong jsonrpc field", behavior: "wrong_jsonrpc"},
		{name: "malformed frame", behavior: "malformed"},
		{name: "invalid UTF-8 frame", behavior: "nonutf8"},
		{name: "nonnumeric id", behavior: "nonnumeric_id"},
		{name: "oversized frame", behavior: "oversized"},
		{name: "unknown id", behavior: "wrong_id"},
		{name: "duplicate response", behavior: "duplicate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PERK_PLUGIN_HELPER", "1")
			t.Setenv("PERK_PLUGIN_BEHAVIOR", test.behavior)
			client, err := spawn(executable, spawnArgs...)
			if err != nil {
				t.Fatalf("spawn: %v", err)
			}
			defer func() { _ = client.Close() }()

			// All in-flight calls fail: the first response frame is the
			// violation, the rest are released by the terminal path.
			const calls = 4
			var wg sync.WaitGroup
			errs := make([]error, calls)
			for i := 0; i < calls; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					var result sharedsql.Result
					errs[i] = client.Call(context.Background(), methodExecute, statementParams{Statement: "select 1"}, &result)
				}(i)
			}
			wg.Wait()
			for i, callErr := range errs {
				if callErr == nil {
					t.Fatalf("call %d succeeded, want a terminal error", i)
				}
			}

			// The client is unusable: the next call fails immediately.
			var result sharedsql.Result
			if err := client.Call(context.Background(), methodExecute, statementParams{Statement: "select 1"}, &result); err == nil {
				t.Fatal("call after terminal succeeded, want an immediate error")
			}

			// Double Close is safe: no panic, and both calls agree on
			// the reap error (nil for a clean child exit).
			first := client.Close()
			second := client.Close()
			if (first == nil) != (second == nil) {
				t.Fatalf("Close = %v then %v, want stable results", first, second)
			}
		})
	}
}

// TestClient_closeReleasesPendingCall covers Close while a call is still
// in flight: the pending call is released with an error and Close is
// idempotent.
func TestClient_closeReleasesPendingCall(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_BEHAVIOR", "block_execute")
	client, err := spawn(executable, spawnArgs...)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	callErr := make(chan error, 1)
	go func() {
		var result sharedsql.Result
		callErr <- client.Call(context.Background(), methodExecute, statementParams{Statement: "select 1"}, &result)
	}()
	time.Sleep(100 * time.Millisecond) // let the request reach the plugin

	if err := client.Close(); err != nil {
		t.Logf("Close = %v (clean child exit yields nil)", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}

	select {
	case err := <-callErr:
		if err == nil {
			t.Fatal("pending call succeeded across Close, want an error")
		}
		if !errors.Is(err, io.EOF) {
			t.Logf("pending call error = %v (terminal error)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending call not released by Close")
	}
}
