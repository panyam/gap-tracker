package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/github"
)

// newSyncCmd handles `gapt sync <source>:<offset> [--count N]`.
//
// The ref:offset shape matches the Python CLI so muscle memory is
// preserved. Examples:
//
//	gapt sync -p mcpkit ext-apps:0           # first 100 ext-apps issues
//	gapt sync -p mcpkit ext-apps:100         # next 100
//	gapt sync -p mcpkit target:0 --count 50  # first 50 target issues
func newSyncCmd() *cobra.Command {
	var count int
	cmd := &cobra.Command{
		Use:   "sync <project> <source>:<offset>",
		Short: "Sync issues for one source (or target) from GitHub into the local DB",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			label, offset, err := parseRefOffset(rest[0])
			if err != nil {
				return err
			}
			repo := a.Cfg.SourceLabelToRepo(label)
			if repo == "" {
				return errf("unknown source label %q", label)
			}
			return syncOne(cmd.Context(), a, label, repo, offset, count)
		},
	}
	cmd.Flags().IntVar(&count, "count", 100, "how many issues to fetch")
	return cmd
}

// newSyncAllCmd is the everyday convenience verb — sync every source +
// the target, each from offset 0. Saves forgetting `sync target:0`.
func newSyncAllCmd() *cobra.Command {
	var count int
	cmd := &cobra.Command{
		Use:   "sync-all <project>",
		Short: "Sync every source + target from offset 0 in one shot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			fmt.Fprintf(os.Stdout, "=== Gap Tracker: %s — sync-all ===\n", a.Cfg.Project)
			fmt.Fprintf(os.Stdout, "Time: %s\n\n",
				time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

			ctx := cmd.Context()
			// Every source, then target.
			for _, src := range a.Cfg.Sources {
				fmt.Fprintf(os.Stdout, "[%s] %s\n", src.Label, src.Repo)
				if err := syncOne(ctx, a, src.Label, src.Repo, 0, count); err != nil {
					return err
				}
			}
			fmt.Fprintf(os.Stdout, "[target] %s\n", a.Cfg.TargetRepo)
			if err := syncOne(ctx, a, "target", a.Cfg.TargetRepo, 0, count); err != nil {
				return err
			}

			total, err := a.Store.CountIssuesByRepo("") // empty repo ⇒ total, see below
			_ = total
			// Count across all repos for the footer
			var allTotal int64
			a.Store.DB().Table("issues").Count(&allTotal)
			fmt.Fprintf(os.Stdout, "\nTotal issues in DB: %d\n", allTotal)
			return err
		},
	}
	cmd.Flags().IntVar(&count, "count", 100, "how many issues to fetch per source")
	return cmd
}

// syncOne does a single (label, repo, offset, count) fetch + upsert.
// Extracted so both sync and sync-all share the work.
func syncOne(ctx context.Context, a *app.App, label, repo string, offset, count int) error {
	fmt.Fprintf(os.Stdout, "  Syncing %s (offset=%d, count=%d)...\n", repo, offset, count)
	issues, err := a.GitHub.FetchIssues(ctx, repo, github.FetchOptions{
		Offset: offset,
		Count:  count,
	})
	if err != nil {
		return fmt.Errorf("fetch %s: %w", repo, err)
	}

	if _, err := a.Store.UpsertIssues(repo, issues); err != nil {
		return fmt.Errorf("upsert %s: %w", repo, err)
	}
	if err := a.Store.LogSync(repo, len(issues), offset, count); err != nil {
		return fmt.Errorf("log sync %s: %w", repo, err)
	}

	if len(issues) > 0 {
		lo := issues[0].Number
		hi := issues[0].Number
		for _, i := range issues {
			if i.Number < lo {
				lo = i.Number
			}
			if i.Number > hi {
				hi = i.Number
			}
		}
		fmt.Fprintf(os.Stdout, "    %d issues (#%d-#%d)\n", len(issues), lo, hi)
	} else {
		fmt.Fprintln(os.Stdout, "    0 issues")
	}
	return nil
}

// parseRefOffset splits "<label>:<offset>" into (label, offset). Returns
// a helpful error if the shape is wrong.
func parseRefOffset(s string) (string, int, error) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return "", 0, errf("ref must be '<source>:<offset>' (e.g. ext-apps:0), got %q", s)
	}
	label := s[:idx]
	off, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return "", 0, errf("invalid offset in %q: %v", s, err)
	}
	return label, off, nil
}
