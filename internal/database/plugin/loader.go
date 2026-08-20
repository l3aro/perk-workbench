package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
)

// Loader owns the lifecycle of every configured plugin entry: the
// spawned child, the registered shim, and every session opened in them.
// Close is the single idempotent cleanup path. Entries rejected at
// load (resolution, pin drift, handshake, protocol, or registration
// failure) are retained with their failure so they stay inspectable and
// restartable; Restart recovers exactly one entry.
type Loader struct {
	mu         sync.Mutex
	configPath string
	entries    []*entry // one per unique configured entry, in config order
	clients    []*Client
	sessions   map[*sessionProxy]struct{}
	closed     bool
}

// spawnArgs is the argv appended to every plugin spawn. Production loads
// leave it nil; the test suite sets it once in TestMain to re-execute the
// test binary as the plugin child. It is the only seam through which Load
// can pass -test.run to the helper child.
var spawnArgs []string

// Load resolves, spawns, handshakes, and registers one plugin per entry,
// in order: resolve, dedupe, spawn, initialize, register. Failures are
// nonfatal — each rejected entry contributes one error and later entries
// still load. The returned Loader owns every successfully spawned child
// (children rejected at handshake or registration are terminated
// immediately) and must be closed by the caller.
func Load(ctx context.Context, configPath string, entries []string, register func(database.Shim) error) (*Loader, []error) {
	return load(ctx, configPath, entries, nil, register)
}

// LoadPinned is Load with per-entry trust verification: immediately
// before each child would be spawned, the entry's canonical path is
// looked up in trust and the configured SHA-256 digest is verified
// against the current bytes. A pinned entry whose digest cannot be
// computed or does not match is refused at that point — the child never
// executes — and contributes one error naming the entry with the
// expected and actual digests; later entries still load. Entries
// without a trust record load unpinned for compatibility.
func LoadPinned(ctx context.Context, configPath string, entries []string, trust map[string]string, register func(database.Shim) error) (*Loader, []error) {
	return load(ctx, configPath, entries, trust, register)
}

func load(ctx context.Context, configPath string, entries []string, trust map[string]string, register func(database.Shim) error) (*Loader, []error) {
	loader := &Loader{configPath: configPath, sessions: map[*sessionProxy]struct{}{}}
	var errs []error
	seen := map[string]struct{}{}
	for _, entryText := range entries {
		item := &entry{configEntry: entryText, register: register}
		fail := func(err error) {
			item.err = err
			errs = append(errs, err)
		}
		path, err := resolvePluginExecutable(entryText, configPath)
		if err != nil {
			fail(fmt.Errorf("plugin %q: %w", entryText, err))
			loader.trackEntry(item)
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue // canonical duplicates are silently skipped
		}
		seen[path] = struct{}{}
		item.path = path

		if pin, pinned := trust[path]; pinned {
			item.trust = pin
			digest, err := SHA256File(path)
			if err != nil {
				fail(fmt.Errorf("plugin %q: verifying pinned sha256: %v; refusing to start", entryText, err))
				loader.trackEntry(item)
				continue
			}
			if !strings.EqualFold(digest, pin) {
				fail(fmt.Errorf("plugin %q: pinned executable changed: expected sha256 %s, got %s; refusing to start", entryText, pin, digest))
				loader.trackEntry(item)
				continue
			}
		}

		client, err := spawn(path, spawnArgs...)
		if err != nil {
			fail(fmt.Errorf("plugin %q: %w", entryText, err))
			loader.trackEntry(item)
			continue
		}
		loader.trackClient(client)
		item.client = client

		var handshake initializeResult
		initStart := time.Now()
		if err := client.Call(ctx, methodInitialize, initializeParams{
			ProtocolVersion:  ProtocolVersion,
			WorkbenchVersion: workbenchVersion,
		}, &handshake); err != nil {
			client.setInitDuration(time.Since(initStart))
			fail(fmt.Errorf("plugin %q: initialize: %w", entryText, err))
			_ = client.Close()
			loader.trackEntry(item)
			continue
		}
		client.setInitDuration(time.Since(initStart))
		if handshake.ProtocolVersion != ProtocolVersion {
			fail(fmt.Errorf("plugin %q: protocol version %d, want %d", entryText, handshake.ProtocolVersion, ProtocolVersion))
			_ = client.Close()
			loader.trackEntry(item)
			continue
		}
		// The plugin has identified itself; operation errors now carry
		// this host-known identity, never the child's data claims.
		client.SetPlugin(handshake.Capabilities.Name)
		client.setProtocolVersion(handshake.ProtocolVersion)
		item.shim = newShim(client, handshake.Capabilities, loader)
		if err := register(item.shim); err != nil {
			fail(fmt.Errorf("plugin %q: %w", entryText, err))
			_ = client.Close()
			loader.trackEntry(item)
			continue
		}
		item.registered = true
		loader.trackEntry(item)
	}
	return loader, errs
}

