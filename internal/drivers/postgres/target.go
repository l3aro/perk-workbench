package postgres

import (
	"net"
	"net/url"
	"strings"
)

// Target builds the PostgreSQL connection URL for the connection form's
// server fields. sslmode is the TLS mode ("verify-full", "require",
// "disable"); blank falls back to the disabled mode. The workbench never
// formats connection URLs itself: the grammar lives with the driver that
// parses it.
func Target(user, pass, host, port, database, sslmode string) string {
	target := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(strings.TrimSpace(user), pass),
		Host:   net.JoinHostPort(host, port),
		Path:   strings.TrimSpace(database),
	}
	if sslmode == "" {
		sslmode = "disable"
	}
	target.RawQuery = url.Values{"sslmode": {sslmode}}.Encode()
	return target.String()
}
