package render

import (
	"fmt"
	"io"
	"strings"
)

// textRenderer writes plain ASCII with aligned columns and no ANSI codes.
// This is the mode pipes default to, and the one we recommend feeding to
// LLMs — escape sequences from pretty mode become noise in that context.
type textRenderer struct{}

// bucketOrder is the canonical iteration order for buckets. Kept as a
// package-level var so pretty/yaml renderers can reuse it.
var bucketOrder = []string{
	"real-gap", "already-covered", "defer-spec-flux",
	"defer-scope", "not-applicable", "done",
}

// RenderStatus writes the per-source summary + unclassified tail.
func (textRenderer) RenderStatus(w io.Writer, s StatusReport) error {
	fmt.Fprintf(w, "Triage status: %s\n", s.Project)
	fmt.Fprintln(w)

	// Build a fixed-width table. Columns: source, open, classified,
	// unclassified, then one column per bucket in canonical order.
	headers := []string{"source", "open", "classified", "unclassified"}
	headers = append(headers, bucketOrder...)

	rows := make([][]string, 0, len(s.Sources))
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
		rows = append(rows, row)
	}
	writeTable(w, headers, rows)

	// Unclassified tail
	if len(s.Unclassified) == 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "All open issues across all sources are classified.")
		return nil
	}
	fmt.Fprintln(w)
	for _, g := range s.Unclassified {
		fmt.Fprintf(w, "Unclassified — %s (%d)\n", g.Label, len(g.Issues))
		uheaders := []string{"ref", "title"}
		urows := make([][]string, 0, len(g.Issues))
		for _, it := range g.Issues {
			urows = append(urows, []string{it.Ref, truncate(it.Title, 100)})
		}
		writeTable(w, uheaders, urows)
		fmt.Fprintln(w)
	}
	return nil
}

// RenderList writes each bucket as its own section with a column-aligned
// table of triage rows.
func (textRenderer) RenderList(w io.Writer, l TriageList) error {
	if len(l.Groups) == 0 {
		fmt.Fprintln(w, "No classifications match the filter.")
		return nil
	}
	for i, g := range l.Groups {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s (%d)\n", g.Bucket, len(g.Rows))
		headers := []string{"ref", "state", "title", "target", "rationale"}
		rows := make([][]string, 0, len(g.Rows))
		for _, r := range g.Rows {
			rows = append(rows, []string{
				r.Ref,
				r.State,
				truncate(r.Title, 60),
				dash(r.Target),
				truncate(r.Rationale, 120),
			})
		}
		writeTable(w, headers, rows)
	}
	return nil
}