// ResolveExecutable maps a plugin entry to its canonical executable
// path with the exact startup resolution and allowlist, without
// spawning anything: bare names resolve through PATH, entries with a
// path separator resolve relative to the config file's directory (or
// the working directory when configPath is empty, for explicit
// operands), and the result must be a regular file with at least one
// executable permission bit. It is the narrow exported face of the
// loader's resolver so CLI tooling never duplicates the rules.
func ResolveExecutable(entry, configPath string) (string, error) {
	return resolvePluginExecutable(entry, configPath)
}

// resolvePluginExecutable maps a config entry to its canonical plugin
// path: entries without a path separator resolve through PATH; entries
// with one resolve relative to the config file's directory. The result
// must be a regular file with at least one executable permission bit.
func resolvePluginExecutable(entry, configPath string) (string, error) {
	var path string
	switch {
	case filepath.Base(entry) == entry:
		resolved, err := exec.LookPath(entry)
		if err != nil {
			return "", err
		}
		path = resolved
	case filepath.IsAbs(entry):
		path = entry
	default:
		path = filepath.Join(filepath.Dir(configPath), entry)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", resolved)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", resolved)
	}
	return resolved, nil
}

func (l *Loader) trackClient(client *Client) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clients = append(l.clients, client)
}

// trackEntry records one configured entry in config order. Entries are
// appended exactly once, at the end of their load (or rejection).
func (l *Loader) trackEntry(item *entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, item)
}

func (l *Loader) trackSession(proxy *sessionProxy) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sessions == nil {
		l.sessions = map[*sessionProxy]struct{}{}
	}
	l.sessions[proxy] = struct{}{}
}

func (l *Loader) unregisterSession(proxy *sessionProxy) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.sessions, proxy)
}

// Snapshots returns one immutable diagnostic snapshot per spawned
// child, in load order. Safe to call at any time, including after
// Close: the loader retains every client reference solely so final
// diagnostics stay inspectable. Each snapshot is a fresh copy — mutating
// a returned Snapshot or its Stderr slice never affects the loader or
// its children.
func (l *Loader) Snapshots() []Snapshot {
	l.mu.Lock()
	clients := make([]*Client, len(l.clients))
	copy(clients, l.clients)
	l.mu.Unlock()
	snapshots := make([]Snapshot, 0, len(clients))
	for _, client := range clients {
		snapshots = append(snapshots, client.Snapshot())
	}
	return snapshots
}

// Close shuts down every live session (their idempotent Close sends the
// close RPC), then closes every plugin child. Idempotent: the second call
// returns nil. Safe after children have exited and while calls are still
// pending — pending calls fail with the terminal error. The client
// references are retained after Close so Snapshots keeps reporting final
// diagnostics; Client.Close is idempotent, so a later Close never
// touches them again.
func (l *Loader) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	sessions := l.sessions
	l.sessions = map[*sessionProxy]struct{}{}
	clients := l.clients
	l.mu.Unlock()

	var errs []error
	for proxy := range sessions {
		if err := proxy.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, client := range clients {
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
