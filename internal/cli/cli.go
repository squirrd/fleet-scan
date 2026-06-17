package cli

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "fleet-scan",
		Short: "Batch scanner for OpenShift cluster fleets",
	}

	root.AddCommand(newScanCommand())
	return root
}

func newScanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan clusters matching a search query",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.Flags().String("search", "", "OCM search query string")
	cmd.Flags().StringSlice("collector", nil, "Collector spec (name:key=val,key2=val2)")
	cmd.Flags().Bool("dry-run", false, "Run search only, report cluster count")
	cmd.Flags().Int("cluster-timeout", 120, "Per-cluster timeout in seconds")
	cmd.Flags().String("output-dir", "./output/", "Output directory for results")
	cmd.Flags().Bool("verbose", false, "Enable verbose logging")
	cmd.Flags().Bool("debug", false, "Enable debug logging")

	return cmd
}
