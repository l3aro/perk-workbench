package mysql

import (
	"net"
	"strings"

	godrv "github.com/go-sql-driver/mysql"
)

// Target builds the MySQL DSN for the connection form's server fields.
// tls is the TLS mode ("true", "skip-verify", "false"); blank falls back
// to the disabled mode. The workbench never formats DSNs itself: the
// grammar lives with the driver that parses it.
func Target(user, pass, host, port, database, tls string) string {
	config := godrv.NewConfig()
	config.User = strings.TrimSpace(user)
	config.Passwd = pass
	config.Net = "tcp"
	config.Addr = net.JoinHostPort(host, port)
	config.DBName = strings.TrimSpace(database)
	if tls == "" {
		tls = "false"
	}
	config.TLSConfig = tls
	return config.FormatDSN()
}
