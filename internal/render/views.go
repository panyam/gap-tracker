package render

// View structs carry the minimum information needed to render a command's
// output. They're intentionally flat and free of GORM or SQL concerns, so
// commands can assemble them by mapping store rows into view fields without
// leaking DB types to the render layer.
//
// Why duplicate fields from the model package: renderer implementations
// should not import model, because doing so would couple output formatting
// to the GORM schema. When the schema grows a column we don't want every
// renderer to gain a new case by accident.

// StatusReport is the aggregate view behind `gapt triage status`.
//
// Per source: total open, classified, unclassified counts, plus the
// per-bucket breakdown of classified rows. Buckets is ordered with the
// canonical bucket order so renderers can iterate deterministically.
type StatusReport struct {
	Project string
	Sources []SourceStatus

	// Unclassified is the tail section — open issues with no triage row,
	// grouped by source. Only sources with >0 unclassified appear.
	Unclassified []UnclassifiedGroup
}

// SourceStatus is the row for one source in the status summary table.
type SourceStatus struct {
	Label           string
	TotalOpen       int
	Classified      int
	Unclassified    int
	BucketCounts    map[string]int // bucket name → count
}

// UnclassifiedGroup lists issues that need attention under a source.
type UnclassifiedGroup struct {
	Label  string
	Issues []UnclassifiedIssue
}

// UnclassifiedIssue is one open issue not yet classified.
type UnclassifiedIssue struct {
	Ref   string // "<label>:<number>"
	Title string
}

// TriageList is the grouped view behind `gapt triage list`. Each group is
// a bucket; rows inside a group are the matching triage entries.
type TriageList struct {
	Groups []TriageGroup
}

// TriageGroup is one bucket section in the list output.
type TriageGroup struct {
	Bucket string
	Rows   []TriageRow
}

// TriageRow is one classified issue in the list output.
type TriageRow struct {
	Ref       string // "<source>:<number>"
	State     string // upstream issue state: "OPEN" / "CLOSED"
	Title     string
	Target    string // "mcpkit#190" or empty
	Test      string // "path::TestName" or empty
	Rationale string
}

// IssueView is the per-issue detail view behind `gapt show`. It mirrors
// the Python cmd_show output layout: issue header, optional triage block,
// optional ad-hoc notes, optional linked legacy gap.
type IssueView struct {
	Repo     string
	Number   int
	Title    string
	State    string
	Labels   string
	URL      string
	Created  string
	Updated  string
	Body     string // optional, may be large — renderers truncate

	Triage *TriageBlock    // nil if no classification exists
	Notes  []NoteEntry     // ad-hoc notes (excluding legacy [triage:...] entries)
	Gap    *LinkedGapBlock // nil if no legacy gap row points here
}

// TriageBlock renders inside IssueView when a triage row exists.
type TriageBlock struct {
	Bucket       string
	Target       string
	Rationale    string
	Test         string
	ClassifiedAt string // YYYY-MM-DD
	UpdatedAt    string // YYYY-MM-DD, omitted in render if equal to ClassifiedAt
}

// NoteEntry is one ad-hoc note on an issue.
type NoteEntry struct {
	Text string
	At   string // YYYY-MM-DD or empty
}

// LinkedGapBlock renders inside IssueView when a legacy gap row references
// this issue in its source_evidence column.
type LinkedGapBlock struct {
	Feature        string
	TargetRepo     string
	TargetIssue    int    // 0 if no target
	TargetState    string // "OPEN" / "CLOSED" / "" if unknown
	TargetTitle    string
	GapStatus      string // open / deferred / not-applicable / closed / in-progress
}
