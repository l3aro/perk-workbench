package app

import (
	"net/url"
	"slices"
	"strings"

	"github.com/l3aro/perk-workbench/internal/database"
	"github.com/l3aro/perk-workbench/internal/workbench/profile"
)

// redacted marks credential material removed from error text before it
// is logged, displayed, or persisted.
const redacted = "[redacted]"

// redactCredentials removes credential material from text before it
// reaches the event log, notification history, status line, or failure
// screen. It is value-aware — exact literal secret values and their
// percent-encoded forms are replaced wherever they appear — and
// URL-userinfo-aware: passwords embedded in scheme:// URLs are stripped
// while the username and the rest of the URL survive for context.
// Benign text is never touched.
func redactCredentials(text string, secrets []string) string {
	if text == "" {
		return ""
	}
	seen := make(map[string]bool, len(secrets))
	for _, secret := range secrets {
		if secret != "" {
			seen[secret] = true
		}
	}
	// Longest first so one secret never masks another.
	ordered := make([]string, 0, len(seen))
	for secret := range seen {
		ordered = append(ordered, secret)
	}
	slices.SortFunc(ordered, func(a, b string) int { return len(b) - len(a) })
	for _, secret := range ordered {
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			if form == "" || !strings.Contains(text, form) {
				continue
			}
			text = strings.ReplaceAll(text, form, redacted)
		}
	}
	return redactURLUserinfo(text)
}

// redactURLUserinfo replaces the password of every scheme:// URL in
// text with url.URL.Redacted, which keeps the username and the rest of
// the URL (path, query, fragment) for context. Non-URL text and URLs
// without a password are left untouched, so file:// references and
// benign "word://" artifacts survive verbatim.
func redactURLUserinfo(text string) string {
	var out strings.Builder
	for {
		marker := strings.Index(text, "://")
		if marker < 0 {
			out.WriteString(text)
			return out.String()
		}
		// Walk back to the scheme start ([A-Za-z][A-Za-z0-9+.-]*).
		start := marker
		for start > 0 && isURLSchemeChar(text[start-1]) {
			start--
		}
		// Walk forward over the URL token, stopping at whitespace or
		// common prose delimiters.
		end := marker + 3
		for end < len(text) && !isURLTerminator(text[end]) {
			end++
		}
		out.WriteString(text[:start])
		token := text[start:end]
		if u, err := url.Parse(token); err == nil && u.User != nil {
			if _, has := u.User.Password(); has {
				token = u.Redacted()
			}
		}
		out.WriteString(token)
		text = text[end:]
	}
}

func isURLSchemeChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'
}

func isURLTerminator(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '"', '\'', '<', '>', '`':
		return true
	}
	return false
}

// connectionSecrets returns the literal credential values that must
// never appear in logged, displayed, or persisted connection errors:
// the form's password and password-kind extras (raw and secret-ref
// resolved), plus any password embedded in the given opener target
// (URL userinfo or the mysql: DSN form, decoded and percent-encoded).
func (m Model) connectionSecrets(target string) []string {
	var secrets []string
	add := func(value string) {
		if value != "" && value != redacted {
			secrets = append(secrets, value)
		}
	}
	form := m.connection.component.Form.Values
	add(form.Pass)
	add(profile.ResolveSecretRef(form.Pass))
	if spec, ok := database.ByName(string(form.Driver)); ok && spec.Form != nil {
		for _, field := range spec.Form.Fields {
			if field.Kind != database.FormPassword {
				continue
			}
			value := form.Extras[field.Key]
			add(value)
			add(profile.ResolveSecretRef(value))
		}
	}
	// Passwords embedded in the target itself (argv/env/form targets)
	// are secrets even when the form never carried them.
	secrets = append(secrets, targetPasswords(target)...)
	return secrets
}

// targetPasswords extracts the literal passwords embedded in an opener
// target: scheme://user:pass@ URLs (decoded and the percent-encoded
// userinfo form), label-prefixed targets ("postgres:postgres://…",
// "redis:redis://…") whose URL is hidden from a direct parse, and the
// mysql:user:pass@tcp(...) DSN form.
func targetPasswords(target string) []string {
	var passwords []string
	trimmed := strings.TrimSpace(target)
	add := func(value string) {
		if value != "" {
			passwords = append(passwords, value)
		}
	}
	addURL := func(s string) {
		u, err := url.Parse(s)
		if err != nil || u.User == nil {
			return
		}
		if pass, has := u.User.Password(); has {
			add(pass)
			// The percent-encoded userinfo form appears in URL targets.
			if encoded := strings.TrimPrefix(u.User.String(), u.User.Username()+":"); encoded != pass {
				add(encoded)
			}
		}
	}
	addURL(trimmed)
	if label, rest, found := strings.Cut(trimmed, ":"); found && label != "" && strings.Contains(rest, "://") {
		addURL(rest)
	}
	if body, found := strings.CutPrefix(trimmed, "mysql:"); found {
		// go-sql-driver DSN: user:password@tcp(host:port)/db. A DSN that
		// parses cannot contain an unescaped '@' in the password.
		if at := strings.LastIndex(body, "@"); at > 0 {
			if colon := strings.Index(body[:at], ":"); colon >= 0 {
				add(body[colon+1 : at])
			}
		}
	}
	return passwords
}
