package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/config"
	"github.com/panyam/gap-tracker/internal/render"
	"github.com/panyam/gap-tracker/internal/store"
)

// newNoteCmd is `gapt note <ref> <text> [--set]`.
//
// Ref accepts both "source:N" (source issue) and bare "N" or "#N"
// (target repo issue), matching Python's _resolve_issue_ref. Append
// is the default; --set overwrites all notes on the issue.
func newNoteCmd() *cobra.Command {
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "note <project> <ref> <text>",
		Short: "Append (or --set overwrite) a note on an issue",
		Long: `Add a free-form note to an issue. Notes are stored as a JSON array in
the issues.notes column. Ref accepts either "<source>:<number>" for a
source-repo issue or "<number>" / "#<number>" for a target-repo issue.

Examples:
  gapt note mcpkit go-sdk:148 "OAuth token caching"
  gapt note mcpkit go-sdk:148 "[pass] test_oauth_refresh passes"
  gapt note mcpkit 42 "scratchpad note on mcpkit#42"
  gapt note mcpkit #42 "same as above, # prefix tolerated"
  gapt note mcpkit 42 "rewritten" --set        # overwrites all notes`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			ref := rest[0]
			text := strings.Join(rest[1:], " ")

			repo, num, err := parseRefOrTarget(a.Cfg, ref)
			if err != nil {
				return err
			}

			entry := store.Note{
				Text: text,
				At:   time.Now().UTC().Format("2006-01-02"),
			}

			var notes []store.Note
			if overwrite {
				notes = []store.Note{entry}
			} else {
				existing, err := a.Store.GetNotes(repo, num)
				if err != nil {
					return err
				}
				notes = append(existing, entry)
			}

			if err := a.Store.SetNotes(repo, num, notes); err != nil {
				return err
			}

			verb := "Added"
			if overwrite {
				verb = "Set"
			}
			fmt.Fprintf(os.Stdout, "%s note on %s#%d: %s\n", verb, repo, num, text)
			return nil
		},
	}
	cmd.Flags().BoolVar(&overwrite, "set", false,
		"overwrite all notes on the issue instead of appending")
	return cmd
}

// newNotesCmd is `gapt notes [filter]` — list issues with non-empty notes.
func newNotesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "notes <project> [filter]",
		Short: "List issues with notes, optionally filtered by substring",
		Long: `Lists every issue whose notes column is non-empty, optionally
filtered by substring (matched against the raw JSON). Useful for
finding issues tagged with e.g. "[fail]", "route:servicekit", or
any other marker you've scattered through notes.

Examples:
  gapt notes mcpkit                     list everything with notes
  gapt notes mcpkit route:servicekit    filter to routing-tagged notes
  gapt notes mcpkit '[fail]'            filter to failed-validation notes`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			filter := ""
			if len(rest) > 0 {
				filter = rest[0]
			}

			summaries, err := a.Store.ListIssuesWithNotes(filter)
			if err != nil {
				return err
			}
			view := buildNotesListView(a, summaries, filter)
			return a.Renderer.RenderNotesList(os.Stdout, view)
		},
	}
}

// buildNotesListView maps store.IssueNoteSummary rows into the
// render.NotesListView. Stays in cli (not store) because it needs
// config for label↔repo resolution.
func buildNotesListView(a *app.App, summaries []store.IssueNoteSummary, filter string) render.NotesListView {
	target := a.Cfg.TargetRepo
	items := make([]render.NotesListItem, 0, len(summaries))
	for _, s := range summaries {
		var ref string
		if s.Repo == target {
			ref = "#" + itoa(s.Number)
		} else {
			label := a.Cfg.RepoToSourceLabel(s.Repo)
			ref = label + ":" + itoa(s.Number)
		}
		latest := ""
		all := make([]string, 0, len(s.Notes))
		for _, n := range s.Notes {
			all = append(all, n.Text)
		}
		if len(s.Notes) > 0 {
			latest = s.Notes[len(s.Notes)-1].Text
		}
		items = append(items, render.NotesListItem{
			Ref:    ref,
			State:  s.State,
			Title:  s.Title,
			Latest: latest,
			Count:  len(s.Notes),
			Notes:  all,
		})
	}
	return render.NotesListView{
		Filter: filter,
		Issues: items,
	}
}

// parseRefOrTarget resolves either "source:N", "N", or "#N" into
// (repo, number). The bare-number form resolves to the configured
// target repo — matches Python _resolve_issue_ref semantics.
func parseRefOrTarget(cfg *config.Config, ref string) (string, int, error) {
	if strings.Contains(ref, ":") {
		label, num, err := parseRef(ref)
		if err != nil {
			return "", 0, err
		}
		repo := cfg.SourceLabelToRepo(label)
		if repo == "" {
			return "", 0, errf("unknown source label %q", label)
		}
		return repo, num, nil
	}
	// Bare number form — strip optional "#" prefix and resolve to target.
	stripped := strings.TrimPrefix(ref, "#")
	num, err := strconv.Atoi(stripped)
	if err != nil {
		return "", 0, errf("ref must be '<source>:<number>', '<number>', or '#<number>'; got %q", ref)
	}
	return cfg.TargetRepo, num, nil
}
