package main

import (
	"os"

	"github.com/squirrd/fleet-scan/internal/cli"

	// Blank-import collector packages to trigger init() registration.
	_ "github.com/squirrd/fleet-scan/internal/collector"
)

const version = "0.1.5"

func main() {
	if err := cli.NewRootCommand(version).Execute(); err != nil {
		os.Exit(1)
	}
}
