package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/render"
)

// newShowCmd handles `gapt show <ref>` where ref is "source:number" like
// "ext-apps:298". Mirrors Python's cmd_show exactly: issue header, triage
// block from the triage table (not from notes), ad-hoc notes excluding
// legacy [triage:...] entries, and optional linked gap.
func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <project> <source>:<number>",
		Short: "Show details for a source issue (title, body, triage, notes, linked gap)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			view, err := buildIssueView(a, rest[0])
			if err != nil {
				return err
			}
			return a.Renderer.RenderIssue(os.Stdout, view)
		},
	}
}

func buildIssueView(a *app.App, ref string) (render.IssueView, error) {
	label, num, err := parseRef(ref)
	if err != nil {
		return render.IssueView{}, err
	}
	repo := a.Cfg.SourceLabelToRepo(label)
	if repo == "" {
		return render.IssueView{}, errf("unknown source label %q", label)
	}

	issue, err := a.Store.GetIssue(repo, num)
	if err != nil {
		return render.IssueView{}, err
	}
	if issue == nil {
		return render.IssueView{}, errf("%s#%d not in DB — run sync first", repo, num)
	}

	v := render.IssueView{
		Repo:    repo,
		Number:  issue.Number,
		Title:   issue.Title,
		State:   issue.State,
		Labels:  issue.Labels,
		URL:     issue.URL,
		Created: issue.CreatedAt,
		Updated: issue.UpdatedAt,
		Body:    issue.Body,
	}

	// Triage row (authoritative classification)
	t, err := a.Store.GetTriage(repo, num)
	if err != nil {
		return render.IssueView{}, err
	}
	if t != nil {
		v.Triage = &render.TriageBlock{
			Bucket:       t.Bucket,
			Target:       t.Target,
			Rationale:    t.Rationale,
			Test:         t.Test,
			ClassifiedAt: t.ClassifiedAt,
			UpdatedAt:    t.UpdatedAt,
		}
	}

	// Ad-hoc notes, excluding legacy [triage:...] entries (superseded)
	v.Notes = parseNotes(issue.Notes)

	// Legacy gap row (optional)
	gaps, err := a.Store.ListGapsBySourceRef(ref)
	if err != nil {
		return render.IssueView{}, err
	}
	if len(gaps) > 0 {
		g := gaps[0] // first match; typically only one
		block := &render.LinkedGapBlock{
			Feature:    g.Feature,
			TargetRepo: a.Cfg.TargetRepo,
			GapStatus:  g.Status,
		}
		if g.TargetIssue != nil {
			block.TargetIssue = *g.TargetIssue
			target, _ := a.Store.GetIssue(a.Cfg.TargetRepo, *g.TargetIssue)
			if target != nil {
				block.TargetTitle = target.Title
				block.TargetState = target.State
			}
		}
		v.Gap = block
	}

	return v, nil
}

// parseRef splits "source:number" into (label, number). Leading "#N"
// forms are not accepted — we need the source label to know which repo
// to query. Python accepted bare "#72" only for target-repo issues, but
// we're keeping the Go surface stricter.
func parseRef(ref string) (string, int, error) {
	idx := strings.LastIndex(ref, ":")
	if idx < 0 {
		return "", 0, errf("ref must be '<source>:<number>' (e.g. ext-apps:298), got %q", ref)
	}
	label := ref[:idx]
	numStr := ref[idx+1:]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return "", 0, errf("invalid issue number in %q: %v", ref, err)
	}
	return label, n, nil
}

// parseNotes decodes the JSON notes column and filters out legacy
// [triage:...] entries (the DB-primary refactor superseded those —
// triage lives in its own table now).
func parseNotes(raw string) []render.NoteEntry {
	if raw == "" || raw == "[]" {
		return nil
	}
	type dbNote struct {
		Text string `json:"text"`
		At   string `json:"at"`
	}
	var notes []dbNote
	if err := jsonDecode(raw, &notes); err != nil {
		// Corrupted or legacy format — display as-is in a single entry.
		return []render.NoteEntry{{Text: raw}}
	}
	out := make([]render.NoteEntry, 0, len(notes))
	for _, n := range notes {
		if strings.HasPrefix(n.Text, "[triage:") {
			continue
		}
		out = append(out, render.NoteEntry{Text: n.Text, At: n.At})
	}
	return out
}

// errf is a tiny helper so cli code reads more like the Python source
// (`errf("not in DB")` vs `fmt.Errorf("not in DB")`).
func errf(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}
