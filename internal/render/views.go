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

// --- legacy verb views ---------------------------------------------------
//
// These views back the four legacy verbs (notes/crossref/reconcile/report)
// after their M7 rewiring. They're shaped flat on purpose so the yaml
// renderer is a one-liner per method — no nested render paths needed.

// NotesListView backs `gapt notes [filter]`.
type NotesListView struct {
	Filter string          `yaml:"filter,omitempty"`
	Issues []NotesListItem `yaml:"issues"`
}

type NotesListItem struct {
	Ref    string   `yaml:"ref"`    // "#42" for target, "label:42" for sources
	State  string   `yaml:"state"`  // "OPEN" / "CLOSED"
	Title  string   `yaml:"title"`
	Latest string   `yaml:"latest"` // most recent note text (truncated in text mode)
	Count  int      `yaml:"count"`  // total notes on the issue
	Notes  []string `yaml:"notes,omitempty"` // all note text, yaml mode only
}

// CrossrefReport backs `gapt crossref`. One section per source.
type CrossrefReport struct {
	Project  string            `yaml:"project"`
	Target   string            `yaml:"target"`
	Sources  []CrossrefSection `yaml:"sources"`
}

type CrossrefSection struct {
	Label     string           `yaml:"label"`
	Repo      string           `yaml:"repo"`
	OpenCount int              `yaml:"open_count"`
	Matched   []CrossrefMatch  `yaml:"matched"`
	Unmatched []CrossrefMiss   `yaml:"unmatched"`
}

type CrossrefMatch struct {
	SrcRef   string `yaml:"src_ref"`   // "label#N"
	SrcTitle string `yaml:"src_title"`
	TgtRef   string `yaml:"tgt_ref"`   // "#N"
	TgtTitle string `yaml:"tgt_title"`
	Score    int    `yaml:"score"`
}

type CrossrefMiss struct {
	Ref   string `yaml:"ref"`   // "label#N"
	Title string `yaml:"title"`
}

// ReconcileReport backs `gapt reconcile`.
type ReconcileReport struct {
	TargetRepo     string          `yaml:"target_repo"`
	SyncedCount    int             `yaml:"synced_count"`
	Closed         []ReconcileRow  `yaml:"closed"`
	UnchangedCount int             `yaml:"unchanged_count"`
	Missing        []ReconcileMiss `yaml:"missing"`
}

type ReconcileRow struct {
	Feature  string `yaml:"feature"`
	Number   int    `yaml:"number"`
	OldState string `yaml:"old_state"`
	Title    string `yaml:"title,omitempty"`
}

type ReconcileMiss struct {
	Feature string `yaml:"feature"`
	Number  int    `yaml:"number"`
}

// ReportView backs `gapt report [--by-priority]`. Nested per-source
// sections + gaps-summary groups. Each source section has tracked +
// untracked tables. The summary has three groups keyed by bucket
// semantics (open / deferred / closed-or-na).
type ReportView struct {
	Project    string          `yaml:"project"`
	TargetRepo string          `yaml:"target_repo"`
	Sources    []ReportSource  `yaml:"sources"`
	Summary    *ReportSummary  `yaml:"summary,omitempty"`
}

type ReportSource struct {
	Label     string            `yaml:"label"`
	Repo      string            `yaml:"repo"`
	OpenCount int               `yaml:"open_count"`
	Tracked   []ReportTracked   `yaml:"tracked"`
	Untracked []ReportUntracked `yaml:"untracked"`
}

type ReportTracked struct {
	Badge     string `yaml:"badge"`      // "[x]" / "[ ]" / "[>]" / ...
	SrcRef    string `yaml:"src_ref"`    // "label#N"
	SrcTitle  string `yaml:"src_title"`
	OursRef   string `yaml:"ours_ref"`   // "#N" or ""
	OursTitle string `yaml:"ours_title"`
	Status    string `yaml:"status"`     // raw status for yaml consumers
}

type ReportUntracked struct {
	Ref    string `yaml:"ref"`    // "label#N"
	Title  string `yaml:"title"`
	Labels string `yaml:"labels,omitempty"`
}

type ReportSummary struct {
	Open     []ReportGapRow `yaml:"open,omitempty"`
	Deferred []ReportGapRow `yaml:"deferred,omitempty"`
	Closed   []ReportGapRow `yaml:"closed,omitempty"`
}

type ReportGapRow struct {
	Badge     string `yaml:"badge"`
	Feature   string `yaml:"feature,omitempty"` // populated for deferred; "evidence" field used elsewhere
	Evidence  string `yaml:"evidence,omitempty"`
	OursRef   string `yaml:"ours_ref,omitempty"`
	OursTitle string `yaml:"ours_title,omitempty"`
	Priority  string `yaml:"priority,omitempty"`
	Status    string `yaml:"status"`
}

// VerifyReport backs `gapt triage verify`. Rows are individual
// triage entries that had a pinned test; the renderer groups them
// by outcome (aligned / drift / skipped / missing-test-file).
type VerifyReport struct {
	TargetPath string       `yaml:"target_path"`
	Tested     int          `yaml:"tested"`
	Skipped    int          `yaml:"skipped"` // had no test: field
	Rows       []VerifyRow  `yaml:"rows"`
}

type VerifyRow struct {
	Ref        string `yaml:"ref"`       // "<label>:<number>"
	Bucket     string `yaml:"bucket"`    // what triage says
	Test       string `yaml:"test"`      // path::TestName
	Outcome    string `yaml:"outcome"`   // "pass" / "fail" / "skip" / "missing" / "error"
	Drift      string `yaml:"drift"`     // "aligned" / "should-close" / "regression" / "reopened" / "unknown"
	Message    string `yaml:"message,omitempty"`
}
