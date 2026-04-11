package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/store"
)

// newLinkCmd is `gapt link <feature> <target_issue#> [source_evidence]`.
//
// Creates or updates a row in the legacy gaps table. Duplicate source
// evidence check is done in the store. After a successful link, the
// target issue is lazily fetched from GitHub and upserted into the
// issues table so `report` and `show` can display its title.
func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <feature> <target_issue#> [source_evidence]",
		Short: "[legacy gaps table] Link a feature to a target issue (prefer: triage set)",
		Long: `Creates or updates a row in the legacy 'gaps' table. Feature name is
the unique key — re-linking with the same feature updates the target
issue and source evidence.

Source evidence is a comma-separated list of source issue refs, e.g.
"go-sdk:148,spec:282". The command refuses if any of those refs is
already linked to a different feature.

After linking, the target issue is fetched from GitHub and cached in
the local issues table so 'report' and 'show' can display its title.

Examples:
  gapt link session-timeout 60 "go-sdk:148,spec:282"
  gapt link oauth-caching 42`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			feature := args[0]
			targetIssue, err := strconv.Atoi(args[1])
			if err != nil {
				return errf("target issue must be an integer, got %q", args[1])
			}
			sourceEvidence := ""
			if len(args) > 2 {
				sourceEvidence = args[2]
			}

			if err := a.Store.LinkGap(feature, targetIssue, sourceEvidence); err != nil {
				return err
			}

			// Lazily cache the target issue so its title is available for
			// report/show. Skip if targetIssue is 0 (deferred/NA).
			if targetIssue > 0 {
				if err := ensureTargetCached(cmd, a, targetIssue); err != nil {
					// Cache failure is non-fatal; warn and keep going.
					fmt.Fprintf(os.Stderr, "WARN: could not cache target %s#%d: %v\n",
						a.Cfg.TargetRepo, targetIssue, err)
				}
			}

			label := "none"
			if sourceEvidence != "" {
				label = sourceEvidence
			}
			fmt.Fprintf(os.Stdout, "Linked: %s → #%d (evidence: %s)\n", feature, targetIssue, label)
			return nil
		},
	}
}

// ensureTargetCached checks whether the target issue is already in the
// issues table; if not, fetches it via github.Client and upserts.
// Non-fatal — cache failures don't block linking.
func ensureTargetCached(cmd *cobra.Command, a *app.App, number int) error {
	existing, err := a.Store.GetIssue(a.Cfg.TargetRepo, number)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	fetched, err := a.GitHub.FetchIssue(cmd.Context(), a.Cfg.TargetRepo, number)
	if err != nil {
		return err
	}
	if fetched == nil {
		return nil // 404 — silently skip, matches Python
	}

	labelsCSV := strings.Join(fetched.Labels, ",")
	payload := store.NewFetchedIssue(
		fetched.Number, fetched.Title, fetched.State,
		labelsCSV,
		fetched.CreatedAt, fetched.UpdatedAt, fetched.URL, fetched.Body,
	)
	_, err = a.Store.EnsureTargetIssue(a.Cfg.TargetRepo, number, payload)
	return err
}

// newMarkCmd is `gapt mark <feature> <status>`. Plain status update on
// the gaps table — matches Python which doesn't check if the feature
// exists first. Call it with a bogus feature name and the underlying
// UPDATE is a no-op; we don't error.
func newMarkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mark <feature> <status>",
		Short: "[legacy gaps table] Update a gap's status (prefer: triage set / triage close)",
		Long: `Set the status column on a gap row by feature name. Common statuses:
open, closed, deferred, not-applicable, in-progress, done.

This is a no-existence-check command — calling mark with an unknown
feature does nothing and prints the usual output. Matches Python
behavior.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			feature := args[0]
			status := args[1]
			if err := a.Store.MarkGap(feature, status); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Marked: %s → %s\n", feature, status)
			return nil
		},
	}
}
