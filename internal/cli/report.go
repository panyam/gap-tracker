package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/model"
)

// newReportCmd is `gapt report [--by-priority]`.
//
// Per-source view of tracked vs untracked open issues, followed by a
// grouped summary of all tracked gaps (open / deferred / closed). This
// is the legacy entry point — the canonical daily-driver view is
// `gapt triage status` / `gapt triage list`.
func newReportCmd() *cobra.Command {
	var byPriority bool
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Per-source view of tracked/untracked open issues + all-gaps summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			fmt.Fprintf(os.Stdout, "\nGap Report: %s  (target: %s)\n\n",
				a.Cfg.Project, a.Cfg.TargetRepo)

			// Build evidence → gap lookup once.
			gaps, err := a.Store.AllGaps()
			if err != nil {
				return err
			}
			evidenceToGap := map[string]*model.Gap{}
			for i := range gaps {
				g := &gaps[i]
				for _, rawRef := range strings.Split(g.SourceEvidence, ",") {
					ref := strings.TrimSpace(rawRef)
					if ref != "" {
						evidenceToGap[ref] = g
					}
				}
			}

			ourIssueCache := map[int]*model.Issue{}
			lookupOurIssue := func(num int) *model.Issue {
				if num <= 0 {
					return nil
				}
				if cached, ok := ourIssueCache[num]; ok {
					return cached
				}
				issue, _ := a.Store.GetIssue(a.Cfg.TargetRepo, num)
				ourIssueCache[num] = issue
				return issue
			}

			// Per-source sections.
			for _, src := range a.Cfg.Sources {
				srcIssues, err := a.Store.OpenIssuesByRepoFull(src.Repo)
				if err != nil {
					return err
				}

				type trackedRow struct {
					SrcIssue model.Issue
					Gap      *model.Gap
				}
				var tracked []trackedRow
				var untracked []model.Issue
				for _, si := range srcIssues {
					ref := src.Label + ":" + itoa(si.Number)
					if gap := evidenceToGap[ref]; gap != nil {
						tracked = append(tracked, trackedRow{SrcIssue: si, Gap: gap})
					} else {
						untracked = append(untracked, si)
					}
				}

				if byPriority {
					sort.SliceStable(untracked, func(i, j int) bool {
						pi := labelPriorityScore(untracked[i].Labels)
						pj := labelPriorityScore(untracked[j].Labels)
						if pi != pj {
							return pi < pj
						}
						return untracked[i].Number > untracked[j].Number
					})
				}

				fmt.Fprintf(os.Stdout, "=== %s (%s) — %d open ===\n\n", src.Label, src.Repo, len(srcIssues))

				if len(tracked) > 0 {
					fmt.Fprintf(os.Stdout, "Tracked (%d)\n", len(tracked))
					rows := make([][]string, 0, len(tracked))
					for _, r := range tracked {
						badge := statusBadge(r.Gap.Status)
						srcRef := fmt.Sprintf("%s#%d", src.Label, r.SrcIssue.Number)
						oursNum := 0
						if r.Gap.TargetIssue != nil {
							oursNum = *r.Gap.TargetIssue
						}
						oursRef := "—"
						if oursNum > 0 {
							oursRef = fmt.Sprintf("#%d", oursNum)
						}
						oursTitle := ""
						if oi := lookupOurIssue(oursNum); oi != nil {
							oursTitle = truncate(oi.Title, 60)
						}
						rows = append(rows, []string{
							badge, srcRef, truncate(r.SrcIssue.Title, 60), oursRef, oursTitle,
						})
					}
					writeTextTable(os.Stdout,
						[]string{"", "source", "source title", "ours", "our title"}, rows)
					fmt.Fprintln(os.Stdout)
				}

				if len(untracked) > 0 {
					fmt.Fprintf(os.Stdout, "Untracked (%d)\n", len(untracked))
					rows := make([][]string, 0, len(untracked))
					for _, si := range untracked {
						rows = append(rows, []string{
							fmt.Sprintf("%s#%d", src.Label, si.Number),
							truncate(si.Title, 70),
							truncate(si.Labels, 30),
						})
					}
					writeTextTable(os.Stdout,
						[]string{"ref", "title", "labels"}, rows)
					fmt.Fprintln(os.Stdout)
				}
			}

			// All-tracked-gaps summary.
			if len(gaps) > 0 {
				printGapsSummary(os.Stdout, gaps, lookupOurIssue)
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&byPriority, "by-priority", false,
		"sort untracked issues by label priority (bugs/security first)")
	return cmd
}

