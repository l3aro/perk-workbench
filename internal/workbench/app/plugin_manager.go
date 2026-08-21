package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
	"github.com/l3aro/perk-workbench/internal/log"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// Plugin manager views, in the order the user walks them.
const (
	pluginViewMenu    = "menu"
	pluginViewAdd     = "add"
	pluginViewPreview = "preview"
	pluginViewRemove  = "remove"
	pluginViewStatus  = "status"
)

// pluginInspectTimeout bounds one async inspect in the plugin manager.
// Every preview and approve re-runs the full resolve/initialize/
// shutdown lifecycle of the executable.
var pluginInspectTimeout = 30 * time.Second

// pluginManager is the root-owned plugin management overlay reachable
// from the command palette. It walks the user through adding a plugin
// (enter executable → async inspect/hash preview → explicit enable →
// async re-verify → atomic save), removing one (pick entry → confirm →
// atomic save), and — through the injected live PluginControl —
// inspecting live child status and restarting one entry (pick entry →
// confirm → async restart, reconnecting when the entry backs the
// current connection). It never mutates the live driver registry; add
// and remove take effect on the next start, and restart swaps only the
// transport client of the registered driver.
type pluginManager struct {
	// view is the current step: menu, add, preview, remove, or status.
	view string
	// cursor selects the menu entry.
	cursor int
	// input is the executable path being entered in the add step.
	input []rune
	// preview is the resolved add preview awaiting confirmation.
	preview *pluginAddPreview
	// err is the last failure shown in the current view.
	err string
	// busy reports an in-flight async inspect, approve, or restart.
	busy bool
	// removeList is the configured entries in config order.
	removeList []string
	// removeCursor selects the entry to remove.
	removeCursor int
	// removeConfirm is the open removal confirmation dialog.
	removeConfirm *confirmationDialog
	// statuses is the last explicit status snapshot, in config order.
	statuses []plugin.Status
	// statusCursor selects the entry whose details render.
	statusCursor int
	// restartConfirm is the open restart confirmation dialog.
	restartConfirm *confirmationDialog
}

// pluginAddPreview is the declarative trust preview shown before
// enabling: canonical path, identity/display, target prefixes, query
// language, write interfaces, and the SHA-256 fingerprint of the exact
// bytes. It never carries form values, credentials, statements, or
// plugin stderr.
type pluginAddPreview struct {
	// Entry is the executable path the user typed; the approve step
	// re-resolves and re-inspects it with the previewed digest.
	Entry    string
	Path     string
	Name     string
	Display  string
	Targets  []string
	Language string
	Writes   string
	SHA256   string
}

// pluginPreviewMsg is the outcome of the async preview command.
type pluginPreviewMsg struct {
	preview *pluginAddPreview
	err     error
}

// pluginApproveMsg is the outcome of the async approve command. On
// success it carries the persisted descriptor state so the model
// goroutine can apply it to appConfig without racing the command goroutine.
type pluginApproveMsg struct {
	path    string
	plugins []PluginConfig
	err     error
}

// pluginRestartMsg is the outcome of the async restart command: the
// entry identifier, the restart error when it failed, and — when the
// entry backed the current connection and the restart succeeded — the
// full connection target to reopen through the normal async open path.
type pluginRestartMsg struct {
	identifier string
	err        error
	reconnect  bool
	target     string
}

// pluginRestartCmd recovers one configured plugin entry through the
// injected control. The restart runs first: a failed restart leaves the
// current service and model state untouched — a healthy connection
// stays usable, a crashed one stays crashed and actionable. Only after
// the replacement is validated and swapped is the old-generation
// service closed, and only when the entry backs the current connection
// does the message carry the full connection target for the model
// goroutine to reconnect through the normal async open path. Closing
// the old generation normally fails with the expected terminal
// dead-child error (its client was already closed by the swap); that is
// never allowed to turn the successful recovery into a failure, while a
// genuinely relevant close error is logged for diagnostics.
func (m Model) pluginRestartCmd(identifier string, service sharedsql.Service, target string, reconnect bool) tea.Cmd {
	appCtx := m.appContext
	control := m.pluginControl
	secrets := m.connectionSecrets(target)
	return func() tea.Msg {
		if err := control.Restart(appCtx, identifier); err != nil {
			// The message carries the known restart target so the model
			// goroutine can scrub it from the failure text.
			return pluginRestartMsg{identifier: identifier, err: err, target: target}
		}
		if reconnect && service != nil {
			if err := service.Close(); err != nil && !plugin.IsTerminal(err) {
				// The close error is plugin-provided text: redact and
				// scrub the target before it reaches the log and the
				// notification pipeline.
				log.Error("close superseded plugin session", errors.New(scrubPluginTarget(redactCredentials(err.Error(), secrets), target)))
			}
		}
		return pluginRestartMsg{identifier: identifier, reconnect: reconnect, target: target}
	}
}

