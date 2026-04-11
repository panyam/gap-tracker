package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// prettyRenderer uses lipgloss for bordered tables, colored headers, and
// selective cell styling. Intended for interactive terminal use. Never
// used for pipes or LLM feeds — those go through textRenderer.
type prettyRenderer struct{}

// Style constants — kept local so renderer choices stay in this file.
var (
	headerStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#87CEEB")).
		Padding(0, 1)

	cellStyle = lipgloss.NewStyle().Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		MarginBottom(1)

	closedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF87"))
	openStyle   = lipgloss.NewStyle().Bold(true)

	rationaleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB"))
	unclassifiedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))

	borderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#5A5A5A"))
)

// RenderStatus mirrors textRenderer's layout but wraps the summary and
// unclassified tails in lipgloss tables.
func (p prettyRenderer) RenderStatus(w io.Writer, s StatusReport) error {
	fmt.Fprintln(w, titleStyle.Render(fmt.Sprintf("Triage status: %s", s.Project)))

	headers := []string{"source", "open", "classified", "unclassified"}
	headers = append(headers, bucketOrder...)

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(borderStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			// Unclassified column (index 3) highlighted when > 0
			if col == 3 {
				return cellStyle.Foreground(lipgloss.Color("#FFD700"))
			}
			return cellStyle
		}).
		Headers(headers...)

	for _, src := range s.Sources {
		row := []string{
			src.Label,
			itoa(src.TotalOpen),
			itoa(src.Classified),
			itoa(src.Unclassified),
		}
		for _, b := range bucketOrder {
			row = append(row, itoa(src.BucketCounts[b]))
		}
		t.Row(row...)
	}
	fmt.Fprintln(w, t.Render())
	fmt.Fprintln(w)

	if len(s.Unclassified) == 0 {
		fmt.Fprintln(w, lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF87")).
			Render("All open issues across all sources are classified."))
		return nil
	}

	for _, g := range s.Unclassified {
		ut := table.New().
			Border(lipgloss.NormalBorder()).
			BorderStyle(borderStyle).
			StyleFunc(func(row, col int) lipgloss.Style {
				if row == table.HeaderRow {
					return headerStyle
				}
				return cellStyle
			}).
			Headers("ref", "title")
		for _, it := range g.Issues {
			ut.Row(it.Ref, truncate(it.Title, 80))
		}
		fmt.Fprintln(w, titleStyle.Render(
			fmt.Sprintf("Unclassified — %s (%d)", g.Label, len(g.Issues))))
		fmt.Fprintln(w, ut.Render())
		fmt.Fprintln(w)
	}
	return nil
}

// RenderList writes one bordered table per bucket.
func (p prettyRenderer) RenderList(w io.Writer, l TriageList) error {
	if len(l.Groups) == 0 {
		fmt.Fprintln(w, "No classifications match the filter.")
		return nil
	}
	for _, g := range l.Groups {
		t := table.New().
			Border(lipgloss.NormalBorder()).
			BorderStyle(borderStyle).
			StyleFunc(func(row, col int) lipgloss.Style {
				if row == table.HeaderRow {
					return headerStyle
				}
				// col 4 is rationale — apply the dim-blue style
				if col == 4 {
					return cellStyle.Inherit(rationaleStyle)
				}
				// col 1 is state — color CLOSED green
				if col == 1 && row >= 0 && row < len(g.Rows) {
					if g.Rows[row].State == "CLOSED" {
						return cellStyle.Inherit(closedStyle)
					}
					return cellStyle.Inherit(openStyle)
				}
				return cellStyle
			}).
			Headers("ref", "state", "title", "target", "rationale")
		for _, r := range g.Rows {
			t.Row(
				r.Ref,
				r.State,
				truncate(r.Title, 60),
				dash(r.Target),
				truncate(r.Rationale, 120),
			)
		}
		fmt.Fprintln(w, titleStyle.Render(
			fmt.Sprintf("%s (%d)", g.Bucket, len(g.Rows))))
		fmt.Fprintln(w, t.Render())
		fmt.Fprintln(w)
	}
	return nil
}

// RenderIssue uses titled sections rather than one monolithic table,
// because an issue view mixes tabular triage data with free-form notes.
func (p prettyRenderer) RenderIssue(w io.Writer, v IssueView) error {
	hr := borderStyle.Render(strings.Repeat("─", 80))
	fmt.Fprintln(w, hr)
	fmt.Fprintln(w, titleStyle.Render(fmt.Sprintf("%s#%d  [%s]",
		v.Repo, v.Number, v.State)))
	fmt.Fprintln(w, "  "+v.Title)
	if v.Labels != "" {
		fmt.Fprintln(w, "  Labels: "+v.Labels)
	}
	if v.URL != "" {
		fmt.Fprintln(w, "  URL: "+v.URL)
	}
	if v.Created != "" || v.Updated != "" {
		fmt.Fprintf(w, "  Created: %s  Updated: %s\n", v.Created, v.Updated)
	}

	if v.Triage != nil {
		t := v.Triage
		header := fmt.Sprintf("Triage: %s", t.Bucket)
		if t.Target != "" {
			header += "  →  " + t.Target
		}
		header += fmt.Sprintf("  (classified %s", shortDate(t.ClassifiedAt))
		if t.UpdatedAt != "" && t.UpdatedAt != t.ClassifiedAt {
			header += fmt.Sprintf(", updated %s", shortDate(t.UpdatedAt))
		}
		header += ")"
		fmt.Fprintln(w, titleStyle.Copy().MarginTop(1).Render(header))
		if t.Test != "" {
			fmt.Fprintln(w, "  test: "+t.Test)
		}
		if t.Rationale != "" {
			fmt.Fprintln(w, rationaleStyle.Render("  "+t.Rationale))
		}
	}

	if len(v.Notes) > 0 {
		fmt.Fprintln(w, titleStyle.Copy().MarginTop(1).Render(
			fmt.Sprintf("Notes (%d)", len(v.Notes))))
		for _, n := range v.Notes {
			at := ""
			if n.At != "" {
				at = " " + borderStyle.Render("("+n.At+")")
			}
			fmt.Fprintf(w, "  • %s%s\n", n.Text, at)
		}
	}

	if v.Gap != nil {
		g := v.Gap
		ours := "none"
		if g.TargetIssue > 0 {
			ours = fmt.Sprintf("#%d", g.TargetIssue)
		}
		fmt.Fprintln(w, titleStyle.Copy().MarginTop(1).Render(
			fmt.Sprintf("Linked gap: %s  →  %s%s  [%s]",
				g.Feature, g.TargetRepo, ours, g.GapStatus)))
		if g.TargetTitle != "" {
			fmt.Fprintf(w, "  %s  [%s]\n", g.TargetTitle, g.TargetState)
		}
	}

	fmt.Fprintln(w, hr)
	if v.Body != "" {
		fmt.Fprintln(w, v.Body)
	}
	return nil
}