// printGapsSummary emits the three grouped tables (open / deferred /
// closed) below the per-source sections. Called only if there are
// gap rows at all.
func printGapsSummary(w *os.File, gaps []model.Gap, lookupOurIssue func(int) *model.Issue) {
	var open, deferred, closed []model.Gap
	for _, g := range gaps {
		switch g.Status {
		case "open", "in-progress":
			open = append(open, g)
		case "deferred":
			deferred = append(deferred, g)
		case "closed", "not-applicable":
			closed = append(closed, g)
		}
	}

	// Sort key matches Python: priority tier, then status sort, then feature.
	sortGaps := func(xs []model.Gap) {
		sort.SliceStable(xs, func(i, j int) bool {
			pi := priorityTier(xs[i].Priority)
			pj := priorityTier(xs[j].Priority)
			if pi != pj {
				return pi < pj
			}
			si := statusSortRank(xs[i].Status)
			sj := statusSortRank(xs[j].Status)
			if si != sj {
				return si < sj
			}
			return xs[i].Feature < xs[j].Feature
		})
	}

	if len(open) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "=== All Tracked Gaps ===")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Open gaps (%d)\n", len(open))
		sortGaps(open)
		writeTextTable(w,
			[]string{"", "evidence", "ours", "title", "pri"},
			gapRows(open, lookupOurIssue))
		fmt.Fprintln(w)
	}

	if len(deferred) > 0 {
		fmt.Fprintf(w, "Deferred (%d)\n", len(deferred))
		sortGaps(deferred)
		writeTextTable(w,
			[]string{"", "evidence", "feature"},
			deferredRows(deferred))
		fmt.Fprintln(w)
	}

	if len(closed) > 0 {
		fmt.Fprintf(w, "Closed / not-applicable (%d)\n", len(closed))
		sortGaps(closed)
		writeTextTable(w,
			[]string{"", "evidence", "ours", "title"},
			closedRows(closed, lookupOurIssue))
		fmt.Fprintln(w)
	}
}

func gapRows(gs []model.Gap, lookup func(int) *model.Issue) [][]string {
	rows := make([][]string, 0, len(gs))
	for _, g := range gs {
		badge := statusBadge(g.Status)
		oursNum := 0
		if g.TargetIssue != nil {
			oursNum = *g.TargetIssue
		}
		oursRef := "—"
		title := ""
		if oursNum > 0 {
			oursRef = fmt.Sprintf("#%d", oursNum)
			if oi := lookup(oursNum); oi != nil {
				title = truncate(oi.Title, 55)
			}
		}
		rows = append(rows, []string{
			badge, g.SourceEvidence, oursRef, title, g.Priority,
		})
	}
	return rows
}

func deferredRows(gs []model.Gap) [][]string {
	rows := make([][]string, 0, len(gs))
	for _, g := range gs {
		rows = append(rows, []string{statusBadge(g.Status), g.SourceEvidence, g.Feature})
	}
	return rows
}

func closedRows(gs []model.Gap, lookup func(int) *model.Issue) [][]string {
	rows := make([][]string, 0, len(gs))
	for _, g := range gs {
		oursNum := 0
		if g.TargetIssue != nil {
			oursNum = *g.TargetIssue
		}
		oursRef := "—"
		title := ""
		if oursNum > 0 {
			oursRef = fmt.Sprintf("#%d", oursNum)
			if oi := lookup(oursNum); oi != nil {
				title = truncate(oi.Title, 55)
			}
		}
		rows = append(rows, []string{
			statusBadge(g.Status), g.SourceEvidence, oursRef, title,
		})
	}
	return rows
}

// --- status + priority helpers (ported from Python) ----------------------

func statusBadge(status string) string {
	switch status {
	case "open":
		return "[ ]"
	case "in-progress":
		return "[~]"
	case "closed":
		return "[x]"
	case "not-applicable":
		return "[-]"
	case "deferred":
		return "[>]"
	default:
		return "[ ]"
	}
}

func statusSortRank(status string) int {
	switch status {
	case "open":
		return 0
	case "in-progress":
		return 1
	case "deferred":
		return 2
	case "closed":
		return 3
	case "not-applicable":
		return 4
	default:
		return 9
	}
}

func priorityTier(p string) int {
	switch p {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	case "P3":
		return 3
	default:
		return 4
	}
}

// labelPriorityScore mirrors Python's _label_priority_score for the
// --by-priority sort on untracked issues. Tiers from highest priority
// (0) to lowest (9).
func labelPriorityScore(labelsCSV string) int {
	if labelsCSV == "" {
		return 9
	}
	labels := map[string]bool{}
	for _, l := range strings.Split(labelsCSV, ",") {
		labels[strings.ToLower(strings.TrimSpace(l))] = true
	}
	if anyIn(labels, "bug", "blocked", "breaking change", "security") {
		return 0
	}
	if anyIn(labels, "ready for work", "p0", "p1") {
		return 1
	}
	if anyIn(labels, "p2", "enhancement", "sep", "sep-requested", "in-review") {
		return 2
	}
	if anyIn(labels, "p3", "help wanted", "external") {
		return 3
	}
	if anyIn(labels, "needs investigation", "needs confirmation") {
		return 4
	}
	if anyIn(labels, "documentation", "question", "needs community feedback", "awaiting-reply") {
		return 5
	}
	return 6
}

func anyIn(set map[string]bool, keys ...string) bool {
	for _, k := range keys {
		if set[k] {
			return true
		}
	}
	return false
}