func newPluginManager() *pluginManager {
	removeList := make([]string, 0, len(appConfig.Plugins))
	for _, descriptor := range appConfig.Plugins {
		if descriptor.Builtin != "" {
			removeList = append(removeList, descriptor.Builtin)
		} else {
			removeList = append(removeList, descriptor.Path)
		}
	}
	return &pluginManager{
		view:       pluginViewMenu,
		removeList: removeList,
	}
}

// pluginPreviewCmd runs the same resolve, trust-check, inspect, and
// hash lifecycle as the CLI preview, asynchronously. A pinned
// executable whose bytes drifted is refused before anything spawns.
func (m Model) pluginPreviewCmd(entry string) tea.Cmd {
	configPath := m.configPath
	appCtx := m.appContext
	return func() tea.Msg {
		path, digest, err := inspectPluginForPreview(configPath, entry)
		if err != nil {
			return pluginPreviewMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(appCtx, pluginInspectTimeout)
		defer cancel()
		result := plugin.Inspect(ctx, path, "")
		if result.Phase != plugin.PhaseOK {
			return pluginPreviewMsg{err: fmt.Errorf("%s: %s", result.Phase, result.Error)}
		}
		if digest == "" {
			digest, err = plugin.SHA256File(path)
			if err != nil {
				return pluginPreviewMsg{err: err}
			}
		}
		preview := &pluginAddPreview{Entry: entry, Path: path, SHA256: digest}
		caps := result.Capabilities
		preview.Name = caps.Name
		preview.Display = caps.Display
		for _, pattern := range caps.Targets {
			preview.Targets = append(preview.Targets, pattern.Prefix)
		}
		preview.Language = "sql" // the legacy default for an absent advertisement
		if caps.QueryLanguage != nil && strings.TrimSpace(caps.QueryLanguage.Name) != "" {
			preview.Language = caps.QueryLanguage.Name
		}
		switch {
		case caps.WriteCapabilities.RowWriter && caps.WriteCapabilities.Document != nil:
			preview.Writes = "row+document"
		case caps.WriteCapabilities.RowWriter:
			preview.Writes = "row"
		case caps.WriteCapabilities.Document != nil:
			preview.Writes = "document"
		default:
			preview.Writes = "none"
		}
		return pluginPreviewMsg{preview: preview}
	}
}

// pluginApproveCmd re-runs the exact preview lifecycle with the
// previewed digest: resolve, trust-check, inspect, and hash must all
// pass and the fresh digest must match the previewed one, and only then
// is the plugin pinned atomically. It returns the persisted state in
// the message; the model goroutine applies it.
func (m Model) pluginApproveCmd(entry, previewDigest string) tea.Cmd {
	configPath := m.configPath
	appCtx := m.appContext
	return func() tea.Msg {
		path, digest, err := inspectPluginForPreview(configPath, entry)
		if err != nil {
			return pluginApproveMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(appCtx, pluginInspectTimeout)
		defer cancel()
		result := plugin.Inspect(ctx, path, "")
		if result.Phase != plugin.PhaseOK {
			return pluginApproveMsg{err: fmt.Errorf("%s: %s", result.Phase, result.Error)}
		}
		if digest == "" {
			digest, err = plugin.SHA256File(path)
			if err != nil {
				return pluginApproveMsg{err: err}
			}
		}
		if !strings.EqualFold(digest, previewDigest) {
			return pluginApproveMsg{err: fmt.Errorf("executable changed since preview (sha256 %s); review the preview and approve again", digest)}
		}
		plugins, _, err := savePlugin(configPath, path, digest)
		if err != nil {
			return pluginApproveMsg{err: err}
		}
		return pluginApproveMsg{path: path, plugins: plugins}
	}
}

// inspectPluginForPreview is the shared body of the preview and approve
// commands: resolve the executable and refuse a pinned mismatch before
// anything spawns. The returned digest is the pinned file's fingerprint
// when a pin exists ("" when unpinned, so the caller hashes after the
// inspect lifecycle).
func inspectPluginForPreview(configPath, entry string) (path, digest string, err error) {
	path, err = plugin.ResolveExecutable(entry, "")
	if err != nil {
		return "", "", err
	}
	if pin, pinned := ReadPluginTrust(configPath)[path]; pinned {
		digest, err = plugin.SHA256File(path)
		if err != nil {
			return "", "", fmt.Errorf("verifying pinned sha256: %w", err)
		}
		if !strings.EqualFold(digest, pin) {
			return "", "", fmt.Errorf("pinned executable changed: expected sha256 %s, got %s", pin, digest)
		}
	}
	return path, digest, nil
}

// updatePluginManager routes every message while the plugin manager
// overlay is open: async preview/approve results, the removal
// confirmation dialog, and the per-view key handling. Mouse input is
// consumed (the overlay is keyboard-driven like the other pickers).
func (m Model) updatePluginManager(message tea.Msg) (tea.Model, tea.Cmd) {
	manager := m.overlay.pluginManager
	switch msg := message.(type) {
	case pluginPreviewMsg:
		manager.busy = false
		if msg.err != nil {
			manager.err = msg.err.Error()
			manager.preview = nil
			manager.view = pluginViewPreview
			return m, nil
		}
		manager.preview = msg.preview
		manager.err = ""
		manager.view = pluginViewPreview
		return m, nil
	case pluginApproveMsg:
		manager.busy = false
		if msg.err != nil {
			manager.err = msg.err.Error()
			manager.view = pluginViewPreview
			return m, nil
		}
		appConfig.Plugins = msg.plugins
		m.overlay.pluginManager = nil
		m.setStatus("plugin enabled; restart required")
		return m, nil
	case pluginRestartMsg:
		manager.busy = false
		if msg.err != nil {
			// Redact credential material and remove the restart target
			// as a whole before the failure reaches the overlay.
			manager.err = safeText(scrubPluginTarget(redactCredentials(msg.err.Error(), m.connectionSecrets(msg.target)), msg.target))
			manager.statuses = m.pluginStatuses()
			return m, nil
		}
		if msg.reconnect {
			// The recovered entry backed the current connection: close
			// the manager and reconnect the same target through the
			// normal async open path.
			m.overlay.pluginManager = nil
			m.setStatus("plugin restarted; reconnecting")
			return m, m.openTarget(msg.target)
		}
		manager.err = ""
		manager.statuses = m.pluginStatuses()
		m.setStatus("plugin restarted")
		return m, nil
	}
	if manager.busy {
		return m, nil
	}

	// The removal confirmation dialog owns every input while visible.
	if manager.removeConfirm != nil {
		completed, action := manager.removeConfirm.Update(message, m.layout.width, m.layout.height)
		if !completed {
			return m, nil
		}
		manager.removeConfirm = nil
		if action != "remove" {
			return m, nil
		}
		entry := manager.removeList[manager.removeCursor]
		if _, _, _, err := RemovePlugin(m.configPath, entry); err != nil {
			manager.err = err.Error()
			return m, nil
		}
		m.overlay.pluginManager = nil
		m.setStatus("plugin removed; restart required")
		return m, nil
	}

	// The restart confirmation dialog owns every input while visible.
	if manager.restartConfirm != nil {
		completed, action := manager.restartConfirm.Update(message, m.layout.width, m.layout.height)
		if !completed {
			return m, nil
		}
		manager.restartConfirm = nil
		if action != "restart" {
			return m, nil
		}
		if m.pluginControl == nil {
			manager.err = "plugin live control unavailable"
			return m, nil
		}
		status := manager.statuses[manager.statusCursor]
		// An entry backing the current connection is recovered with a
		// reconnect: the dead service is closed, the child restarted,
		// and the same target reopened through the normal async open
		// path. Anything else restarts only the child.
		reconnect := false
		target := ""
		if m.Database != nil && m.connectionTarget != "" {
			if entry, ok := m.pluginControl.EntryForService(m.Database); ok && entry == status.Entry {
				reconnect = true
				target = m.connectionTarget
			}
		}
		manager.busy = true
		manager.err = ""
		return m, m.pluginRestartCmd(status.Entry, m.Database, target, reconnect)
	}

	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch manager.view {
	case pluginViewMenu:
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			m.overlay.pluginManager = nil
		case tea.KeyEnter:
			switch manager.cursor {
			case 0:
				manager.view = pluginViewAdd
				manager.input = nil
				manager.err = ""
				manager.preview = nil
			case 1:
				manager.view = pluginViewRemove
				manager.err = ""
			case 2:
				// Live status is an explicit snapshot: read once on
				// entry, refreshed only by the user (r).
				manager.view = pluginViewStatus
				manager.err = ""
				manager.statusCursor = 0
				manager.statuses = m.pluginStatuses()
			default:
				m.overlay.pluginManager = nil
			}
		case tea.KeyUp:
			manager.cursor = max(0, manager.cursor-1)
		case tea.KeyDown:
			manager.cursor = min(3, manager.cursor+1)
		default:
			switch keyPress.Keystroke() {
			case "j":
				manager.cursor = min(3, manager.cursor+1)
			case "k":
				manager.cursor = max(0, manager.cursor-1)
			}
		}
	case pluginViewAdd:
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			manager.view = pluginViewMenu
		case tea.KeyEnter:
			if len(manager.input) > 0 {
				manager.busy = true
				manager.err = ""
				return m, m.pluginPreviewCmd(string(manager.input))
			}
		case tea.KeyBackspace:
			if len(manager.input) > 0 {
				manager.input = manager.input[:len(manager.input)-1]
			}
		default:
			if stroke := keyPress.Keystroke(); len(stroke) == 1 && stroke[0] >= ' ' {
				manager.input = append(manager.input, rune(stroke[0]))
			}
		}
	case pluginViewPreview:
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			manager.view = pluginViewAdd
			manager.preview = nil
			manager.err = ""
		case tea.KeyEnter:
			if manager.preview != nil {
				manager.busy = true
				manager.err = ""
				return m, m.pluginApproveCmd(manager.preview.Entry, manager.preview.SHA256)
			}
		}
	case pluginViewRemove:
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			manager.view = pluginViewMenu
		case tea.KeyEnter:
			if len(manager.removeList) > 0 {
				manager.removeConfirm = yesNoConfirmation("Remove plugin?", manager.removeList[manager.removeCursor], "remove")
			}
		case tea.KeyUp:
			manager.removeCursor = max(0, manager.removeCursor-1)
		case tea.KeyDown:
			manager.removeCursor = min(len(manager.removeList)-1, manager.removeCursor+1)
		default:
			switch keyPress.Keystroke() {
			case "j":
				manager.removeCursor = min(len(manager.removeList)-1, manager.removeCursor+1)
			case "k":
				manager.removeCursor = max(0, manager.removeCursor-1)
			}
		}
	case pluginViewStatus:
		switch keyPress.Key().Code {
		case tea.KeyEscape:
			manager.view = pluginViewMenu
		case tea.KeyEnter:
			if len(manager.statuses) > 0 {
				status := manager.statuses[manager.statusCursor]
				label := status.Plugin
				if label == "" {
					label = status.Entry
				}
				manager.restartConfirm = yesNoConfirmation("Restart plugin?", label, "restart")
			}
		case tea.KeyUp:
			manager.statusCursor = max(0, manager.statusCursor-1)
		case tea.KeyDown:
			manager.statusCursor = min(len(manager.statuses)-1, manager.statusCursor+1)
		default:
			switch keyPress.Keystroke() {
			case "j":
				manager.statusCursor = min(len(manager.statuses)-1, manager.statusCursor+1)
			case "k":
				manager.statusCursor = max(0, manager.statusCursor-1)
			case "r":
				// Refresh is explicit: re-read the live status snapshot.
				manager.statuses = m.pluginStatuses()
				manager.err = ""
			}
		}
	}
	return m, nil
}

