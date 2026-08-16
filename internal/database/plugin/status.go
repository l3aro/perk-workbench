package plugin

import (
	"time"
)

// Status is one configured plugin entry's live state, read without
// spawning, mutating, or blocking on protocol I/O. It carries the
// configured entry text and canonical path, the host-known plugin
// identity, the perk protocol version negotiated at the last successful
// handshake, the trust state (configured fingerprint), the child's
// process and exit state, initialize duration, in-flight count, the
// last terminal/structured failure, and the bounded stderr tail.
// Every field is a fresh copy; mutating a returned Status or its
// Stderr slice never affects the loader or its children.
type Status struct {
	// Entry is the configured entry text (relative or bare name) whose
	// canonical path resolves to Path.
	Entry string `json:"entry"`
	// Path is the canonical executable path; "" when resolution has
	// never succeeded.
	Path string `json:"path"`
	// Plugin is the host-known identity claimed at the last successful
	// initialize handshake; "" before any successful handshake.
	Plugin string `json:"plugin"`
	// ProtocolVersion is the perk/v1 protocol version negotiated at the
	// last successful handshake; 0 when no handshake has succeeded.
	ProtocolVersion int `json:"protocol_version"`
	// Trusted reports whether the entry is pinned in the config trust
	// map; Fingerprint is the configured sha256 pin.
	Trusted     bool   `json:"trusted"`
	Fingerprint string `json:"fingerprint,omitempty"`
	// PID is the current child's pid; 0 once reaped.
	PID int `json:"pid"`
	// Running reports whether the current child is not yet reaped.
	Running bool `json:"running"`
	// ExitStatus is the current child's exit code once reaped; -1 while
	// running or signal-killed.
	ExitStatus int `json:"exit_status"`
	// InitDuration is the last initialize RPC duration, on success or
	// failure; 0 when no handshake has completed.
	InitDuration time.Duration `json:"init_duration"`
	// InFlight is the number of pending requests on the current child at
	// status time.
	InFlight int `json:"in_flight"`
	// Error is the last terminal/structured failure: the load rejection
	// or restart failure text, or the current child's terminal error.
	Error string `json:"error,omitempty"`
	// Stderr is the current child's newest bounded diagnostics tail.
	Stderr []string `json:"stderr"`
}

// Statuses returns one immutable status per configured entry, in config
// order — including entries rejected at load and entries whose child
// crashed. Safe to call at any time, including during Restart and after
// Close; status reads never spawn, mutate, or exchange protocol traffic.
func (l *Loader) Statuses() []Status {
	l.mu.Lock()
	entries := make([]*entry, len(l.entries))
	copy(entries, l.entries)
	l.mu.Unlock()

	statuses := make([]Status, 0, len(entries))
	for _, item := range entries {
		statuses = append(statuses, item.status())
	}
	return statuses
}

// status snapshots one entry: the immutable identity and trust fields,
// the entry's last failure, and the current client's diagnostics. The
// client snapshot is taken under the client lock only; the stderr tail
// is copied under the drain lock.
func (e *entry) status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	status := Status{
		Entry:       e.configEntry,
		Path:        e.path,
		Trusted:     e.trust != "",
		Fingerprint: e.trust,
	}
	if e.err != nil {
		status.Error = e.err.Error()
	}
	if e.client != nil {
		snap := e.client.Snapshot()
		status.Plugin = snap.Plugin
		status.ProtocolVersion = snap.ProtocolVersion
		status.PID = snap.PID
		status.Running = snap.Running
		status.ExitStatus = snap.ExitStatus
		status.InitDuration = snap.InitDuration
		status.InFlight = snap.InFlight
		if status.Error == "" {
			status.Error = snap.Error
		}
		status.Stderr = snap.Stderr
	}
	return status
}
