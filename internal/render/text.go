package render

import (
	"fmt"
	"io"
	"strings"
)

// textRenderer writes plain ASCII with aligned columns and no ANSI codes.
// This is the mode pipes default to, and the one we recommend feeding to
// LLMs — escape sequences from pretty mode become noise in that context.
//
// When links is non-nil (e.g. when pretty delegates legacy verbs), refs
// are wrapped in OSC 8 terminal hyperlinks.
type textRenderer struct {
	links LinkResolver
}

// linkRef wraps ref text in an OSC 8 clickable hyperlink if a resolver is set.
func (t textRenderer) linkRef(ref string) string {
	if t.links == nil {
		return ref
	}
	return osc8(t.links.RefURL(ref), ref)
}

// bucketOrder is the canonical iteration order for buckets. Kept as a
// package-level var so pretty/yaml renderers can reuse it.
var bucketOrder = []string{
	"real-gap", "already-covered", "defer-spec-flux",
	"defer-scope", "not-applicable", "done",
}

// RenderStatus writes the per-source summary + unclassified tail.
func (tr textRenderer) RenderStatus(w io.Writer, s StatusReport) error {
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
			urows = append(urows, []string{tr.linkRef(it.Ref), truncate(it.Title, 100)})
		}
		writeTable(w, uheaders, urows)
		fmt.Fprintln(w)
	}
	return nil
}