// pluginManagerContent renders the current plugin manager view.
func (m Model) pluginManagerContent() string {
	manager := m.overlay.pluginManager
	switch manager.view {
	case pluginViewMenu:
		return pluginManagerMenuContent(manager)
	case pluginViewAdd:
		return pluginManagerAddContent(manager)
	case pluginViewPreview:
		return pluginManagerPreviewContent(manager)
	case pluginViewStatus:
		return pluginManagerStatusContent(manager)
	default:
		return pluginManagerRemoveContent(manager)
	}
}

func pluginManagerMenuContent(manager *pluginManager) string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Plugins "))
	content.WriteString("\n\n")
	for i, option := range []string{"Add plugin", "Remove plugin", "Status", "Back"} {
		prefix := "  "
		label := option
		if i == manager.cursor {
			prefix = "> "
			label = selectedItemStyle.Render(label)
		}
		content.WriteString(prefix + label + "\n")
	}
	content.WriteString("\n")
	content.WriteString(mutedStyle.Render(" j/k or arrows navigate | enter select | esc close"))
	return content.String()
}

func pluginManagerAddContent(manager *pluginManager) string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Add plugin "))
	content.WriteString("\n\n")
	content.WriteString("  executable: ")
	content.WriteString(safeText(string(manager.input)))
	content.WriteString("▌")
	content.WriteString("\n\n")
	if manager.err != "" {
		content.WriteString(selectedItemStyle.Render(" " + manager.err))
		content.WriteString("\n")
		content.WriteString(mutedStyle.Render(" esc: back"))
		return content.String()
	}
	content.WriteString(mutedStyle.Render(" enter: inspect & fingerprint • esc: back"))
	return content.String()
}

