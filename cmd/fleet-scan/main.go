package main

import (
	"os"

	"github.com/squirrd/fleet-scan/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
