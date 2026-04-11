package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/store"
	"github.com/panyam/gap-tracker/internal/triage"
)

// --- export --------------------------------------------------------------

// newTriageExportCmd writes a YAML snapshot of the entire triage table.
// The snapshot is parse-compatible with Python's cmd_triage_export
// output so Python and Go can consume each other's files during the
// port. Default path: <project>/triage.snapshot.yaml.
func newTriageExportCmd() *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write a YAML snapshot of the triage table",
		Long: `Writes a YAML snapshot of every classified issue to <project>/triage.snapshot.yaml
(or the path given by --out). The snapshot is intended for git review — commit
it periodically to get a point-in-time history of the triage state.

The file is parse-compatible with the Python export output, so you can
diff two snapshots from different tools without worrying about shape.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			snap, err := buildSnapshot(a)
			if err != nil {
				return err
			}

			path := outPath
			if path == "" {
				path = filepath.Join(a.Cfg.ProjectRoot, "triage.snapshot.yaml")
			}
			if err := writeSnapshotFile(path, a.Cfg.Project, snap); err != nil {
				return err
			}

			total := 0
			for _, m := range snap {
				total += len(m)
			}
			fmt.Fprintf(os.Stdout, "Exported %d classifications to %s\n", total, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&outPath, "out", "",
		"output path (default: <project>/triage.snapshot.yaml)")
	return cmd
}

// buildSnapshot maps every triage row to its source label and packs the
// result into a triage.Snapshot. Lives in CLI (not the triage package)
// because it needs the config.Config for label↔repo resolution.
func buildSnapshot(a *app.App) (triage.Snapshot, error) {
	rows, err := a.Store.AllTriage()
	if err != nil {
		return nil, err
	}
	snap := triage.Snapshot{}
	for _, r := range rows {
		label := a.Cfg.RepoToSourceLabel(r.Repo)
		if snap[label] == nil {
			snap[label] = map[int]triage.Entry{}
		}
		snap[label][r.Number] = triage.Entry{
			Bucket:    r.Bucket,
			Rationale: r.Rationale,
			Target:    r.Target,
			Test:      r.Test,
		}
	}
	return snap, nil
}

// writeSnapshotFile writes atomically: temp file in the same directory,
// then rename into place. A partial write never corrupts the snapshot
// because rename is atomic on POSIX.
func writeSnapshotFile(path, project string, snap triage.Snapshot) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".triage.snapshot.*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// Clean up the temp file if anything below fails.
	defer func() {
		_ = tmp.Close()
		if _, err := os.Stat(tmpPath); err == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := triage.WriteSnapshot(tmp, project, snap); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// --- import --------------------------------------------------------------

// newTriageImportCmd reads a YAML snapshot and bulk-upserts its entries
// into the triage table. This is the rare reverse path — normal use is
// to edit via `triage set`, not YAML. Importing is idempotent: re-running
// with the same file produces the same DB state.
func newTriageImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <path>",
		Short: "Bulk-upsert triage entries from a YAML snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()

			snap, err := triage.ReadSnapshot(f)
			if err != nil {
				return errf("parse %s: %w", args[0], err)
			}

			imported, skipped, warnings := applySnapshot(a, snap)
			fmt.Fprintf(os.Stdout, "Imported %d classifications from %s\n", imported, args[0])
			if skipped > 0 {
				fmt.Fprintf(os.Stdout, "Skipped %d rows with no matching issue in DB — run sync first\n", skipped)
			}
			for _, w := range warnings {
				fmt.Fprintln(os.Stderr, "WARN:", w)
			}
			return nil
		},
	}
}

// applySnapshot walks the parsed YAML and upserts each row. Returns
// (imported, skipped, warnings). Skipped rows are ones whose source
// issue isn't in the DB (common after cloning to a new machine and
// forgetting to sync). Warnings cover unknown source labels and
// invalid buckets — we log instead of failing so one bad row doesn't
// block a mostly-valid import.
func applySnapshot(a *app.App, snap triage.Snapshot) (int, int, []string) {
	var warnings []string
	imported := 0
	skipped := 0

	for label, entries := range snap {
		repo := a.Cfg.SourceLabelToRepo(label)
		if repo == "" {
			warnings = append(warnings, fmt.Sprintf("unknown source label %q; skipping %d entries", label, len(entries)))
			continue
		}
		for number, e := range entries {
			if !store.ValidBuckets[e.Bucket] {
				warnings = append(warnings, fmt.Sprintf("%s:%d has invalid bucket %q; skipping", label, number, e.Bucket))
				continue
			}
			err := a.Store.UpsertTriage(store.UpsertTriageInput{
				Repo:      repo,
				Number:    number,
				Bucket:    e.Bucket,
				Target:    e.Target,
				Rationale: e.Rationale,
				Test:      e.Test,
			})
			if err != nil {
				// Most likely "issue not in DB" — count as skipped.
				skipped++
				continue
			}
			imported++
		}
	}
	return imported, skipped, warnings
}