func pluginManagerPreviewContent(manager *pluginManager) string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Plugin preview "))
	content.WriteString("\n\n")
	if manager.preview == nil {
		if manager.err != "" {
			content.WriteString(selectedItemStyle.Render(" " + manager.err))
			content.WriteString("\n")
		}
		content.WriteString("\n")
		content.WriteString(mutedStyle.Render(" esc: back"))
		return content.String()
	}
	preview := manager.preview
	fmt.Fprintf(&content, "  path: %s\n", preview.Path)
	fmt.Fprintf(&content, "  name: %s\n", preview.Name)
	fmt.Fprintf(&content, "  display: %q\n", preview.Display)
	targets := "none"
	if len(preview.Targets) > 0 {
		targets = strings.Join(preview.Targets, ",")
	}
	fmt.Fprintf(&content, "  targets: %s\n", targets)
	fmt.Fprintf(&content, "  query language: %s\n", preview.Language)
	fmt.Fprintf(&content, "  writes: %s\n", preview.Writes)
	fmt.Fprintf(&content, "  sha256: %s\n", preview.SHA256)
	content.WriteString("\n")
	if manager.err != "" {
		content.WriteString(selectedItemStyle.Render(" " + manager.err))
		content.WriteString("\n")
		content.WriteString(mutedStyle.Render(" esc: review input"))
		return content.String()
	}
	if manager.busy {
		content.WriteString(mutedStyle.Render(" verifying & saving…"))
		return content.String()
	}
	content.WriteString(mutedStyle.Render(" enter: enable (pin & save) • esc: cancel"))
	return content.String()
}

