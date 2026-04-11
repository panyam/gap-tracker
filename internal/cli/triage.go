package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/render"
	"github.com/panyam/gap-tracker/internal/store"
)

// newTriageCmd is the parent for all `gapt triage <verb>` subcommands.
// M2 ships read-only verbs: status, list. Writes (set/unset/close) land
// in M4; export/import in M5; verify is a stub (M7).
func newTriageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "triage",
		Short: "Classify upstream issues against local project state",
		Long: `gapt triage is the entry point for every classification action.

The DB table 'triage' is the source of truth. See docs/STATE_MACHINE.md
for the full flow model and developer entry points.`,
	}
	cmd.AddCommand(newTriageStatusCmd())
	cmd.AddCommand(newTriageListCmd())
	return cmd
}

// --- status --------------------------------------------------------------

func newTriageStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show per-source bucket counts + unclassified items",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			report, err := buildStatusReport(a)
			if err != nil {
				return err
			}
			return a.Renderer.RenderStatus(os.Stdout, report)
		},
	}
}

// buildStatusReport pulls the bucket counts + unclassified lists per
// source and packages them into the render.StatusReport view struct.
// Stays in CLI (not store) because it glues config + two store queries.
func buildStatusReport(a *app.App) (render.StatusReport, error) {
	counts, err := a.Store.BucketCountsBySource()
	if err != nil {
		return render.StatusReport{}, err
	}

	report := render.StatusReport{Project: a.Cfg.Project}
	for _, src := range a.Cfg.Sources {
		open, err := a.Store.OpenIssuesBySource(src.Repo)
		if err != nil {
			return render.StatusReport{}, err
		}
		classified, err := a.Store.ClassifiedNumbers(src.Repo)
		if err != nil {
			return render.StatusReport{}, err
		}

		unclassified := make([]render.UnclassifiedIssue, 0)
		for _, i := range open {
			if !classified[i.Number] {
				unclassified = append(unclassified, render.UnclassifiedIssue{
					Ref:   src.Label + ":" + itoa(i.Number),
					Title: i.Title,
				})
			}
		}

		report.Sources = append(report.Sources, render.SourceStatus{
			Label:        src.Label,
			TotalOpen:    len(open),
			Classified:   len(open) - len(unclassified),
			Unclassified: len(unclassified),
			BucketCounts: counts[src.Repo],
		})
		if len(unclassified) > 0 {
			report.Unclassified = append(report.Unclassified, render.UnclassifiedGroup{
				Label:  src.Label,
				Issues: unclassified,
			})
		}
	}
	return report, nil
}

// --- list ----------------------------------------------------------------

func newTriageListCmd() *cobra.Command {
	var (
		bucketFilter string
		sourceFilter string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List classified issues, grouped by bucket",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			list, err := buildTriageList(a, bucketFilter, sourceFilter)
			if err != nil {
				return err
			}
			return a.Renderer.RenderList(os.Stdout, list)
		},
	}
	cmd.Flags().StringVar(&bucketFilter, "bucket", "",
		"filter to one bucket (real-gap, already-covered, defer-spec-flux, defer-scope, not-applicable, done)")
	cmd.Flags().StringVar(&sourceFilter, "source", "",
		"filter to one source label (e.g. ext-apps, go-sdk, spec, target)")
	return cmd
}

func buildTriageList(a *app.App, bucket, sourceLabel string) (render.TriageList, error) {
	filter := store.ListTriageFilter{Bucket: bucket}
	if sourceLabel != "" {
		repo := a.Cfg.SourceLabelToRepo(sourceLabel)
		if repo == "" {
			return render.TriageList{}, errf("unknown source %q", sourceLabel)
		}
		filter.Repo = repo
	}

	rows, err := a.Store.ListTriage(filter)
	if err != nil {
		return render.TriageList{}, err
	}

	// Group by bucket preserving canonical order.
	byBucket := map[string][]render.TriageRow{}
	for _, r := range rows {
		label := a.Cfg.RepoToSourceLabel(r.Repo)
		byBucket[r.Bucket] = append(byBucket[r.Bucket], render.TriageRow{
			Ref:       label + ":" + itoa(r.Number),
			State:     r.State,
			Title:     r.Title,
			Target:    r.Target,
			Test:      r.Test,
			Rationale: r.Rationale,
		})
	}

	list := render.TriageList{}
	for _, b := range canonicalBucketOrder {
		if items, ok := byBucket[b]; ok {
			list.Groups = append(list.Groups, render.TriageGroup{
				Bucket: b,
				Rows:   items,
			})
		}
	}
	return list, nil
}
