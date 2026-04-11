package cli

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
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

			targetIssues, err := a.Store.AllIssuesByRepo(a.Cfg.TargetRepo)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "=== Cross-reference: %s ===\n\n", a.Cfg.Project)

			for _, src := range a.Cfg.Sources {
				srcIssues, err := a.Store.OpenIssuesByRepoWithBody(src.Repo)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "--- %s (%d open) → %s ---\n",
					src.Label, len(srcIssues), a.Cfg.TargetRepo)

				type match struct {
					SrcNum   int
					SrcTitle string
					TgtNum   int
					TgtTitle string
					Score    int
				}
				var matched []match
				var unmatched []store.IssueWithBody

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
						matched = append(matched, match{
							SrcNum:   si.Number,
							SrcTitle: si.Title,
							TgtNum:   best.Number,
							TgtTitle: best.Title,
							Score:    bestScore,
						})
					} else {
						unmatched = append(unmatched, si)
					}
				}

				sort.Slice(matched, func(i, j int) bool {
					return matched[i].Score > matched[j].Score
				})

				for _, m := range matched {
					fmt.Fprintf(os.Stdout, "  %s#%d → #%d (score=%d)\n",
						src.Label, m.SrcNum, m.TgtNum, m.Score)
					fmt.Fprintf(os.Stdout, "    src: %s\n", truncate(m.SrcTitle, 70))
					fmt.Fprintf(os.Stdout, "    tgt: %s\n", truncate(m.TgtTitle, 70))
				}

				if len(unmatched) > 0 {
					fmt.Fprintf(os.Stdout, "\n  Unmatched (%d):\n", len(unmatched))
					shown := unmatched
					if len(shown) > 20 {
						shown = shown[:20]
					}
					for _, si := range shown {
						fmt.Fprintf(os.Stdout, "    %s#%d: %s\n",
							src.Label, si.Number, truncate(si.Title, 70))
					}
					if len(unmatched) > 20 {
						fmt.Fprintf(os.Stdout, "    ... and %d more\n", len(unmatched)-20)
					}
				}
				fmt.Fprintln(os.Stdout)
			}

			return nil
		},
	}
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
