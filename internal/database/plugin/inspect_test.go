package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInspect_success: Inspect resolves, spawns, handshakes, validates,
// and closes one child; the result carries the canonical path, the
// advertised capabilities, and a clean final snapshot.
func TestInspect_success(t *testing.T) {
	t.Setenv("PERK_PLUGIN_HELPER", "1")
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin-child")
	copyPlugin(t, path)

	result := Inspect(context.Background(), path, "")
	if result.Phase != PhaseOK || result.Error != "" {
		t.Fatalf("Inspect = %+v, want an ok result", result)
	}
	if result.Path != path {
		t.Fatalf("Path = %q, want %q", result.Path, path)
	}
	if result.Capabilities == nil || result.Capabilities.Name != "pluginkv" {
		t.Fatalf("Capabilities = %+v, want the advertised pluginkv", result.Capabilities)
	}
	if result.Snapshot == nil || result.Snapshot.Running || result.Snapshot.PID != 0 || result.Snapshot.ExitStatus != 0 {
		t.Fatalf("Snapshot = %+v, want a reaped clean child", result.Snapshot)
	}
}

// TestInspect_resolveFailure: an unresolvable entry fails with the
// resolve phase and no path or snapshot.
func TestInspect_resolveFailure(t *testing.T) {
	result := Inspect(context.Background(), filepath.Join(t.TempDir(), "no-such-plugin"), "")
	if result.Phase != PhaseResolve || result.Error == "" || result.Path != "" {
		t.Fatalf("Inspect = %+v, want a resolve failure", result)
	}
	if result.Capabilities != nil || result.Snapshot != nil {
		t.Fatalf("Inspect = %+v, want no capabilities or snapshot on resolve failure", result)
	}
}

// TestSHA256File_roundTrip: the digest is lowercase hex of the exact
// bytes, and any byte change alters it.
func TestSHA256File_roundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "executable")
	contents := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		t.Fatal(err)
	}

	digest, err := SHA256File(path)
	if err != nil {
		t.Fatalf("SHA256File = %v, want nil error", err)
	}
	sum := sha256.Sum256(contents)
	if want := hex.EncodeToString(sum[:]); digest != want {
		t.Fatalf("digest = %s, want %s", digest, want)
	}
	if digest != strings.ToLower(digest) {
		t.Fatalf("digest = %s, want lowercase", digest)
	}

	if err := os.WriteFile(path, append(contents, []byte("# drifted\n")...), 0o755); err != nil {
		t.Fatal(err)
	}
	drifted, err := SHA256File(path)
	if err != nil {
		t.Fatalf("SHA256File = %v, want nil error", err)
	}
	if drifted == digest {
		t.Fatalf("drifted digest = %s, want it to differ from %s", drifted, digest)
	}
}

// TestSHA256File_missingFile: a missing file is an error.
func TestSHA256File_missingFile(t *testing.T) {
	if _, err := SHA256File(filepath.Join(t.TempDir(), "no-such-file")); err == nil {
		t.Fatal("SHA256File on a missing file = nil error, want an error")
	}
}
