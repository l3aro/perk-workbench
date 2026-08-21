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
	// Entry is the configured identity (builtin name or external path).
	Entry           string        `json:"entry"`
	Display         string        `json:"display,omitempty"`
	Source          string        `json:"source"`
	Executable      string        `json:"executable"`
	Args            []string      `json:"args,omitempty"`
	Builtin         bool          `json:"builtin"`
	Path            string        `json:"path"`
	Plugin          string        `json:"plugin"`
	ProtocolVersion int           `json:"protocol_version"`
	Trusted         bool          `json:"trusted"`
	Fingerprint     string        `json:"fingerprint,omitempty"`
	PID             int           `json:"pid"`
	Running         bool          `json:"running"`
	ExitStatus      int           `json:"exit_status"`
	InitDuration    time.Duration `json:"init_duration"`
	InFlight        int           `json:"in_flight"`
	Error           string        `json:"error,omitempty"`
	Stderr          []string      `json:"stderr"`
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

func (e *entry) status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	source := "external"
	if e.config.Builtin {
		source = "builtin"
	}
	status := Status{
		Entry:       e.config.identity(),
		Display:     e.config.display(),
		Source:      source,
		Executable:  e.config.Executable,
		Args:        append([]string(nil), e.config.Args...),
		Builtin:     e.config.Builtin,
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