// RenderList writes each bucket as its own section with a column-aligned
// table of triage rows.
func (tr textRenderer) RenderList(w io.Writer, l TriageList) error {
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
				tr.linkRef(r.Ref),
				r.State,
				truncate(r.Title, 60),
				tr.linkRef(dash(r.Target)),
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
func (tr textRenderer) RenderIssue(w io.Writer, v IssueView) error {
	fmt.Fprintln(w, strings.Repeat("-", 80))
	issueRef := fmt.Sprintf("%s#%d", v.Repo, v.Number)
	fmt.Fprintf(w, "  %s  [%s]\n", tr.linkRef(issueRef), v.State)
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
			line += "  →  " + tr.linkRef(t.Target)
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
			oursRef := fmt.Sprintf("%s#%d", g.TargetRepo, g.TargetIssue)
			ours = tr.linkRef(oursRef)
		}
		fmt.Fprintf(w, "  + Linked gap: %s  ->  %s  [%s]\n",
			g.Feature, ours, g.GapStatus)
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

// runeLen counts visible runes, not bytes, so multi-byte characters don't
// skew column widths. Strips OSC 8 hyperlink escape sequences so invisible
// control characters don't inflate the count.
func runeLen(s string) int {
	s = stripOSC8(s)
	n := 0
	for range s {
		n++
	}
	return n
}

// stripOSC8 removes OSC 8 terminal hyperlink sequences, leaving only the
// visible link text. Format: \x1b]8;;URL\x1b\TEXT\x1b]8;;\x1b\
func stripOSC8(s string) string {
	if !strings.Contains(s, "\x1b]8;") {
		return s
	}
	var b strings.Builder
	for len(s) > 0 {
		i := strings.Index(s, "\x1b]8;")
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		s = s[i:]
		// Skip past the ST (string terminator): \x1b\
		j := strings.Index(s, "\x1b\\")
		if j < 0 {
			b.WriteString(s)
			break
		}
		s = s[j+2:] // skip \x1b\
	}
	return b.String()
}

// --- legacy verb renderers (text) ----------------------------------------

// RenderNotesList writes the plain-text view behind `gapt notes`.
func (textRenderer) RenderNotesList(w io.Writer, v NotesListView) error {
	if len(v.Issues) == 0 {
		if v.Filter != "" {
			fmt.Fprintf(w, "No issues with notes matching %q.\n", v.Filter)
		} else {
			fmt.Fprintln(w, "No issues with notes.")
		}
		return nil
	}
	if v.Filter != "" {
		fmt.Fprintf(w, "Issues with notes (filter: %s)\n\n", v.Filter)
	} else {
		fmt.Fprintln(w, "Issues with notes")
		fmt.Fprintln(w)
	}
	rows := make([][]string, 0, len(v.Issues))
	for _, it := range v.Issues {
		latest := truncate(it.Latest, 60)
		if it.Count > 1 {
			latest = fmt.Sprintf("(%d) %s", it.Count, latest)
		}
		rows = append(rows, []string{
			it.Ref,
			it.State,
			truncate(it.Title, 60),
			latest,
		})
	}
	writeTable(w, []string{"ref", "state", "title", "notes"}, rows)
	return nil
}

// RenderCrossref writes the plain-text view behind `gapt crossref`.
func (tr textRenderer) RenderCrossref(w io.Writer, r CrossrefReport) error {
	fmt.Fprintf(w, "=== Cross-reference: %s ===\n\n", r.Project)
	for _, sec := range r.Sources {
		fmt.Fprintf(w, "--- %s (%d open) → %s ---\n", sec.Label, sec.OpenCount, r.Target)
		for _, m := range sec.Matched {
			fmt.Fprintf(w, "  %s → %s (score=%d)\n", tr.linkRef(m.SrcRef), tr.linkRef(m.TgtRef), m.Score)
			fmt.Fprintf(w, "    src: %s\n", truncate(m.SrcTitle, 70))
			fmt.Fprintf(w, "    tgt: %s\n", truncate(m.TgtTitle, 70))
		}
		if len(sec.Unmatched) > 0 {
			fmt.Fprintf(w, "\n  Unmatched (%d):\n", len(sec.Unmatched))
			shown := sec.Unmatched
			if len(shown) > 20 {
				shown = shown[:20]
			}
			for _, u := range shown {
				fmt.Fprintf(w, "    %s: %s\n", tr.linkRef(u.Ref), truncate(u.Title, 70))
			}
			if len(sec.Unmatched) > 20 {
				fmt.Fprintf(w, "    ... and %d more\n", len(sec.Unmatched)-20)
			}
		}
		fmt.Fprintln(w)
	}
	return nil
}

// RenderReconcile writes the plain-text view behind `gapt reconcile`.
func (textRenderer) RenderReconcile(w io.Writer, r ReconcileReport) error {
	fmt.Fprintf(w, "=== Synced target repo: %s (%d issues) ===\n\n", r.TargetRepo, r.SyncedCount)
	if len(r.Closed) > 0 {
		fmt.Fprintf(w, "Closed (%d)\n", len(r.Closed))
		rows := make([][]string, 0, len(r.Closed))
		for _, c := range r.Closed {
			rows = append(rows, []string{
				c.Feature, fmt.Sprintf("#%d", c.Number), c.OldState, truncate(c.Title, 60),
			})
		}
		writeTable(w, []string{"feature", "issue", "was", "issue title"}, rows)
		fmt.Fprintln(w)
	}
	if r.UnchangedCount > 0 {
		fmt.Fprintf(w, "Unchanged: %d gaps still open\n", r.UnchangedCount)
	}
	if len(r.Missing) > 0 {
		fmt.Fprintf(w, "Warning: %d gaps reference issues not found in DB:\n", len(r.Missing))
		for _, m := range r.Missing {
			fmt.Fprintf(w, "  %s → #%d\n", m.Feature, m.Number)
		}
	}
	fmt.Fprintf(w, "\nReconciled: %d closed, %d unchanged, %d missing\n",
		len(r.Closed), r.UnchangedCount, len(r.Missing))
	return nil
}

// RenderSearch writes the FTS5 search results as a table.
func (tr textRenderer) RenderSearch(w io.Writer, r SearchReport) error {
	fmt.Fprintf(w, "Search: %q in %s (%d hits)\n\n", r.Query, r.Project, len(r.Hits))
	if len(r.Hits) == 0 {
		fmt.Fprintln(w, "No results.")
		return nil
	}
	rows := make([][]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		rows = append(rows, []string{
			h.Ref,
			h.State,
			truncate(h.Title, 60),
			dash(h.Bucket),
			truncate(h.Snippet, 80),
		})
	}
	writeTable(w, []string{"ref", "state", "title", "bucket", "snippet"}, rows)
	return nil
}

// RenderQuery writes arbitrary SQL results as a plain table.
func (textRenderer) RenderQuery(w io.Writer, r QueryReport) error {
	if len(r.Rows) == 0 {
		fmt.Fprintln(w, "No rows returned.")
		return nil
	}
	writeTable(w, r.Columns, r.Rows)
	fmt.Fprintf(w, "\n(%d rows)\n", len(r.Rows))
	return nil
}

// RenderProposals writes proposals grouped by stage.
func (tr textRenderer) RenderProposals(w io.Writer, l ProposalList) error {
	header := fmt.Sprintf("Proposals: %s", l.Project)
	if l.Kind != "" {
		header += fmt.Sprintf(" [%s]", l.Kind)
	}
	if l.Release != "" {
		header += fmt.Sprintf(" release=%s", l.Release)
	}
	fmt.Fprintf(w, "%s (%d total)\n\n", header, l.Total)

	if l.Total == 0 {
		fmt.Fprintln(w, "No proposals found.")
		return nil
	}

	for _, g := range l.Groups {
		fmt.Fprintf(w, "── %s (%d) ──\n", g.Stage, g.Count)
		rows := make([][]string, 0, len(g.Rows))
		for _, r := range g.Rows {
			rows = append(rows, []string{
				r.Identifier,
				dash(r.Priority),
				truncate(r.Title, 55),
				dash(r.Theme),
				dash(r.Sponsor),
				dash(r.Bucket),
			})
		}
		writeTable(w, []string{"id", "pri", "title", "theme", "sponsor", "bucket"}, rows)
		fmt.Fprintln(w)
	}
	return nil
}

// RenderVerify writes the drift-check view behind `gapt triage verify`.
// Rows are grouped by drift status so the interesting ones (anything
// that isn't "aligned") surface at the top.
func (textRenderer) RenderVerify(w io.Writer, r VerifyReport) error {
	fmt.Fprintf(w, "=== Drift check (target_path: %s) ===\n\n", r.TargetPath)

	// Group by drift class.
	groups := map[string][]VerifyRow{}
	for _, row := range r.Rows {
		groups[row.Drift] = append(groups[row.Drift], row)
	}
	order := []string{
		"should-close", "regression", "reopened", "stale-done", "unknown", "aligned",
	}
	titles := map[string]string{
		"should-close": "Should close (real-gap tests now pass)",
		"regression":   "Regression (already-covered tests now fail)",
		"reopened":     "Re-opened (done tests now fail)",
		"stale-done":   "Stale done (marked done, but repro still PENDING-skipped)",
		"unknown":      "Could not run (missing file / error)",
		"aligned":      "Aligned (no drift)",
	}

	shownAny := false
	for _, g := range order {
		rows := groups[g]
		if len(rows) == 0 {
			continue
		}
		shownAny = true
		fmt.Fprintf(w, "%s (%d)\n", titles[g], len(rows))
		tableRows := make([][]string, 0, len(rows))
		for _, row := range rows {
			tableRows = append(tableRows, []string{
				row.Ref,
				row.Bucket,
				row.Outcome,
				truncate(row.Test, 55),
				truncate(row.Message, 50),
			})
		}
		writeTable(w, []string{"ref", "bucket", "outcome", "test", "note"}, tableRows)
		fmt.Fprintln(w)
	}
	if !shownAny {
		fmt.Fprintln(w, "No classified rows have a pinned test yet.")
	}

	fmt.Fprintf(w, "Checked: %d tested, %d skipped (no test: field)\n",
		r.Tested, r.Skipped)
	return nil
}
