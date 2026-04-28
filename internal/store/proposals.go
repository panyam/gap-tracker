package store

import "fmt"

// ValidProposalStages is the authoritative set of allowed proposal stages.
var ValidProposalStages = map[string]bool{
	"proposal":              true,
	"draft":                 true,
	"in-review":             true,
	"accepted":              true,
	"accepted-with-changes": true,
	"final":                 true,
	"rejected":              true,
}

// UpsertProposalInput carries the fields for inserting/updating a proposal.
type UpsertProposalInput struct {
	Repo       string
	Number     int
	Kind       string // "SEP", "PEP", "JEP"
	Identifier string // "SEP-2356"
	Priority   string // "P0", "P1", ""
	Stage      string // must be in ValidProposalStages
	Theme      string
	Release    string
	Sponsor    string
}

// UpsertProposal inserts or updates a proposal row. The backing issue
// must exist in the issues table — sync the repo first (with include_prs
// if the proposal lives in a PR).
func (s *Store) UpsertProposal(in UpsertProposalInput) error {
	if in.Stage != "" && !ValidProposalStages[in.Stage] {
		return fmt.Errorf("invalid proposal stage %q", in.Stage)
	}

	// Verify the backing issue exists.
	var count int64
	s.db.Table("issues").Where("repo = ? AND number = ?", in.Repo, in.Number).Count(&count)
	if count == 0 {
		return fmt.Errorf("no issue (%s, #%d) — sync the repo first (set include_prs in config if this is a PR)", in.Repo, in.Number)
	}

	const stmt = `
INSERT INTO proposals (repo, number, kind, identifier, priority, stage, theme, release, sponsor, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(repo, number) DO UPDATE SET
    kind       = excluded.kind,
    identifier = excluded.identifier,
    priority   = excluded.priority,
    stage      = excluded.stage,
    theme      = excluded.theme,
    release    = excluded.release,
    sponsor    = excluded.sponsor,
    updated_at = excluded.updated_at
`
	return s.db.Exec(stmt,
		in.Repo, in.Number, in.Kind, in.Identifier,
		in.Priority, in.Stage, in.Theme, in.Release, in.Sponsor,
	).Error
}

// UpsertProposals bulk-inserts proposals. Stops on first error.
func (s *Store) UpsertProposals(inputs []UpsertProposalInput) (int, error) {
	count := 0
	for _, in := range inputs {
		if err := s.UpsertProposal(in); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// ProposalRow is the joined result of a proposal + its backing issue.
type ProposalRow struct {
	Repo       string
	Number     int
	Kind       string
	Identifier string
	Priority   string
	Stage      string
	Theme      string
	Release    string
	Sponsor    string
	Title      string // from issues
	State      string // from issues (OPEN/CLOSED)
	URL        string // from issues
	Bucket     string // from triage, empty if unclassified
}

// ProposalFilter restricts proposal list queries.
type ProposalFilter struct {
	Kind     string // empty = all kinds
	Stage    string // empty = all stages
	Release  string // empty = all releases
	Priority string // empty = all priorities
	Theme    string // empty = all themes
	Repo     string // empty = all repos
}

// ListProposals returns proposals matching the filter, joined with issue
// metadata and optional triage classification.
func (s *Store) ListProposals(f ProposalFilter) ([]ProposalRow, error) {
	var rows []ProposalRow

	q := s.db.Table("proposals p").
		Select(`p.repo, p.number, p.kind, p.identifier, p.priority, p.stage,
		        p.theme, p.release, p.sponsor,
		        i.title, i.state, i.url,
		        COALESCE(t.bucket, '') AS bucket`).
		Joins("JOIN issues i ON i.repo = p.repo AND i.number = p.number").
		Joins("LEFT JOIN triage t ON t.repo = p.repo AND t.number = p.number").
		Order("p.priority, p.stage, p.number")

	if f.Kind != "" {
		q = q.Where("p.kind = ?", f.Kind)
	}
	if f.Stage != "" {
		q = q.Where("p.stage = ?", f.Stage)
	}
	if f.Release != "" {
		q = q.Where("p.release = ?", f.Release)
	}
	if f.Priority != "" {
		q = q.Where("p.priority = ?", f.Priority)
	}
	if f.Theme != "" {
		q = q.Where("p.theme LIKE ?", "%"+f.Theme+"%")
	}
	if f.Repo != "" {
		q = q.Where("p.repo = ?", f.Repo)
	}

	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetProposal returns a single proposal by repo+number, or nil if not found.
func (s *Store) GetProposal(repo string, number int) (*ProposalRow, error) {
	var row ProposalRow
	err := s.db.Table("proposals p").
		Select(`p.repo, p.number, p.kind, p.identifier, p.priority, p.stage,
		        p.theme, p.release, p.sponsor,
		        i.title, i.state, i.url,
		        COALESCE(t.bucket, '') AS bucket`).
		Joins("JOIN issues i ON i.repo = p.repo AND i.number = p.number").
		Joins("LEFT JOIN triage t ON t.repo = p.repo AND t.number = p.number").
		Where("p.repo = ? AND p.number = ?", repo, number).
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.Repo == "" {
		return nil, nil
	}
	return &row, nil
}

// DeleteProposal removes a proposal row. Returns true if a row was deleted.
func (s *Store) DeleteProposal(repo string, number int) (bool, error) {
	result := s.db.Exec("DELETE FROM proposals WHERE repo = ? AND number = ?", repo, number)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ProposalSummary is per-stage counts for a given filter.
type ProposalSummary struct {
	Kind    string
	Release string
	Total   int
	ByStage map[string]int
	ByPri   map[string]int
}

// SummarizeProposals returns aggregate counts grouped by stage and priority.
func (s *Store) SummarizeProposals(f ProposalFilter) (*ProposalSummary, error) {
	rows, err := s.ListProposals(f)
	if err != nil {
		return nil, err
	}
	sum := &ProposalSummary{
		Kind:    f.Kind,
		Release: f.Release,
		Total:   len(rows),
		ByStage: make(map[string]int),
		ByPri:   make(map[string]int),
	}
	for _, r := range rows {
		if r.Stage != "" {
			sum.ByStage[r.Stage]++
		}
		if r.Priority != "" {
			sum.ByPri[r.Priority]++
		}
	}
	return sum, nil
}
