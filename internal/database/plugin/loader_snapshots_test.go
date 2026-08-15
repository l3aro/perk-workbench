package plugin

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/l3aro/perk-workbench/internal/database"
)

// TestLoaderSnapshots_afterClose: snapshots remain available after
// Loader.Close reaped every child, and reflect the final state.
func TestLoaderSnapshots_afterClose(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")

	loader, errs := Load(context.Background(), configPath, []string{executable}, func(database.Shim) error {
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	if err := loader.Close(); err != nil {
		t.Fatalf("Loader.Close = %v, want nil", err)
	}

	snapshots := loader.Snapshots()
	if len(snapshots) != 1 {
		t.Fatalf("Snapshots = %d entries, want 1", len(snapshots))
	}
	snap := snapshots[0]
	if snap.Path != executable {
		t.Fatalf("snapshot path = %q, want %q", snap.Path, executable)
	}
	if snap.Plugin != "pluginkv" {
		t.Fatalf("snapshot plugin = %q, want the handshake identity pluginkv", snap.Plugin)
	}
	if snap.InitDuration <= 0 {
		t.Fatalf("snapshot init duration = %v, want positive", snap.InitDuration)
	}
	if snap.Running || snap.PID != 0 {
		t.Fatalf("snapshot = %+v, want the child reaped (running=false, pid=0)", snap)
	}
	if snap.ExitStatus != 0 {
		t.Fatalf("snapshot exit status = %d, want 0 for a clean close", snap.ExitStatus)
	}

	// Close stays idempotent and snapshots still resolve afterwards.
	if err := loader.Close(); err != nil {
		t.Fatalf("second Loader.Close = %v, want nil", err)
	}
	if got := loader.Snapshots(); len(got) != 1 {
		t.Fatalf("Snapshots after second Close = %d entries, want 1", len(got))
	}
}

// TestLoaderSnapshots_immutableCopies: mutating a returned snapshot or
// its Stderr slice never affects the loader; a later call returns the
// untouched state.
func TestLoaderSnapshots_immutableCopies(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), []string{executable}, func(database.Shim) error {
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}
	t.Cleanup(func() { _ = loader.Close() })

	snapshots := loader.Snapshots()
	if len(snapshots) != 1 {
		t.Fatalf("Snapshots = %d entries, want 1", len(snapshots))
	}
	snapshots[0].Path = "tampered"
	snapshots[0].Plugin = "tampered"
	snapshots[0].Stderr = append(snapshots[0].Stderr, "tampered")

	again := loader.Snapshots()
	if len(again) != 1 {
		t.Fatalf("Snapshots = %d entries, want 1", len(again))
	}
	if again[0].Path == "tampered" || again[0].Plugin == "tampered" {
		t.Fatalf("snapshot mutation leaked into the loader: %+v", again[0])
	}
	for _, line := range again[0].Stderr {
		if line == "tampered" {
			t.Fatalf("Stderr mutation leaked into the loader: %q", line)
		}
	}
}

// TestLoaderSnapshots_concurrentAndAfterClose: Snapshots is safe to
// call concurrently with a child flooding stderr, and keeps resolving
// after Close — the race detector guards the lock discipline.
func TestLoaderSnapshots_concurrentAndAfterClose(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	t.Setenv("PERK_PLUGIN_STDERR_FLOOD", "1048576")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	loader, errs := Load(context.Background(), filepath.Join(t.TempDir(), "config.json"), []string{executable}, func(database.Shim) error {
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("Load errors = %v, want none", errs)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				loader.Snapshots()
			}
		}()
	}
	wg.Wait()
	if err := loader.Close(); err != nil {
		t.Fatalf("Loader.Close = %v, want nil", err)
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				loader.Snapshots()
			}
		}()
	}
	wg.Wait()

	snapshots := loader.Snapshots()
	if len(snapshots) != 1 {
		t.Fatalf("Snapshots = %d entries, want 1", len(snapshots))
	}
	if len(snapshots[0].Stderr) == 0 {
		t.Fatal("flooded stderr should leave a retained tail")
	}
	if snapshots[0].Running {
		t.Fatal("snapshot after Close still reports the child running")
	}
}
