package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/squirrd/fleet-scan/internal/ocm"
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
		RunE:  runScan,
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

func runScan(cmd *cobra.Command, args []string) error {
	search, _ := cmd.Flags().GetString("search")
	collectorRaw, _ := cmd.Flags().GetStringSlice("collector")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")
	debug, _ := cmd.Flags().GetBool("debug")

	level := slog.LevelWarn
	if verbose {
		level = slog.LevelInfo
	}
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	specs, err := ParseCollectorSpecs(collectorRaw)
	if err != nil {
		return err
	}

	if err := ValidateCollectors(specs, dryRun); err != nil {
		return err
	}

	token, err := ocm.ResolveToken()
	if err != nil {
		return err
	}

	client, err := ocm.NewSDKClient(token)
	if err != nil {
		return err
	}

	if dryRun {
		return runDryRun(cmd, client, search)
	}

	_ = specs
	cmd.PrintErrln("scan mode not yet implemented (Phase 2)")
	return nil
}

func runDryRun(cmd *cobra.Command, client ocm.OCMClient, search string) error {
	ctx := context.Background()
	total, err := ocm.GetClusterCount(ctx, client, search)
	if err != nil {
		return err
	}
	cmd.Printf("Found %d clusters matching search\n", total)
	return nil
}
