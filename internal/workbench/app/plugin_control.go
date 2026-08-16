package app

import (
	"context"
	"net/url"
	"strings"

	"github.com/l3aro/perk-workbench/internal/database/plugin"
	sharedsql "github.com/l3aro/perk-workbench/internal/sql"
)

// PluginControl is the injected live plugin lifecycle controller: the
// real plugin.Loader in production, a fake in tests. The workbench never
// owns plugin child processes — cmd main builds the loader and hands it
// over through Model.SetPluginControl. Statuses never spawn, mutate, or
// exchange protocol traffic; Restart recovers exactly one configured
// entry; EntryForService reports which configured entry backs a live
// service (never an old generation).
type PluginControl interface {
	// Statuses returns one status per configured entry, in config order.
	Statuses() []plugin.Status
	// Restart recovers the configured entry identified by its entry
	// text or canonical path.
	Restart(ctx context.Context, identifier string) error
	// EntryForService returns the configured entry text of the plugin
	// child backing service, or "" when service is not a live session of
	// the controller's current generation.
	EntryForService(service sharedsql.Service) (string, bool)
}

// pluginStoppedCTA is the concise actionable status shown when an
// operation fails because a plugin child exited or the perk/v1 protocol
// died: the plugin stopped and the Plugins → Status → Restart path
// recovers it. The original structured error stays in the query-log
// detail and diagnostics.
const pluginStoppedCTA = "the plugin stopped; open Plugins → Status → Restart to recover it"

// pluginFailureStatus renders the status line for a failed operation:
// when the failure is plugin-terminal, the actionable recovery path
// replaces the raw error text (which stays in detail/diagnostics);
// otherwise the fallback text passes through unchanged.
func pluginFailureStatus(err error, fallback string) string {
	if plugin.IsTerminal(err) {
		return pluginStoppedCTA
	}
	return fallback
}

// pluginStatuses returns the live plugin statuses with credential
// material redacted for display: the entry failure text and every
// stderr line pass through the credential redactor and the connection
// target scrubber before they reach the status view, notifications, or
// logs.
func (m Model) pluginStatuses() []plugin.Status {
	if m.pluginControl == nil {
		return nil
	}
	statuses := m.pluginControl.Statuses()
	secrets := m.connectionSecrets(m.Target)
	for i := range statuses {
		statuses[i].Error = scrubPluginTarget(redactCredentials(statuses[i].Error, secrets), m.connectionTarget)
		for j := range statuses[i].Stderr {
			statuses[i].Stderr[j] = scrubPluginTarget(redactCredentials(statuses[i].Stderr[j], secrets), m.connectionTarget)
		}
	}
	return statuses
}

// scrubPluginTarget removes the known connection target from diagnostic
// text as a whole, after credential masking: the raw target, its
// credential-redacted URL form, the userinfo-stripped URL form, and the
// authority/host fragments derived from it are all replaced, so neither
// the raw URL, its redacted form, nor host/path fragments that identify
// the target survive. It is driven by the known target only — arbitrary
// hosts in unrelated errors are never touched — and is applied before
// the text is rendered, logged, or persisted.
func scrubPluginTarget(text, target string) string {
	if text == "" {
		return ""
	}
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return text
	}
	fragments := targetFragments(trimmed)
	// Longest first, so one fragment never masks another.
	for i := 1; i < len(fragments); i++ {
		for j := i; j > 0 && len(fragments[j]) > len(fragments[j-1]); j-- {
			fragments[j], fragments[j-1] = fragments[j-1], fragments[j]
		}
	}
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			continue
		}
		text = strings.ReplaceAll(text, fragment, redacted)
	}
	return text
}

// targetFragments derives the identifying forms of one connection
// target: the raw text, and — when the target is a URL or a
// label-prefixed URL ("redis:redis://…") — the credential-redacted and
// userinfo-stripped URL forms plus the authority, hostname, and exact
// path fragments. The known path is scrubbed in both its percent-encoded
// and decoded renderings, leading slash included; generic individual
// path segments are never derived, so unrelated paths and text survive
// exact matching untouched.
func targetFragments(target string) []string {
	var fragments []string
	add := func(fragment string) {
		if strings.TrimSpace(fragment) != "" {
			fragments = append(fragments, fragment)
		}
	}
	add(target)

	urlText := ""
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		urlText = target
	} else if label, rest, found := strings.Cut(target, ":"); found && label != "" && strings.Contains(rest, "://") {
		urlText = rest
	}
	u, err := url.Parse(urlText)
	if err != nil || u.Host == "" {
		return fragments
	}
	escapedPath := u.EscapedPath()
	decodedPath := u.Path
	hasPath := decodedPath != "" && decodedPath != "/"

	// The credential-redacted URL, with and without path. The escaped
	// rendering (u.Redacted) and — when the path was percent-encoded —
	// the decoded rendering are both derived, because a plugin may echo
	// either form.
	add(u.Redacted())
	noPath := *u
	noPath.Path, noPath.RawQuery, noPath.Fragment = "", "", ""
	add(noPath.Redacted())
	if hasPath {
		decoded := *u
		decoded.RawPath = ""
		add(decoded.Redacted())
	}
	// The literal redaction-marker userinfo forms: the bracketed marker
	// makes url.Parse fail, so the redactor's URL rewrite is skipped and
	// the marker stays inside the URL. Escaped and decoded path.
	if u.User != nil {
		userinfo := u.User.String()
		if colon := strings.Index(userinfo, ":"); colon >= 0 {
			userinfo = userinfo[:colon] + ":" + redacted
		}
		add(u.Scheme + "://" + userinfo + "@" + u.Host + escapedPath)
		add(u.Scheme + "://" + userinfo + "@" + u.Host)
		if hasPath {
			add(u.Scheme + "://" + userinfo + "@" + u.Host + decodedPath)
		}
	}
	// Userinfo-stripped URL forms: escaped and decoded path.
	noUser := noPath
	noUser.User = nil
	add(noUser.String())
	if hasPath {
		add(noUser.String() + escapedPath)
		add(noUser.String() + decodedPath)
	}
	// Authority, hostname, and exact path fragments.
	if hasPath {
		add(u.Host + escapedPath)
		add(u.Host + decodedPath)
	}
	add(u.Host)
	if hostname := u.Hostname(); hostname != "" {
		add(hostname)
	}
	if hasPath {
		add(escapedPath)
		add(decodedPath)
	}
	return fragments
}