// RenderIssue writes the per-issue detail view — header, optional triage
// block, optional notes, optional linked gap. Matches the Python cmd_show
// layout closely for parity testing.
func (textRenderer) RenderIssue(w io.Writer, v IssueView) error {
	fmt.Fprintln(w, strings.Repeat("-", 80))
	fmt.Fprintf(w, "  %s#%d  [%s]\n", v.Repo, v.Number, v.State)
	fmt.Fprintf(w, "  %s\n", v.Title)
	if v.Labels != "" {
		fmt.Fprintf(w, "  Labels: %s\n", v.Labels)
	}
	if v.URL != "" {
		fmt.Fprintf(w, "  URL: %s\n", v.URL)
	}
	if v.Created != "" || v.Updated != "" {
		fmt.Fprintf(w, "  Created: %s  Updated: %s\n", v.Created, v.Updated)
	}

	if v.Triage != nil {
		t := v.Triage
		line := fmt.Sprintf("Triage: %s", t.Bucket)
		if t.Target != "" {
			line += "  →  " + t.Target
		}
		line += fmt.Sprintf("  (classified %s", shortDate(t.ClassifiedAt))
		if t.UpdatedAt != "" && t.UpdatedAt != t.ClassifiedAt {
			line += fmt.Sprintf(", updated %s", shortDate(t.UpdatedAt))
		}
		line += ")"
		fmt.Fprintf(w, "  + %s\n", line)
		if t.Test != "" {
			fmt.Fprintf(w, "  |  test: %s\n", t.Test)
		}
		if t.Rationale != "" {
			wrapRationale(w, t.Rationale, "  |  ", "  |          ", 70)
		}
		fmt.Fprintln(w, "  +")
	}

	if len(v.Notes) > 0 {
		fmt.Fprintf(w, "  Notes (%d):\n", len(v.Notes))
		for _, n := range v.Notes {
			at := ""
			if n.At != "" {
				at = fmt.Sprintf(" (%s)", n.At)
			}
			fmt.Fprintf(w, "    * %s%s\n", n.Text, at)
		}
	}

	if v.Gap != nil {
		g := v.Gap
		ours := "none"
		if g.TargetIssue > 0 {
			ours = fmt.Sprintf("#%d", g.TargetIssue)
		}
		fmt.Fprintf(w, "  + Linked gap: %s  ->  %s%s  [%s]\n",
			g.Feature, g.TargetRepo, ours, g.GapStatus)
		if g.TargetTitle != "" {
			fmt.Fprintf(w, "  |  %s  [%s]\n", g.TargetTitle, g.TargetState)
		}
		fmt.Fprintln(w, "  +")
	}

	fmt.Fprintln(w, strings.Repeat("-", 80))

	if v.Body != "" {
		fmt.Fprintln(w, v.Body)
	}
	return nil
}

// --- helpers --------------------------------------------------------------

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// shortDate takes "2026-04-11T18:47:31Z" or "2026-04-11 18:47:31" and
// returns just "2026-04-11". Tolerates missing time parts.
func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// wrapRationale prints a long rationale wrapped at width, with firstPrefix
// on the first line and restPrefix on continuations. Both prefixes should
// already include their trailing space.
func wrapRationale(w io.Writer, text, firstPrefix, restPrefix string, width int) {
	if len(text) <= width {
		fmt.Fprintf(w, "%srationale: %s\n", firstPrefix, text)
		return
	}
	fmt.Fprintf(w, "%srationale: %s\n", firstPrefix, text[:width])
	for i := width; i < len(text); i += width {
		end := i + width
		if end > len(text) {
			end = len(text)
		}
		fmt.Fprintf(w, "%s%s\n", restPrefix, text[i:end])
	}
}

// writeTable renders headers + rows with auto-sized columns. Plain ASCII,
// tab-delimited cells padded to column width with spaces, no borders.
// LLM-safe and diff-friendly.
func writeTable(w io.Writer, headers []string, rows [][]string) {
	// Compute column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i, cell := range r {
			if i >= len(widths) {
				break
			}
			if n := runeLen(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	// Header line
	for i, h := range headers {
		pad(w, h, widths[i])
		if i < len(headers)-1 {
			fmt.Fprint(w, "  ")
		}
	}
	fmt.Fprintln(w)

	// Separator
	for i, wd := range widths {
		fmt.Fprint(w, strings.Repeat("-", wd))
		if i < len(widths)-1 {
			fmt.Fprint(w, "  ")
		}
	}
	fmt.Fprintln(w)

	// Rows
	for _, r := range rows {
		for i, cell := range r {
			if i >= len(widths) {
				break
			}
			pad(w, cell, widths[i])
			if i < len(r)-1 && i < len(widths)-1 {
				fmt.Fprint(w, "  ")
			}
		}
		fmt.Fprintln(w)
	}
}

func pad(w io.Writer, s string, width int) {
	n := runeLen(s)
	fmt.Fprint(w, s)
	if n < width {
		fmt.Fprint(w, strings.Repeat(" ", width-n))
	}
}

// runeLen counts runes, not bytes, so multi-byte characters don't skew
// column widths. Not grapheme-cluster aware but good enough for our data.
func runeLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
