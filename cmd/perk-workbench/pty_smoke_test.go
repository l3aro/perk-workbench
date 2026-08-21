package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/xpty"
)

func TestBuiltHostSQLitePTYReachesReady(t *testing.T) {
	if testing.Short() {
		t.Skip("PTY smoke test is not part of short runs")
	}

	repoRoot := filepath.Join("..", "..")
	binary := filepath.Join(t.TempDir(), "perk-workbench")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, "./cmd/perk-workbench")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}

	configHome := t.TempDir()
	database := filepath.Join(repoRoot, "demo", "chinook-sqlite.db")
	command := exec.Command(binary, database)
	command.Env = append(os.Environ(), "XDG_CONFIG_HOME="+configHome, "TERM=xterm-256color")

	terminal, err := xpty.NewPty(140, 40)
	if err != nil {
		t.Fatalf("create PTY: %v", err)
	}
	defer terminal.Close()
	if err := terminal.Start(command); err != nil {
		t.Fatalf("start host in PTY: %v", err)
	}

	output := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		chunk := make([]byte, 4096)
		for {
			n, readErr := terminal.Read(chunk)
			if n > 0 {
				buffer.Write(chunk[:n])
				plain := ansi.Strip(buffer.String())
				// The ready-state footer carries the database product
				// version next to the fullscreen hint; the opening and
				// failure screens never render both.
				if strings.Contains(plain, "f fullscreen") && sqliteInfo.MatchString(plain) {
					output <- plain
					return
				}
			}
			if readErr != nil {
				output <- ansi.Strip(buffer.String())
				return
			}
		}
	}()

	var plain string
	select {
	case plain = <-output:
	case <-time.After(30 * time.Second):
		_ = command.Process.Kill()
		t.Fatalf("SQLite PTY did not reach ready state within 30s")
	}
	for _, obsolete := range []string{"incomplete-release-metadata", "sqlite-not-registered", "sqlite driver not registered"} {
		if strings.Contains(plain, obsolete) {
			t.Fatalf("PTY transcript contains obsolete startup error %q:\n%s", obsolete, plain)
		}
	}
	if !sqliteInfo.MatchString(plain) || !strings.Contains(plain, "f fullscreen") {
		t.Fatalf("PTY transcript did not contain the ready footer:\n%s", plain)
	}

	// Ctrl+C is the direct app.quit binding; through the PTY line
	// discipline it also raises SIGINT, so either path tears down.
	if _, err := terminal.Write([]byte{0x03}); err != nil {
		t.Fatalf("sending quit keystroke: %v", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- xpty.WaitProcess(context.Background(), command) }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatalf("host exit after PTY smoke: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("host did not exit after PTY smoke")
	}
}

// sqliteInfo matches the ready-state footer's database segment, for
// example "SQLite 3.53.3".
var sqliteInfo = regexp.MustCompile(`SQLite \d`)
