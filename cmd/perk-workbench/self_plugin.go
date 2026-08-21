package main

import (
	"fmt"
	"io"

	"github.com/l3aro/perk-workbench-plugin-sdk-go/driver"
	"github.com/l3aro/perk-workbench-plugin-sdk-go/server"
	"github.com/l3aro/perk-workbench/internal/drivers/mongodb"
	"github.com/l3aro/perk-workbench/internal/drivers/mysql"
	"github.com/l3aro/perk-workbench/internal/drivers/postgres"
	"github.com/l3aro/perk-workbench/internal/drivers/sqlite"
)

func dispatchSelfPlugin(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintf(stderr, "--plugin expects exactly one plugin name, got %d operands\n", len(args))
		return 2
	}

	var factory driver.Factory
	switch args[0] {
	case "sqlite":
		factory = sqlite.Factory{}
	case "mysql":
		factory = mysql.Factory{}
	case "postgres":
		factory = postgres.Factory{}
	case "mongodb":
		factory = mongodb.Factory{}
	default:
		fmt.Fprintf(stderr, "unknown plugin %q\n", args[0])
		return 2
	}
	if err := server.Run(stdin, stdout, factory); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
