package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/store"
)

func newSettleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "settle <project>",
		Short: "Bulk-close real-gap entries whose target issues are now closed",
		Long: `Iterates every real-gap triage row and flips it to 'done' if the
target issue is CLOSED in the local DB. Rows whose targets are still
open are silently skipped.

Run 'gapt sync <project> target:0' first if target issues were
recently closed upstream and haven't been synced yet.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			rows, err := a.Store.ListTriage(store.ListTriageFilter{Bucket: "real-gap"})
			if err != nil {
				return err
			}

			today := time.Now().UTC().Format("2006-01-02")
			closed, skipped, notInDB := 0, 0, 0

			for _, r := range rows {
				if r.Target == "" {
					skipped++
					continue
				}

				targetRepo, targetNum, err := parseTargetRef(a.Cfg.TargetRepo, r.Target)
				if err != nil {
					skipped++
					continue
				}

				issue, err := a.Store.GetIssue(targetRepo, targetNum)
				if err != nil {
					skipped++
					continue
				}
				if issue == nil {
					notInDB++
					skipped++
					continue
				}
				if issue.State != "CLOSED" {
					skipped++
					continue
				}

				label := a.Cfg.RepoToSourceLabel(r.Repo)
				if err := a.Store.CloseTriage(r.Repo, r.Number, r.Target, today); err != nil {
					fmt.Fprintf(os.Stderr, "  %s:%d — error: %v\n", label, r.Number, err)
					skipped++
					continue
				}
				fmt.Fprintf(os.Stdout, "  Closed %s:%d → done (target %s CLOSED)\n",
					label, r.Number, r.Target)
				closed++
			}

			fmt.Fprintf(os.Stdout, "\nSettled: %d closed, %d unchanged\n", closed, skipped)
			if notInDB > 0 {
				fmt.Fprintf(os.Stderr, "\nHint: %d target issues not in local DB. Run 'gapt sync %s target:0' to refresh.\n",
					notInDB, a.Cfg.Project)
			}
			return nil
		},
	}
}
