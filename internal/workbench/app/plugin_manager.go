package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/l3aro/perk-workbench/internal/database/plugin"
)

// Plugin manager views, in the order the user walks them.
const (
	pluginViewMenu    = "menu"
	pluginViewAdd     = "add"
	pluginViewPreview = "preview"
	pluginViewRemove  = "remove"
)

// pluginInspectTimeout bounds one async inspect in the plugin manager.
// Every preview and approve re-runs the full resolve/initialize/
// shutdown lifecycle of the executable.
var pluginInspectTimeout = 30 * time.Second

// pluginManager is the root-owned plugin management overlay reachable
// from the command palette. It walks the user through adding a plugin
// (enter executable → async inspect/hash preview → explicit enable →
// async re-verify → atomic save) and removing one (pick entry → confirm
// → atomic save). It never mutates the live driver registry; changes
// take effect on the next start.
type pluginManager struct {
	// view is the current step: menu, add, preview, or remove.
	view string
	// cursor selects the menu entry.
	cursor int
	// input is the executable path being entered in the add step.
	input []rune
	// preview is the resolved add preview awaiting confirmation.
	preview *pluginAddPreview
	// err is the last failure shown in the current view.
	err string
	// busy reports an in-flight async inspect or approve.
	busy bool
	// removeList is the configured entries in config order.
	removeList []string
	// removeCursor selects the entry to remove.
	removeCursor int
	// removeConfirm is the open removal confirmation dialog.
	removeConfirm *confirmationDialog
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
// success it carries the persisted plugins/trust state so the model
// goroutine can apply it to appConfig without racing the command
// goroutine.
type pluginApproveMsg struct {
	path    string
	plugins []string
	trust   map[string]string
	err     error
}

func newPluginManager() *pluginManager {
	return &pluginManager{
		view:       pluginViewMenu,
		removeList: append([]string{}, appConfig.Plugins...),
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
		plugins, trust, _, err := savePlugin(configPath, path, digest)
		if err != nil {
			return pluginApproveMsg{err: err}
		}
		return pluginApproveMsg{path: path, plugins: plugins, trust: trust}
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
		appConfig.PluginTrust = msg.trust
		m.overlay.pluginManager = nil
		m.setStatus("plugin enabled; restart required")
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
			default:
				m.overlay.pluginManager = nil
			}
		case tea.KeyUp:
			manager.cursor = max(0, manager.cursor-1)
		case tea.KeyDown:
			manager.cursor = min(2, manager.cursor+1)
		default:
			switch keyPress.Keystroke() {
			case "j":
				manager.cursor = min(2, manager.cursor+1)
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
	default:
		return pluginManagerRemoveContent(manager)
	}
}

func pluginManagerMenuContent(manager *pluginManager) string {
	var content strings.Builder
	content.WriteString(headerStyle.Render(" Plugins "))
	content.WriteString("\n\n")
	for i, option := range []string{"Add plugin", "Remove plugin", "Back"} {
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
