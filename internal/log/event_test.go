package log

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWritesConcurrently(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			Error("test", os.ErrNotExist)
			Printf("iteration %d", n)
		}(i)
	}
	wg.Wait()

	b, err := os.ReadFile(filepath.Join(dir, "perk-workbench", "event.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("log file is empty")
	}
}

func TestErrorNilIsNoop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	Error("nil test", nil)

	if _, err := os.Stat(filepath.Join(dir, "perk-workbench", "event.log")); err == nil {
		t.Fatal("file created for nil error")
	}
}