func pluginManagerRemoveContent(manager *pluginManager) string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Remove plugin "))
	content.WriteString("\n\n")
	if len(manager.removeList) == 0 {
		content.WriteString("  no plugins configured\n")
	} else {
		for i, entry := range manager.removeList {
			prefix := "  "
			label := safeText(entry)
			if i == manager.removeCursor {
				prefix = "> "
				label = selectedItemStyle.Render(label)
			}
			content.WriteString(prefix + label + "\n")
		}
	}
	if manager.err != "" {
		content.WriteString("\n")
		content.WriteString(selectedItemStyle.Render(" " + manager.err))
		content.WriteString("\n")
	}
	content.WriteString("\n")
	content.WriteString(mutedStyle.Render(" enter: remove • j/k or arrows navigate • esc: back"))
	return content.String()
}

// pluginManagerStatusContent renders the live status view: one selectable
// line per configured entry, the compact details of the selected entry
// (identity, protocol, trust, process/exit state, initialize duration,
// in-flight count, last failure, bounded stderr tail), and the explicit
// refresh/restart hints. The statuses were redacted when read.
func pluginManagerStatusContent(manager *pluginManager) string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Plugin status "))
	content.WriteString("\n\n")
	if len(manager.statuses) == 0 {
		content.WriteString("  no plugin status available\n")
		if manager.err != "" {
			content.WriteString("\n")
			content.WriteString(selectedItemStyle.Render(" " + manager.err))
			content.WriteString("\n")
		}
		content.WriteString("\n")
		content.WriteString(mutedStyle.Render(" esc: back"))
		return content.String()
	}
	for i, status := range manager.statuses {
		label := safeText(pluginStatusLabel(status))
		prefix := "  "
		if i == manager.statusCursor {
			prefix = "> "
			label = selectedItemStyle.Render(label)
		}
		content.WriteString(prefix + label + "\n")
	}
	content.WriteString("\n")
	content.WriteString(pluginStatusDetails(manager.statuses[min(manager.statusCursor, len(manager.statuses)-1)]))
	content.WriteString("\n")
	if manager.err != "" {
		content.WriteString(selectedItemStyle.Render(" " + manager.err))
		content.WriteString("\n")
	}
	if manager.busy {
		content.WriteString(mutedStyle.Render(" restarting…"))
		return content.String()
	}
	content.WriteString(mutedStyle.Render(" r: refresh • enter: restart • esc: back"))
	return content.String()
}

