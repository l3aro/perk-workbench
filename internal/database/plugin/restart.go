package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/l3aro/perk-workbench/internal/database"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// entry is one configured plugin invocation, its current client generation,
// and its last failure.
type entry struct {
	mu sync.Mutex

	config     Entry
	path       string
	trust      string
	register   func(database.Shim) error
	registered bool
	shim       *shim
	client     *Client
	err        error
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

	executable := item.config.Executable
	if executable == "" {
		executable = item.config.Config
	}
	path, err := resolvePluginExecutable(executable, l.configPath)
	if err != nil {
		return item.fail(fmt.Errorf("plugin %q: %w", item.config.identity(), err))
	}
	if item.trust != "" && !item.config.Builtin {
		digest, err := SHA256File(path)
		if err != nil {
			return item.fail(fmt.Errorf("plugin %q: verifying pinned sha256: %v; refusing to start", item.config.identity(), err))
		}
		if digest != item.trust {
			return item.fail(fmt.Errorf("plugin %q: pinned executable changed: expected sha256 %s, got %s; refusing to start", item.config.identity(), item.trust, digest))
		}
	}

	args := append(append([]string{}, item.config.Args...), spawnArgs...)
	client, err := spawn(path, args...)
	if err != nil {
		return item.fail(fmt.Errorf("plugin %q: %w", item.config.identity(), err))
	}

	var handshake initializeResult
	initStart := time.Now()
	if err := client.Call(ctx, methodInitialize, initializeParams{
		ProtocolVersion:  ProtocolVersion,
		WorkbenchVersion: workbenchVersion,
	}, &handshake); err != nil {
		client.setInitDuration(time.Since(initStart))
		_ = client.Close()
		return item.fail(fmt.Errorf("plugin %q: initialize: %w", item.config.identity(), err))
	}
	client.setInitDuration(time.Since(initStart))
	if handshake.ProtocolVersion != ProtocolVersion {
		_ = client.Close()
		return item.fail(fmt.Errorf("plugin %q: protocol version %d, want %d", item.config.identity(), handshake.ProtocolVersion, ProtocolVersion))
	}
	client.SetPlugin(handshake.Capabilities.Name)
	client.setProtocolVersion(handshake.ProtocolVersion)
	source := "external"
	if item.config.Builtin {
		source = "builtin"
	}
	replacement := newShim(client, handshake.Capabilities, l, source)
	if item.registered {
		if err := database.ValidateShimReplacement(replacement); err != nil {
			_ = client.Close()
			return item.fail(fmt.Errorf("plugin %q: %w", item.config.identity(), err))
		}
		if replacement.Capabilities().Name != item.shim.Capabilities().Name {
			_ = client.Close()
			return item.fail(fmt.Errorf("plugin %q: identity changed on restart: %q, want %q", item.config.identity(), replacement.Capabilities().Name, item.shim.Capabilities().Name))
		}
	} else if item.register == nil {
		_ = client.Close()
		return item.fail(fmt.Errorf("plugin %q: no registration callback; cannot restart", item.config.identity()))
	}

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
			return item.fail(fmt.Errorf("plugin %q: %w", item.config.identity(), err))
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

// findEntry resolves the configured identity or canonical executable path.
func (l *Loader) findEntry(identifier string) (*entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, errors.New("plugin loader is closed")
	}
	var match *entry
	for _, item := range l.entries {
		if identifier == item.config.identity() || (item.path != "" && identifier == item.path) {
			if match != nil {
				return nil, fmt.Errorf("ambiguous configured plugin entry %q", identifier)
			}
			match = item
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no configured plugin entry %q", identifier)
	}
	return match, nil
}

func (e *entry) fail(err error) error {
	e.err = err
	return err
}

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
			return item.config.identity(), true
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
