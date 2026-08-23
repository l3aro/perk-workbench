package mysql

import (
	"net"
	"strings"

	godrv "github.com/go-sql-driver/mysql"
	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
)

// Target builds the MySQL DSN for the connection form's server fields.
// tls is the TLS mode ("true", "skip-verify", "false"); blank falls back
// to the disabled mode. The workbench never formats DSNs itself: the
// grammar lives with the driver that parses it.
func Target(values driver.FormValues) string {
	config := godrv.NewConfig()
	config.InterpolateParams = true
	config.User = strings.TrimSpace(values.User)
	config.Passwd = values.Pass
	config.Net = "tcp"
	config.Addr = net.JoinHostPort(values.Host, values.Port)
	config.DBName = strings.TrimSpace(values.Database)
	tls := values.TLS
	if tls == "" {
		tls = "false"
	}
	config.TLSConfig = tls
	return config.FormatDSN()
}
