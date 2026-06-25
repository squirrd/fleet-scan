package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/squirrd/fleet-scan/internal/backplane"
	"github.com/squirrd/fleet-scan/internal/ocm"
	"github.com/squirrd/fleet-scan/internal/output"
	"github.com/squirrd/fleet-scan/internal/runner"
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
	cmd.Flags().String("resume", "", "Resume a previous run from the given run directory path")
	cmd.Flags().Int("concurrency", 1, "Number of clusters to process concurrently")
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
	resumePath, _ := cmd.Flags().GetString("resume")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	clusterTimeoutSec, _ := cmd.Flags().GetInt("cluster-timeout")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	if concurrency < 1 {
		return fmt.Errorf("--concurrency must be at least 1, got %d", concurrency)
	}

	level := slog.LevelWarn
	if verbose {
		level = slog.LevelInfo
	}
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	// Handle --resume mutual exclusion.
	if resumePath != "" {
		searchChanged := cmd.Flags().Changed("search")
		collectorChanged := cmd.Flags().Changed("collector")
		if searchChanged || collectorChanged {
			var conflicts []string
			if searchChanged {
				conflicts = append(conflicts, "--search")
			}
			if collectorChanged {
				conflicts = append(conflicts, "--collector")
			}
			return fmt.Errorf("--resume cannot be combined with %s", strings.Join(conflicts, " and "))
		}
	}

	// If resuming, read search and collectors from meta.json.
	if resumePath != "" {
		metaPath := filepath.Join(resumePath, "meta.json")
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			return fmt.Errorf("reading meta.json from resume path: %w", err)
		}

		var prevMeta output.RunMeta
		if err := json.Unmarshal(metaBytes, &prevMeta); err != nil {
			return fmt.Errorf("parsing meta.json: %w", err)
		}

		search = prevMeta.Search
		collectorRaw = prevMeta.Collectors
	}

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

	// --- Scan mode (Phase 2) ---
	ctx := context.Background()

	// Get clusters from OCM.
	clusters, err := ocm.ListAllClusters(ctx, client, search)
	if err != nil {
		return err
	}

	// Build collector spec strings for meta.json.
	var collectorSpecs []string
	for _, raw := range collectorRaw {
		collectorSpecs = append(collectorSpecs, raw)
	}

	meta := output.RunMeta{
		Status:                "running",
		Search:                search,
		Collectors:            collectorSpecs,
		ClusterTimeoutSeconds: clusterTimeoutSec,
		ClustersTotal:         len(clusters),
		StartedAt:             time.Now().UTC(),
	}

	// Create writer — either resume into existing dir or create new.
	var w *output.Writer
	if resumePath != "" {
		meta.ResumedAt = time.Now().UTC()
		w, err = output.ResumeWriter(resumePath, meta)
	} else {
		w, err = output.NewWriter(outputDir, meta)
	}
	if err != nil {
		return err
	}

	// If resuming, filter out already-completed clusters.
	if resumePath != "" {
		jsonlPath := filepath.Join(resumePath, "results.jsonl")
		completed, loadErr := output.LoadCompletedSet(jsonlPath)
		if loadErr == nil {
			var remaining []ocm.ClusterMetadata
			for _, c := range clusters {
				if !completed[c.ID] {
					remaining = append(remaining, c)
				}
			}
			clusters = remaining
		}
	}

	// Create kubeconfigs directory for isolated backplane logins.
	runDir := w.RunDir()
	kubeconfigDir := filepath.Join(runDir, "kubeconfigs")
	if err := os.MkdirAll(kubeconfigDir, 0o700); err != nil {
		return fmt.Errorf("creating kubeconfigs directory: %w", err)
	}

	startTime := time.Now()
	opts := runner.RunOptions{
		ClusterTimeout: time.Duration(clusterTimeoutSec) * time.Second,
		Stderr:         cmd.ErrOrStderr(),
		BackplaneLogin: backplane.MakeBackplaneLoginFunc(kubeconfigDir),
	}

	d := runner.NewDispatcher(concurrency, opts)
	runErr := d.Dispatch(ctx, clusters, w)

	// Finalize.
	status := "completed"
	if runErr != nil {
		status = "interrupted"
	}
	dur := time.Since(startTime)

	// Print compact summary line to stderr.
	stderr := cmd.ErrOrStderr()
	if runErr != nil {
		fmt.Fprintln(stderr, d.InterruptedSummaryLine(len(clusters), dur, w.RunDir()))
	} else {
		fmt.Fprintln(stderr, d.SummaryLine(len(clusters), dur, w.RunDir()))
	}

	if finalErr := w.Finalize(status, d.Succeeded(), d.Failed(), d.Skipped(), dur); finalErr != nil {
		return finalErr
	}

	return runErr
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
