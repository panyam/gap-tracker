package cli

import (
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/render"
	"github.com/panyam/gap-tracker/internal/store"
)

// newCrossrefCmd is `gapt crossref` — heuristic match from source issues
// to target issues by keyword overlap. Read-only; prints suggestions.
//
// Algorithm (ported from Python _match_score):
//   - Normalize text to a set of 3+ char lowercase words, minus a small
//     stopword list.
//   - Compute intersection of normalized(src.title + src.body[:500])
//     and normalized(tgt.title + tgt.body[:500]).
//   - Score = 2 * body_overlap + 5 * title_overlap.
//   - Pairs with score >= 15 are reported as matches, sorted by score.
func newCrossrefCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "crossref",
		Short: "Suggest matches from source issues to target issues by keyword overlap",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			report, err := buildCrossrefReport(a)
			if err != nil {
				return err
			}
			return a.Renderer.RenderCrossref(os.Stdout, report)
		},
	}
}

// buildCrossrefReport runs the full keyword-match sweep and packs the
// result into a render.CrossrefReport. All formatting decisions stay
// in the renderer; this function is pure data.
func buildCrossrefReport(a *app.App) (render.CrossrefReport, error) {
	targetIssues, err := a.Store.AllIssuesByRepo(a.Cfg.TargetRepo)
	if err != nil {
		return render.CrossrefReport{}, err
	}

	report := render.CrossrefReport{
		Project: a.Cfg.Project,
		Target:  a.Cfg.TargetRepo,
	}
	for _, src := range a.Cfg.Sources {
		srcIssues, err := a.Store.OpenIssuesByRepoWithBody(src.Repo)
		if err != nil {
			return render.CrossrefReport{}, err
		}

		section := render.CrossrefSection{
			Label:     src.Label,
			Repo:      src.Repo,
			OpenCount: len(srcIssues),
		}

		for _, si := range srcIssues {
			bestScore := 0
			var best store.IssueWithBody
			for _, ti := range targetIssues {
				score := matchScore(si.Title, si.Body, ti.Title, ti.Body)
				if score > bestScore {
					bestScore = score
					best = ti
				}
			}
			if bestScore >= 15 {
				section.Matched = append(section.Matched, render.CrossrefMatch{
					SrcRef:   src.Label + "#" + itoa(si.Number),
					SrcTitle: si.Title,
					TgtRef:   "#" + itoa(best.Number),
					TgtTitle: best.Title,
					Score:    bestScore,
				})
			} else {
				section.Unmatched = append(section.Unmatched, render.CrossrefMiss{
					Ref:   src.Label + "#" + itoa(si.Number),
					Title: si.Title,
				})
			}
		}

		sort.Slice(section.Matched, func(i, j int) bool {
			return section.Matched[i].Score > section.Matched[j].Score
		})

		report.Sources = append(report.Sources, section)
	}
	return report, nil
}

// crossrefStopwords is the small common-word exclusion set from
// Python's _normalize. Kept as a package var so matchScore doesn't
// rebuild it on every call.
var crossrefStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"for": true, "to": true, "of": true, "in": true, "on": true,
	"with": true, "and": true, "or": true, "not": true, "this": true,
	"that": true, "add": true, "implement": true, "support": true,
	"proposal": true,
}

// crossrefWordRe matches "words" for the normalize step: 3+ chars,
// first char is a letter, rest are letters/digits. Matches Python's
// r"[a-z][a-z0-9]{2,}" applied after lowercase.
var crossrefWordRe = regexp.MustCompile(`[a-z][a-z0-9]{2,}`)

// normalizeWords extracts the set of non-stopword tokens from text.
func normalizeWords(text string) map[string]bool {
	lower := strings.ToLower(text)
	tokens := crossrefWordRe.FindAllString(lower, -1)
	out := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		if !crossrefStopwords[t] {
			out[t] = true
		}
	}
	return out
}

// matchScore computes the Python _match_score: 2 * body overlap + 5 * title overlap.
// Body is truncated at 500 chars (matches Python).
func matchScore(srcTitle, srcBody, tgtTitle, tgtBody string) int {
	srcBody = clipTo(srcBody, 500)
	tgtBody = clipTo(tgtBody, 500)

	srcAll := normalizeWords(srcTitle + " " + srcBody)
	tgtAll := normalizeWords(tgtTitle + " " + tgtBody)
	if len(srcAll) == 0 || len(tgtAll) == 0 {
		return 0
	}
	bodyOverlap := setIntersectSize(srcAll, tgtAll)

	srcTitleSet := normalizeWords(srcTitle)
	tgtTitleSet := normalizeWords(tgtTitle)
	titleOverlap := setIntersectSize(srcTitleSet, tgtTitleSet)

	return bodyOverlap*2 + titleOverlap*5
}

// setIntersectSize returns |a ∩ b| for two string sets.
func setIntersectSize(a, b map[string]bool) int {
	if len(a) > len(b) {
		a, b = b, a // iterate the smaller set
	}
	n := 0
	for k := range a {
		if b[k] {
			n++
		}
	}
	return n
}

// clipTo returns the first n runes of s. Used to cap body lengths
// before normalization so long descriptions don't dominate the score.
func clipTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Byte slice is fine here — we only read the result for word
	// extraction, not for display, and the regex handles partial
	// multi-byte sequences gracefully.
	return s[:n]
}
