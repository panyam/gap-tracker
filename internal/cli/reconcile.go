package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/github"
	"github.com/panyam/gap-tracker/internal/store"
)

// newReconcileCmd is `gapt reconcile`. It syncs the target repo to get
// fresh issue states, then walks every open/in-progress gap and auto-
// closes gaps whose target issue has closed upstream.
//
// This is a low-frequency maintenance verb — run it after a wave of
// issues closes upstream so the gaps table doesn't drift stale.
func newReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "[legacy gaps table] Sync target + auto-close resolved gaps (prefer: sync target:0 + triage close)",
		Long: `Reconciles the gaps table with the current state of the target repo:

  1. Syncs the target repo (up to 1000 issues) to refresh issue states.
  2. Walks every gap in 'open' or 'in-progress' status that has a linked
     target_issue.
  3. For each gap whose target issue is now CLOSED in the local DB,
     flips the gap status to 'closed' and appends a reconciliation note.
  4. Prints a table of what changed plus a summary.

This does not touch the triage table — triage close is the verb for
triage-table resolution. reconcile is strictly for the legacy gaps
workflow.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			// Step 1: sync the target repo. High count (1000) because
			// we want a clean snapshot — matches Python.
			fmt.Fprintf(os.Stdout, "=== Syncing target repo: %s ===\n", a.Cfg.TargetRepo)
			issues, err := a.GitHub.FetchIssues(cmd.Context(), a.Cfg.TargetRepo, github.FetchOptions{
				Offset: 0,
				Count:  1000,
			})
			if err != nil {
				return err
			}
			if _, err := a.Store.UpsertIssues(a.Cfg.TargetRepo, issues); err != nil {
				return err
			}
			if err := a.Store.LogSync(a.Cfg.TargetRepo, len(issues), 0, 1000); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "  fetched %d issues\n\n", len(issues))

			// Step 2: find candidate gaps.
			candidates, err := a.Store.ListReconcileCandidates(a.Cfg.TargetRepo)
			if err != nil {
				return err
			}
			if len(candidates) == 0 {
				fmt.Fprintln(os.Stdout, "No open/in-progress gaps with linked issues.")
				return nil
			}

			// Step 3: classify each candidate.
			type closedRow struct {
				Feature  string
				Number   int
				OldState string
				Title    string
			}
			var changed []closedRow
			var unchanged int
			type missingRow struct {
				Feature string
				Number  int
			}
			var missing []missingRow

			today := time.Now().UTC().Format("2006-01-02")
			nowUTC := time.Now().UTC().Format("2006-01-02T15:04:05Z")

			for _, c := range candidates {
				if c.IssueState == nil {
					missing = append(missing, missingRow{c.Feature, c.TargetIssue})
					continue
				}
				if *c.IssueState != "CLOSED" {
					unchanged++
					continue
				}

				note := store.Note{
					Text: fmt.Sprintf("[reconcile] #%d closed — gap resolved (%s)", c.TargetIssue, today),
					At:   nowUTC,
				}
				if err := a.Store.CloseGapWithNote(c.Feature, note); err != nil {
					return err
				}
				title := ""
				if c.IssueTitle != nil {
					title = *c.IssueTitle
				}
				changed = append(changed, closedRow{c.Feature, c.TargetIssue, c.Status, title})
			}

			// Step 4: report.
			if len(changed) > 0 {
				fmt.Fprintf(os.Stdout, "Closed (%d)\n", len(changed))
				rows := make([][]string, 0, len(changed))
				for _, r := range changed {
					rows = append(rows, []string{
						r.Feature, fmt.Sprintf("#%d", r.Number), r.OldState, truncate(r.Title, 60),
					})
				}
				writeTextTable(os.Stdout, []string{"feature", "issue", "was", "issue title"}, rows)
				fmt.Fprintln(os.Stdout)
			}
			if unchanged > 0 {
				fmt.Fprintf(os.Stdout, "Unchanged: %d gaps still open\n", unchanged)
			}
			if len(missing) > 0 {
				fmt.Fprintf(os.Stdout, "Warning: %d gaps reference issues not found in DB:\n", len(missing))
				for _, m := range missing {
					fmt.Fprintf(os.Stdout, "  %s → #%d\n", m.Feature, m.Number)
				}
			}
			fmt.Fprintf(os.Stdout, "\nReconciled: %d closed, %d unchanged, %d missing\n",
				len(changed), unchanged, len(missing))
			return nil
		},
	}
}

// truncate is a small helper used by reconcile and (eventually) other
// legacy verbs. Matches render.truncate but lives here to avoid a
// cross-package dependency.
func truncate(s string, n int) string {
	if runeCount(s) <= n {
		return s
	}
	// Walk runes to handle multi-byte cleanly.
	out := make([]rune, 0, n)
	for _, r := range s {
		if len(out) >= n-1 {
			break
		}
		out = append(out, r)
	}
	return string(out) + "…"
}
