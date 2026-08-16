package plugin

import (
	"context"
	"errors"

	"github.com/l3aro/perk-workbench/internal/database"
)

// Lifecycle phase names, stable in JSON and human output. They are the
// shared vocabulary of the inspect lifecycle, reused by the CLI reports
// and the TUI trust preview.
const (
	PhaseResolve    = "resolve"
	PhaseInitialize = "initialize"
	PhaseProtocol   = "protocol"
	PhaseRegister   = "register"
	PhaseShutdown   = "shutdown"
	PhaseOK         = "ok"
)

// InspectResult is the outcome of one inspect lifecycle: the resolved
// canonical path, the driver advertisement, the final diagnostic
// snapshot, and — when the lifecycle failed — the failing phase and its
// error text.
type InspectResult struct {
	// Path is the canonical executable path once resolution succeeded.
	Path string
	// Capabilities is the driver advertisement once the initialize
	// handshake succeeded, or nil when the handshake failed.
	Capabilities *database.Capabilities
	// Snapshot is the final diagnostic snapshot, taken after the child
	// was closed: canonical path, init duration, exit/running state, and
	// the bounded stderr tail. Nil when the child never spawned.
	Snapshot *Snapshot
	// Phase is the failing lifecycle phase — resolve, initialize,
	// protocol, register, or shutdown — or PhaseOK when every phase
	// passed.
	Phase string
	// Error is the failure text when Phase is not PhaseOK.
	Error string
}

// Inspect runs one plugin through the full resolve, initialize and
// registration-validation, shutdown lifecycle with its own Loader, so
// items never mutate or contaminate each other or the global driver
// registry. configPath is the config file path used to resolve relative
// entries ("" for explicit operands, which resolve against the working
// directory). Registration validation uses the side-effect-free
// database.ValidateShim — no global driver is ever installed. The
// snapshot is taken after Loader.Close so it reflects the final
// exit/running state, and it remains available because the loader
// retains its clients.
func Inspect(ctx context.Context, entry, configPath string) InspectResult {
	result := InspectResult{}
	path, err := ResolveExecutable(entry, configPath)
	if err != nil {
		result.Phase = PhaseResolve
		result.Error = err.Error()
		return result
	}
	result.Path = path

	var registerErr error
	loader, errs := Load(ctx, configPath, []string{path}, func(shim database.Shim) error {
		caps := shim.Capabilities()
		result.Capabilities = &caps
		registerErr = database.ValidateShim(shim)
		return registerErr
	})
	closeErr := loader.Close()
	snapshots := loader.Snapshots()
	if len(snapshots) > 0 {
		snapshot := snapshots[0]
		result.Snapshot = &snapshot
	}

	switch {
	case len(errs) > 0:
		switch {
		case registerErr != nil:
			result.Phase = PhaseRegister
			result.Error = registerErr.Error()
		case result.Snapshot != nil && !benignProtocolError(result.Snapshot.Error):
			// The child hit a terminal protocol or process failure: a
			// malformed response stream, a crash, or a premature exit
			// mid-handshake. Its text is already part of the snapshot.
			result.Phase = PhaseProtocol
			result.Error = result.Snapshot.Error
		default:
			result.Phase = PhaseInitialize
			result.Error = errors.Join(errs...).Error()
		}
	case closeErr != nil:
		result.Phase = PhaseShutdown
		result.Error = closeErr.Error()
	default:
		result.Phase = PhaseOK
	}
	return result
}

// benignProtocolError reports whether a snapshot's terminal error text
// is the clean-close artifact rather than a protocol or process
// failure: EOF on the response stream after a normal child exit, or the
// client-closed marker a clean close leaves behind.
func benignProtocolError(errText string) bool {
	return errText == "" || errText == "EOF" || errText == "perk/v1: client closed"
}
