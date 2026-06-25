package main

import (
	"os"

	"github.com/squirrd/fleet-scan/internal/cli"

	// Blank-import collector packages to trigger init() registration.
	_ "github.com/squirrd/fleet-scan/internal/collector"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
