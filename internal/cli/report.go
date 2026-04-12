package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/model"
	"github.com/panyam/gap-tracker/internal/render"
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
		Use:   "report <project>",
		Short: "[legacy gaps table] Per-source tracked/untracked + all-gaps summary (prefer: triage status / triage list)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			view, err := buildReportView(a, byPriority)
			if err != nil {
				return err
			}
			return a.Renderer.RenderReport(os.Stdout, view)
		},
	}
	cmd.Flags().BoolVar(&byPriority, "by-priority", false,
		"sort untracked issues by label priority (bugs/security first)")
	return cmd
}

// buildReportView pulls every gap + source issue, groups them, and
// packs the result into a render.ReportView. Formatting decisions
// live in the renderer; this function just shapes data.
func buildReportView(a *app.App, byPriority bool) (render.ReportView, error) {
	gaps, err := a.Store.AllGaps()
	if err != nil {
		return render.ReportView{}, err
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

	view := render.ReportView{
		Project:    a.Cfg.Project,
		TargetRepo: a.Cfg.TargetRepo,
	}

	for _, src := range a.Cfg.Sources {
		srcIssues, err := a.Store.OpenIssuesByRepoFull(src.Repo)
		if err != nil {
			return render.ReportView{}, err
		}

		section := render.ReportSource{
			Label:     src.Label,
			Repo:      src.Repo,
			OpenCount: len(srcIssues),
		}

		type trackedPair struct {
			SrcIssue model.Issue
			Gap      *model.Gap
		}
		var trackedPairs []trackedPair
		var untracked []model.Issue
		for _, si := range srcIssues {
			ref := src.Label + ":" + itoa(si.Number)
			if gap := evidenceToGap[ref]; gap != nil {
				trackedPairs = append(trackedPairs, trackedPair{SrcIssue: si, Gap: gap})
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

		for _, r := range trackedPairs {
			oursNum := 0
			if r.Gap.TargetIssue != nil {
				oursNum = *r.Gap.TargetIssue
			}
			oursRef := ""
			oursTitle := ""
			if oursNum > 0 {
				oursRef = fmt.Sprintf("#%d", oursNum)
				if oi := lookupOurIssue(oursNum); oi != nil {
					oursTitle = oi.Title
				}
			}
			section.Tracked = append(section.Tracked, render.ReportTracked{
				Badge:     statusBadge(r.Gap.Status),
				SrcRef:    fmt.Sprintf("%s#%d", src.Label, r.SrcIssue.Number),
				SrcTitle:  r.SrcIssue.Title,
				OursRef:   oursRef,
				OursTitle: oursTitle,
				Status:    r.Gap.Status,
			})
		}
		for _, si := range untracked {
			section.Untracked = append(section.Untracked, render.ReportUntracked{
				Ref:    fmt.Sprintf("%s#%d", src.Label, si.Number),
				Title:  si.Title,
				Labels: si.Labels,
			})
		}

		view.Sources = append(view.Sources, section)
	}

	// Gaps summary.
	if len(gaps) > 0 {
		view.Summary = buildGapsSummary(gaps, lookupOurIssue)
	}
	return view, nil
}

// buildGapsSummary groups gaps by status bucket and sorts each group.
func buildGapsSummary(gaps []model.Gap, lookupOurIssue func(int) *model.Issue) *render.ReportSummary {
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
	sortGaps(open)
	sortGaps(deferred)
	sortGaps(closed)

	toRow := func(g model.Gap) render.ReportGapRow {
		oursNum := 0
		if g.TargetIssue != nil {
			oursNum = *g.TargetIssue
		}
		oursRef := ""
		oursTitle := ""
		if oursNum > 0 {
			oursRef = fmt.Sprintf("#%d", oursNum)
			if oi := lookupOurIssue(oursNum); oi != nil {
				oursTitle = oi.Title
			}
		}
		return render.ReportGapRow{
			Badge:     statusBadge(g.Status),
			Feature:   g.Feature,
			Evidence:  g.SourceEvidence,
			OursRef:   oursRef,
			OursTitle: oursTitle,
			Priority:  g.Priority,
			Status:    g.Status,
		}
	}

	sum := &render.ReportSummary{}
	for _, g := range open {
		sum.Open = append(sum.Open, toRow(g))
	}
	for _, g := range deferred {
		sum.Deferred = append(sum.Deferred, toRow(g))
	}
	for _, g := range closed {
		sum.Closed = append(sum.Closed, toRow(g))
	}
	return sum
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
