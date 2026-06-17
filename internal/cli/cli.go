package cli

import "github.com/spf13/cobra"

// NewRootCommand returns the root cobra command for fleet-scan.
// TODO: implement — register scan subcommand with all flags.
func NewRootCommand() *cobra.Command {
	return &cobra.Command{
		Use: "fleet-scan",
	}
}
