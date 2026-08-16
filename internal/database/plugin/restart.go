package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// entry is one configured plugin entry: its identity, configured pin,
// current client generation, and last failure. The loader keeps one
// entry per unique canonical path, in config order, including entries
// rejected at load — a rejected entry stays inspectable through
// Statuses and recoverable through Restart.
type entry struct {
	// mu serializes restarts of this entry and guards the mutable
	// lifecycle fields: client, shim, registered, err.
	mu sync.Mutex

	configEntry string // configured entry text
	path        string // canonical executable path; "" when never resolved
	trust       string // configured pin (lowercase sha256); "" when unpinned
	register    func(database.Shim) error

	registered bool    // a shim was installed (through register)
	shim       *shim   // the registered shim; client pointer swaps on restart
	client     *Client // current client generation (old ones stay in loader.clients)
	err        error   // last load rejection or restart failure; nil while healthy
}

// Restart recovers exactly one configured entry — loaded, rejected, or
// crashed — identified by its configured entry text or canonical path.
// The pin is re-verified immediately before the replacement spawns
// (drift fails closed and nothing executes); the old child is closed
// and reaped, the replacement is initialized and validated, and the
// client used by future session opens is swapped atomically — the
// global driver registration is never touched. Sessions opened before
// the restart keep their old client generation and fail deterministically
// rather than jumping to the replacement. A failed restart leaves the
// previous state intact and the entry's failure text updated.
//
// Restart is safe to call concurrently with Statuses, other Restarts of
// other entries, and Close. Close racing Restart wins: the restart
// aborts at the swap point and its replacement child is closed, so no
// child is ever left behind. Restarts of the same entry are serialized.
func (l *Loader) Restart(ctx context.Context, identifier string) error {
	item, err := l.findEntry(identifier)
	if err != nil {
		return err
	}

	item.mu.Lock()
	defer item.mu.Unlock()

	// Re-resolve the configured entry and re-verify its pin before
	// anything executes; a drifted pin fails closed exactly like load.
	path, err := resolvePluginExecutable(item.configEntry, l.configPath)
	if err != nil {
		return item.fail(fmt.Errorf("plugin %q: %w", item.configEntry, err))
	}
	if item.trust != "" {
		digest, err := SHA256File(path)
		if err != nil {
			return item.fail(fmt.Errorf("plugin %q: verifying pinned sha256: %v; refusing to start", item.configEntry, err))
		}
		if !strings.EqualFold(digest, item.trust) {
			return item.fail(fmt.Errorf("plugin %q: pinned executable changed: expected sha256 %s, got %s; refusing to start", item.configEntry, item.trust, digest))
		}
	}

	client, err := spawn(path, spawnArgs...)
	if err != nil {
		return item.fail(fmt.Errorf("plugin %q: %w", item.configEntry, err))
	}

	var handshake initializeResult
	initStart := time.Now()
	if err := client.Call(ctx, methodInitialize, initializeParams{
		ProtocolVersion:  ProtocolVersion,
		WorkbenchVersion: workbenchVersion,
	}, &handshake); err != nil {
		client.setInitDuration(time.Since(initStart))
		_ = client.Close()
		return item.fail(fmt.Errorf("plugin %q: initialize: %w", item.configEntry, err))
	}
	client.setInitDuration(time.Since(initStart))
	if handshake.ProtocolVersion != ProtocolVersion {
		_ = client.Close()
		return item.fail(fmt.Errorf("plugin %q: protocol version %d, want %d", item.configEntry, handshake.ProtocolVersion, ProtocolVersion))
	}
	client.SetPlugin(handshake.Capabilities.Name)
	client.setProtocolVersion(handshake.ProtocolVersion)

	replacement := newShim(client, handshake.Capabilities, l)
	if item.registered {
		// The driver is already installed under the registered identity:
		// the replacement must validate and keep that identity, and only
		// the transport client swaps — the global registration is never
		// replaced or duplicated. The entry's own registration is
		// excluded from the conflict check.
		if err := database.ValidateShimReplacement(replacement); err != nil {
			_ = client.Close()
			return item.fail(fmt.Errorf("plugin %q: %w", item.configEntry, err))
		}
		if replacement.Capabilities().Name != item.shim.Capabilities().Name {
			_ = client.Close()
			return item.fail(fmt.Errorf("plugin %q: identity changed on restart: %q, want %q", item.configEntry, replacement.Capabilities().Name, item.shim.Capabilities().Name))
		}
	} else if item.register == nil {
		_ = client.Close()
		return item.fail(fmt.Errorf("plugin %q: no registration callback; cannot restart", item.configEntry))
	}

	// Atomically publish the replacement and reap the old child. Close
	// racing this swap wins: the loader is already closed, so the fresh
	// child is closed and reaped here and nothing is published. An
	// entry rejected at load registers here, under the loader lock, so
	// Close can never observe a global driver identity installed for a
	// closed loader (the registration is not undoable).
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = client.Close()
		return errors.New("plugin loader is closed")
	}
	if !item.registered {
		if err := item.register(replacement); err != nil {
			l.mu.Unlock()
			_ = client.Close()
			return item.fail(fmt.Errorf("plugin %q: %w", item.configEntry, err))
		}
		item.registered = true
		item.shim = replacement
	}
	l.clients = append(l.clients, client)
	oldClient := item.client
	item.client = client
	item.shim.client.Store(client)
	item.err = nil
	item.path = path
	l.mu.Unlock()

	if oldClient != nil {
		_ = oldClient.Close()
	}
	return nil
}

// findEntry resolves identifier — a configured entry text or canonical
// path — to exactly one entry.
func (l *Loader) findEntry(identifier string) (*entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, errors.New("plugin loader is closed")
	}
	var match *entry
	for _, item := range l.entries {
		if identifier == item.configEntry || (item.path != "" && identifier == item.path) {
			match = item
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no configured plugin entry %q", identifier)
	}
	return match, nil
}

// fail records err as the entry's last failure and returns it.
func (e *entry) fail(err error) error {
	e.err = err
	return err
}

// EntryForService reports the configured entry text of the plugin
// child backing service, or "" when the service is not a live session
// of this loader's current client generation. Old generations (sessions
// opened before a restart) are deliberately not matched: they fail
// deterministically and a restart of their entry would recover a fresh
// connection, not this one.
func (l *Loader) EntryForService(service sharedsql.Service) (string, bool) {
	proxy := unwrapSessionProxy(service)
	if proxy == nil {
		return "", false
	}
	client := proxy.client
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, tracked := l.sessions[proxy]; !tracked {
		return "", false
	}
	for _, item := range l.entries {
		if item.client == client {
			return item.configEntry, true
		}
	}
	return "", false
}

// unwrapSessionProxy strips the capability wrappers to the underlying
// session proxy. nil for any service this loader did not create.
func unwrapSessionProxy(service sharedsql.Service) *sessionProxy {
	switch s := service.(type) {
	case *sessionProxy:
		return s
	case *rowWriter:
		return s.proxy
	case *documentWriter:
		return s.proxy
	case *documentRowWriter:
		return s.proxy
	}
	return nil
}