// pluginStatusLabel is the compact one-line identity of one status: the
// host-known plugin name (or the configured entry before any successful
// handshake) plus its state.
func pluginStatusLabel(status plugin.Status) string {
	name := status.Plugin
	if name == "" {
		name = status.Entry
	}
	return name + " (" + pluginStatusState(status) + ")"
}

// pluginStatusState classifies one status for display: running,
// unresolved (resolution never succeeded), rejected (never reached a
// handshake), crashed (child died with a terminal failure), or stopped
// (reaped cleanly).
func pluginStatusState(status plugin.Status) string {
	switch {
	case status.Path == "":
		return "unresolved"
	case status.Running:
		return "running"
	case status.Error != "":
		if status.PID == 0 && status.InitDuration == 0 {
			return "rejected"
		}
		return "crashed"
	case status.ExitStatus != 0:
		return "crashed"
	default:
		return "stopped"
	}
}

// pluginStatusDetails renders the compact details of one status: entry
// and canonical path, plugin identity, perk protocol version, trust
// state and fingerprint, pid/running/exit state, initialize duration,
// in-flight count, the last failure, and the bounded stderr tail (capped
// for display; the status itself carries the full bounded tail).
func pluginStatusDetails(status plugin.Status) string {
	var details strings.Builder
	fmt.Fprintf(&details, "  entry: %s\n", safeText(status.Entry))
	fmt.Fprintf(&details, "  source: %s\n", safeText(status.Source))
	if len(status.Args) > 0 {
		fmt.Fprintf(&details, "  args: %s\n", safeText(strings.Join(status.Args, " ")))
	}
	if status.Path != "" {
		fmt.Fprintf(&details, "  path: %s\n", safeText(status.Path))
	}
	fmt.Fprintf(&details, "  name: %s\n", safeText(status.Plugin))
	protocol := "perk/v1"
	if status.ProtocolVersion > 0 {
		protocol = fmt.Sprintf("perk/v1 (%d)", status.ProtocolVersion)
	}
	fmt.Fprintf(&details, "  protocol: %s\n", protocol)
	if status.Trusted {
		fmt.Fprintf(&details, "  trust: pinned (sha256 %s…)\n", shortFingerprint(status.Fingerprint))
	} else {
		details.WriteString("  trust: unpinned\n")
	}
	process := "reaped"
	if status.Running {
		process = fmt.Sprintf("pid %d", status.PID)
	}
	fmt.Fprintf(&details, "  process: %s • exit: %d\n", process, status.ExitStatus)
	fmt.Fprintf(&details, "  initialize: %s • in-flight: %d\n", status.InitDuration.Round(time.Millisecond), status.InFlight)
	if status.Error != "" {
		fmt.Fprintf(&details, "  failure: %s\n", safeText(status.Error))
	} else {
		details.WriteString("  failure: none\n")
	}
	if len(status.Stderr) > 0 {
		details.WriteString("  stderr (last 6):\n")
		tail := status.Stderr
		if len(tail) > pluginStatusStderrLines {
			tail = tail[len(tail)-pluginStatusStderrLines:]
		}
		for _, line := range tail {
			fmt.Fprintf(&details, "    %s\n", safeText(line))
		}
	}
	return details.String()
}

// pluginStatusStderrLines caps the stderr lines rendered in the status
// view; the status itself carries the loader's full bounded tail.
const pluginStatusStderrLines = 6

// shortFingerprint abbreviates a sha256 pin for display.
func shortFingerprint(pin string) string {
	if len(pin) <= 16 {
		return pin
	}
	return pin[:16]
}
