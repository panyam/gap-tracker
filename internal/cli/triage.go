package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

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
	cmd.AddCommand(newTriageSetCmd())
	cmd.AddCommand(newTriageUnsetCmd())
	cmd.AddCommand(newTriageCloseCmd())
	cmd.AddCommand(newTriageExportCmd())
	cmd.AddCommand(newTriageImportCmd())
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

// --- set -----------------------------------------------------------------

// targetTokenRe matches a "target token" — a single identifier-ish string
// with no spaces. Used to disambiguate `triage set <ref> <bucket> <3rd>`:
// if the third positional matches, we treat it as the target; otherwise
// everything after <bucket> is rationale. Matches Python's heuristic.
var targetTokenRe = regexp.MustCompile(`^[A-Za-z0-9_./#-]+$`)

func newTriageSetCmd() *cobra.Command {
	var testPath string
	cmd := &cobra.Command{
		Use:   "set <ref> <bucket> [target] <rationale...>",
		Short: "Upsert a triage classification for one issue",
		Long: `Upsert a triage row. Target is optional and detected by shape:
if the third positional arg has no spaces and matches a token-like
pattern (letters, digits, _.#/-), it's treated as the target
(e.g. "mcpkit#190") and the remaining args become the rationale.
Otherwise everything after <bucket> is rationale.

Examples:
  gapt triage set ext-apps:298 real-gap mcpkit#190 "No template support"
  gapt triage set ext-apps:26 not-applicable "IANA admin task"

Use --test path::TestName to record the pinned repro test. The field
is only overwritten when --test is passed, so re-setting without it
preserves the existing test pointer.`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			ref := args[0]
			bucket := args[1]
			rest := args[2:]

			target := ""
			rationaleParts := rest
			if len(rest) >= 2 && targetTokenRe.MatchString(rest[0]) && !strings.Contains(rest[0], " ") {
				target = rest[0]
				rationaleParts = rest[1:]
			}
			rationale := strings.Join(rationaleParts, " ")
			if rationale == "" {
				return errf("rationale is required — explain why this bucket applies")
			}

			label, number, err := parseRef(ref)
			if err != nil {
				return err
			}
			repo := a.Cfg.SourceLabelToRepo(label)
			if repo == "" {
				return errf("unknown source label %q", label)
			}

			if err := a.Store.UpsertTriage(store.UpsertTriageInput{
				Repo:      repo,
				Number:    number,
				Bucket:    bucket,
				Target:    target,
				Rationale: rationale,
				Test:      testPath,
			}); err != nil {
				return err
			}

			extras := []string{}
			if target != "" {
				extras = append(extras, "target: "+target)
			}
			if testPath != "" {
				extras = append(extras, "test: "+testPath)
			}
			suffix := ""
			if len(extras) > 0 {
				suffix = " (" + strings.Join(extras, ", ") + ")"
			}
			fmt.Fprintf(os.Stdout, "Set %s:%d → %s%s\n", label, number, bucket, suffix)
			return nil
		},
	}
	cmd.Flags().StringVar(&testPath, "test", "",
		"pinned repro test path, e.g. ext/ui/extension_test.go::TestFoo")
	return cmd
}

// --- unset ---------------------------------------------------------------

func newTriageUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <ref>",
		Short: "Remove a triage classification",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			label, number, err := parseRef(args[0])
			if err != nil {
				return err
			}
			repo := a.Cfg.SourceLabelToRepo(label)
			if repo == "" {
				return errf("unknown source label %q", label)
			}

			deleted, err := a.Store.DeleteTriage(repo, number)
			if err != nil {
				return err
			}
			if !deleted {
				fmt.Fprintf(os.Stdout, "%s:%d was not classified; nothing to unset.\n", label, number)
				return nil
			}
			fmt.Fprintf(os.Stdout, "Unset %s:%d\n", label, number)
			return nil
		},
	}
}

// --- close ---------------------------------------------------------------

func newTriageCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <ref> [target]",
		Short: "Flip a classification to 'done' after the target issue closed",
		Long: `Flip a triage row to the 'done' bucket. Refuses unless the target
issue is actually CLOSED in the local DB — so run:

  gapt sync -p <project> target:0

first if the target was just closed upstream. This keeps the 'done'
classification honest: you can't mark something resolved until the
fix has landed and the issue is closed in source control.

If [target] is omitted, the existing target from the triage row is
used. Pass [target] explicitly to override (e.g. when a different
issue actually fixed the gap).`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			ref := args[0]
			override := ""
			if len(args) > 1 {
				override = args[1]
			}

			label, number, err := parseRef(ref)
			if err != nil {
				return err
			}
			repo := a.Cfg.SourceLabelToRepo(label)
			if repo == "" {
				return errf("unknown source label %q", label)
			}

			// Must have an existing triage row.
			existing, err := a.Store.GetTriage(repo, number)
			if err != nil {
				return err
			}
			if existing == nil {
				return errf("%s:%d is not classified; use 'triage set' first", label, number)
			}

			// Resolve the target we're closing against.
			target := override
			if target == "" {
				target = existing.Target
			}
			if target == "" {
				return errf("%s:%d has no target recorded; pass one explicitly: gapt triage close %s <target>",
					label, number, ref)
			}

			// Verify that target is CLOSED in the local DB.
			targetRepo, targetNum, err := parseTargetRef(a.Cfg.TargetRepo, target)
			if err != nil {
				return err
			}
			targetIssue, err := a.Store.GetIssue(targetRepo, targetNum)
			if err != nil {
				return err
			}
			if targetIssue == nil {
				return errf("target %s#%d not in DB. Run: gapt sync -p %s target:0",
					targetRepo, targetNum, a.Cfg.Project)
			}
			if targetIssue.State != "CLOSED" {
				return errf("refusing to close: target %s#%d is still %s. Fix the gap first, or reclassify manually.",
					targetRepo, targetNum, targetIssue.State)
			}

			today := time.Now().UTC().Format("2006-01-02")
			if err := a.Store.CloseTriage(repo, number, target, today); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Closed %s:%d → done (target %s#%d CLOSED)\n",
				label, number, targetRepo, targetNum)
			return nil
		},
	}
}

// parseTargetRef takes a string like "mcpkit#190" or "panyam/mcpkit#190"
// and resolves it to (fullRepo, number). Short forms like "mcpkit" are
// only accepted if they match the configured target repo's trailing
// path segment (so we don't accidentally close against a different repo).
func parseTargetRef(configuredTarget, target string) (string, int, error) {
	idx := strings.LastIndex(target, "#")
	if idx < 0 {
		return "", 0, errf("target must be '<repo>#<number>' or '<short>#<number>', got %q", target)
	}
	repoPart := target[:idx]
	numStr := target[idx+1:]
	var number int
	if _, err := fmt.Sscanf(numStr, "%d", &number); err != nil || number == 0 {
		return "", 0, errf("invalid target issue number in %q", target)
	}

	repo := configuredTarget
	if strings.Contains(repoPart, "/") {
		// Full repo form. Use as-is.
		repo = repoPart
	} else {
		// Short form — verify it matches the configured target's trailing segment.
		if !strings.HasSuffix(configuredTarget, "/"+repoPart) {
			return "", 0, errf("target %q doesn't match configured target_repo %q",
				repoPart, configuredTarget)
		}
	}
	return repo, number, nil
}

// --- list ----------------------------------------------------------------

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
