package postgres

import (
	"net"
	"net/url"
	"strings"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

// Target builds the PostgreSQL connection URL for the connection form's
// server fields. TLS is the mode ("verify-full", "require", "disable");
// blank falls back to the disabled mode.
func Target(values driver.FormValues) string {
	target := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(strings.TrimSpace(values.User), values.Pass),
		Host:   net.JoinHostPort(values.Host, values.Port),
		Path:   strings.TrimSpace(values.Database),
	}
	tls := values.TLS
	if tls == "" {
		tls = "disable"
	}
	target.RawQuery = url.Values{"sslmode": {tls}}.Encode()
	return target.String()
}
