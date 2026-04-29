package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/render"
	"github.com/panyam/gap-tracker/internal/store"
)

// newSearchCmd is `gapt search <project> <query> [--source ...] [--bucket ...] [--state ...] [--limit ...]`.
//
// Full-text search across issue titles, bodies, labels, and notes using
// SQLite FTS5. Results are ranked by bm25 relevance and joined against
// the triage table for classification context.
func newSearchCmd() *cobra.Command {
	var (
		source string
		bucket string
		state  string
		limit  int
	)

	cmd := &cobra.Command{
		Use:   "search <project> <query...>",
		Short: "Full-text search across issues (FTS5)",
		Long: `Search issue titles, bodies, labels, and notes using FTS5 syntax.

Examples:
  gapt search mcpkit oauth token
  gapt search mcpkit "session recovery" --source go-sdk
  gapt search mcpkit streaming --bucket real-gap
  gapt search mcpkit auth* --state OPEN --limit 10

Query syntax (FTS5):
  bare words      match anywhere
  "phrase"         exact phrase
  word1 AND word2  both required
  word1 OR word2   either matches
  word1 NOT word2  exclude
  prefix*          prefix match`,
		Args: cobra.MinimumNArgs(2), // project + at least one query word
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			query := strings.Join(rest, " ")

			// Resolve --source label to repo name.
			repo := source
			if repo != "" {
				resolved := a.Cfg.SourceLabelToRepo(repo)
				if resolved == "" {
					return fmt.Errorf("unknown source label %q", repo)
				}
				repo = resolved
			}

			results, err := a.Store.Search(query, store.SearchFilter{
				Repo:   repo,
				Bucket: bucket,
				State:  state,
				Limit:  limit,
			})
			if err != nil {
				return err
			}

			report := render.SearchReport{
				Project: a.Cfg.Project,
				Query:   query,
			}
			for _, r := range results {
				report.Hits = append(report.Hits, render.SearchHit{
					Ref:     a.Cfg.RepoToSourceLabel(r.Repo) + ":" + itoa(r.Number),
					Title:   r.Title,
					State:   r.State,
					Snippet: r.Snippet,
					Rank:    r.Rank,
					Bucket:  r.Bucket,
					Target:  r.Target,
				})
			}

			return a.Renderer.RenderSearch(os.Stdout, report)
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "filter to one source label (e.g. go-sdk, spec)")
	cmd.Flags().StringVar(&bucket, "bucket", "", "filter to one triage bucket")
	cmd.Flags().StringVar(&state, "state", "", "filter by issue state (OPEN/CLOSED)")
	cmd.Flags().IntVar(&limit, "limit", 50, "max results")

	return cmd
}
