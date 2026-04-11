package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/github"
	"github.com/panyam/gap-tracker/internal/render"
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

			report, err := runReconcile(cmd.Context(), a)
			if err != nil {
				return err
			}
			return a.Renderer.RenderReconcile(os.Stdout, report)
		},
	}
}

// runReconcile performs the sync + gap-close logic and returns the
// report for the renderer. Side effects: writes to `issues` and
// `gaps` tables, appends to sync_log. Does not print.
func runReconcile(ctx context.Context, a *app.App) (render.ReconcileReport, error) {
	// Step 1: sync the target repo at high count.
	issues, err := a.GitHub.FetchIssues(ctx, a.Cfg.TargetRepo, github.FetchOptions{
		Offset: 0,
		Count:  1000,
	})
	if err != nil {
		return render.ReconcileReport{}, err
	}
	if _, err := a.Store.UpsertIssues(a.Cfg.TargetRepo, issues); err != nil {
		return render.ReconcileReport{}, err
	}
	if err := a.Store.LogSync(a.Cfg.TargetRepo, len(issues), 0, 1000); err != nil {
		return render.ReconcileReport{}, err
	}

	report := render.ReconcileReport{
		TargetRepo:  a.Cfg.TargetRepo,
		SyncedCount: len(issues),
	}

	// Step 2: find candidate gaps.
	candidates, err := a.Store.ListReconcileCandidates(a.Cfg.TargetRepo)
	if err != nil {
		return render.ReconcileReport{}, err
	}
	if len(candidates) == 0 {
		return report, nil
	}

	// Step 3: walk candidates and classify.
	today := time.Now().UTC().Format("2006-01-02")
	nowUTC := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	for _, c := range candidates {
		if c.IssueState == nil {
			report.Missing = append(report.Missing, render.ReconcileMiss{
				Feature: c.Feature, Number: c.TargetIssue,
			})
			continue
		}
		if *c.IssueState != "CLOSED" {
			report.UnchangedCount++
			continue
		}

		note := store.Note{
			Text: fmt.Sprintf("[reconcile] #%d closed — gap resolved (%s)", c.TargetIssue, today),
			At:   nowUTC,
		}
		if err := a.Store.CloseGapWithNote(c.Feature, note); err != nil {
			return render.ReconcileReport{}, err
		}
		title := ""
		if c.IssueTitle != nil {
			title = *c.IssueTitle
		}
		report.Closed = append(report.Closed, render.ReconcileRow{
			Feature:  c.Feature,
			Number:   c.TargetIssue,
			OldState: c.Status,
			Title:    title,
		})
	}
	return report, nil
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
