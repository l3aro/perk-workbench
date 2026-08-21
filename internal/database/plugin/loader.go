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

// Entry is one configured child-process invocation. Config is the stable
// configured identity shown by status and commands; Executable and Args are
// the process identity used for deduplication. Builtin entries are self-hosted
// and never use SHA256.
type Entry struct {
	Config     string
	Display    string
	Executable string
	Args       []string
	SHA256     string
	Builtin    bool
}

func (e Entry) identity() string {
	config := e.Config
	if config == "" {
		config = e.Executable
	}
	return config
}

func (e Entry) display() string {
	if e.Display != "" {
		return e.Display
	}
	return e.identity()
}

func (e Entry) key(path string) string {
	return path + "\x00" + strings.Join(e.Args, "\x00")
}

// Loader owns the lifecycle of every configured plugin entry: the
// spawned child, the registered shim, and every session opened in them.
// Close is the single idempotent cleanup path.
type Loader struct {
	mu         sync.Mutex
	configPath string
	entries    []*entry
	clients    []*Client
	sessions   map[*sessionProxy]struct{}
	closed     bool
}

// spawnArgs is appended after each Entry.Args for the test helper seam.
var spawnArgs []string

// Load resolves, spawns, handshakes, and registers structured entries.
func Load(ctx context.Context, configPath string, entries []Entry, register func(database.Shim) error) (*Loader, []error) {
	return load(ctx, configPath, entries, register)
}

func load(ctx context.Context, configPath string, entries []Entry, register func(database.Shim) error) (*Loader, []error) {
	loader := &Loader{configPath: configPath, sessions: map[*sessionProxy]struct{}{}}
	var errs []error
	seen := map[string]struct{}{}
	for _, configured := range entries {
		item := &entry{config: configured, register: register}
		fail := func(err error) {
			item.err = err
			errs = append(errs, err)
		}
		executable := configured.Executable
		if executable == "" {
			executable = configured.Config
		}
		path, err := resolvePluginExecutable(executable, configPath)
		if err != nil {
			fail(fmt.Errorf("plugin %q: %w", configured.identity(), err))
			loader.trackEntry(item)
			continue
		}
		if _, duplicate := seen[configured.key(path)]; duplicate {
			continue
		}
		seen[configured.key(path)] = struct{}{}
		item.path = path

		if configured.SHA256 != "" && !configured.Builtin {
			item.trust = configured.SHA256
			digest, err := SHA256File(path)
			if err != nil {
				fail(fmt.Errorf("plugin %q: verifying pinned sha256: %v; refusing to start", configured.identity(), err))
				loader.trackEntry(item)
				continue
			}
			if digest != configured.SHA256 {
				fail(fmt.Errorf("plugin %q: pinned executable changed: expected sha256 %s, got %s; refusing to start", configured.identity(), configured.SHA256, digest))
				loader.trackEntry(item)
				continue
			}
		}

		args := append(append([]string{}, configured.Args...), spawnArgs...)
		client, err := spawn(path, args...)
		if err != nil {
			fail(fmt.Errorf("plugin %q: %w", configured.identity(), err))
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
			fail(fmt.Errorf("plugin %q: initialize: %w", configured.identity(), err))
			_ = client.Close()
			loader.trackEntry(item)
			continue
		}
		client.setInitDuration(time.Since(initStart))
		if handshake.ProtocolVersion != ProtocolVersion {
			fail(fmt.Errorf("plugin %q: protocol version %d, want %d", configured.identity(), handshake.ProtocolVersion, ProtocolVersion))
			_ = client.Close()
			loader.trackEntry(item)
			continue
		}
		client.SetPlugin(handshake.Capabilities.Name)
		client.setProtocolVersion(handshake.ProtocolVersion)
		source := "external"
		if configured.Builtin {
			source = "builtin"
		}
		item.shim = newShim(client, handshake.Capabilities, loader, source)
		if err := register(item.shim); err != nil {
			fail(fmt.Errorf("plugin %q: %w", configured.identity(), err))
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
