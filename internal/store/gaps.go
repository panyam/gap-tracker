package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/panyam/gap-tracker/internal/model"
)

// LinkGap upserts a row in the legacy `gaps` table. Feature name is the
// unique key — re-linking with the same feature updates target_issue
// and source_evidence. Runs a duplicate-evidence check first: if any
// ref in the comma-separated source_evidence is already attached to a
// DIFFERENT feature, returns an error naming the existing gap. This
// matches Python's cmd_link behavior.
//
// Does NOT fetch the target issue — that's the caller's job (CLI layer
// coordinates with github.Client).
func (s *Store) LinkGap(feature string, targetIssue int, sourceEvidence string) error {
	for _, rawRef := range strings.Split(sourceEvidence, ",") {
		ref := strings.TrimSpace(rawRef)
		if ref == "" {
			continue
		}
		var clash model.Gap
		err := s.db.
			Where("source_evidence LIKE ? AND feature != ?", "%"+ref+"%", feature).
			First(&clash).Error
		if err == nil {
			return fmt.Errorf(
				"%s is already linked to gap %q → #%d; use a different feature name or update the existing gap",
				ref, clash.Feature, derefInt(clash.TargetIssue),
			)
		}
	}

	const stmt = `
INSERT INTO gaps (feature, target_issue, source_evidence, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(feature) DO UPDATE SET
    target_issue = excluded.target_issue,
    source_evidence = excluded.source_evidence,
    updated_at = datetime('now')
`
	return s.db.Exec(stmt, feature, targetIssue, sourceEvidence).Error
}

// MarkGap updates the status of a gap by feature name. Mirrors Python:
// no existence check, no explicit error if the feature doesn't exist —
// just `UPDATE` and trust the caller.
func (s *Store) MarkGap(feature, status string) error {
	return s.db.Exec(
		"UPDATE gaps SET status = ?, updated_at = datetime('now') WHERE feature = ?",
		status, feature,
	).Error
}

// EnsureTargetIssue makes sure (targetRepo, number) exists in the
// issues table. If it doesn't, the caller is expected to fetch it via
// github.Client and pass the result here. This exists so cmd_link can
// cache a fresh title for report/show without doing its own upsert
// dance.
//
// Returns (true, nil) if a row already existed, (false, nil) if a new
// row was inserted using the passed fetched issue, (false, err) on
// failure. If fetched is nil and no row exists, returns (false, nil)
// and leaves the DB unchanged.
func (s *Store) EnsureTargetIssue(targetRepo string, number int, fetched *fetchedIssue) (bool, error) {
	var count int64
	if err := s.db.Table("issues").
		Where("repo = ? AND number = ?", targetRepo, number).
		Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	if fetched == nil {
		return false, nil // no existing row, no fetched payload — noop
	}

	const stmt = `
INSERT INTO issues (repo, number, title, state, labels, created_at, updated_at, url, body, synced_at, notes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), '[]')
ON CONFLICT(repo, number) DO UPDATE SET
    title      = excluded.title,
    state      = excluded.state,
    labels     = excluded.labels,
    updated_at = excluded.updated_at,
    synced_at  = excluded.synced_at
    /* notes preserved */
`
	return false, s.db.Exec(
		stmt,
		targetRepo, fetched.Number, fetched.Title, fetched.State, fetched.Labels,
		fetched.CreatedAt, fetched.UpdatedAt, fetched.URL, fetched.Body,
	).Error
}

// fetchedIssue is a tiny struct so EnsureTargetIssue doesn't have to
// import internal/github and form an import cycle. CLI layer converts
// a github.Issue into this shape at the call site.
type fetchedIssue struct {
	Number    int
	Title     string
	State     string
	Labels    string // joined with ","
	CreatedAt string
	UpdatedAt string
	URL       string
	Body      string
}

// NewFetchedIssue is a helper the CLI uses to convert from its
// github.Issue into the store-local fetchedIssue shape without leaking
// github.Issue into this package.
func NewFetchedIssue(number int, title, state, labelsCSV, createdAt, updatedAt, url, body string) *fetchedIssue {
	return &fetchedIssue{
		Number:    number,
		Title:     title,
		State:     state,
		Labels:    labelsCSV,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		URL:       url,
		Body:      body,
	}
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ReconcileCandidate is one row returned by ListReconcileCandidates —
// a gap in 'open' or 'in-progress' state whose target_issue might now
// be closed upstream.
type ReconcileCandidate struct {
	Feature     string
	TargetIssue int
	Status      string
	Notes       string
	IssueState  *string // nil if the target issue isn't in the DB
	IssueTitle  *string // nil if the target issue isn't in the DB
}

// ListReconcileCandidates returns every gap that's still "active" (open
// or in-progress) and has a linked target issue we can check against
// the DB. The caller then walks the list and decides what to close.
func (s *Store) ListReconcileCandidates(targetRepo string) ([]ReconcileCandidate, error) {
	const query = `
SELECT g.feature, g.target_issue, g.status, g.notes,
       i.state AS issue_state, i.title AS issue_title
  FROM gaps g
  LEFT JOIN issues i ON i.repo = ? AND i.number = g.target_issue
 WHERE g.target_issue > 0
   AND g.status IN ('open', 'in-progress')
`
	type row struct {
		Feature     string
		TargetIssue int
		Status      string
		Notes       string
		IssueState  *string
		IssueTitle  *string
	}
	var rows []row
	if err := s.db.Raw(query, targetRepo).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ReconcileCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, ReconcileCandidate(r))
	}
	return out, nil
}

// CloseGapWithNote flips a gap's status to 'closed' and appends a note
// to its notes column in a single atomic update pair. Used by
// reconcile when a target issue is detected as closed upstream.
func (s *Store) CloseGapWithNote(feature string, note Note) error {
	// Fetch current notes for the gap.
	var raw string
	row := s.db.Table("gaps").
		Select("notes").
		Where("feature = ?", feature).
		Row()
	if err := row.Scan(&raw); err != nil {
		return fmt.Errorf("read gap notes for %q: %w", feature, err)
	}
	notes := parseNotesJSON(raw)
	notes = append(notes, note)
	encoded, err := jsonMarshalNotes(notes)
	if err != nil {
		return err
	}
	return s.db.Exec(`
UPDATE gaps
   SET status = 'closed',
       notes = ?,
       updated_at = datetime('now')
 WHERE feature = ?
`, encoded, feature).Error
}

// jsonMarshalNotes serializes a slice of Note to JSON. Extracted so
// both SetNotes and CloseGapWithNote share encoding behavior.
func jsonMarshalNotes(notes []Note) (string, error) {
	bytes, err := json.Marshal(notes)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// AllGaps returns every row in the gaps table, unordered. The caller
// is responsible for sorting; report has a custom sort key.
func (s *Store) AllGaps() ([]model.Gap, error) {
	var gaps []model.Gap
	err := s.db.Find(&gaps).Error
	return gaps, err
}

// OpenIssuesByRepoFull returns OPEN issues for a repo with enough
// columns to render report's Untracked section — includes labels
// and updated_at that OpenIssuesBySource doesn't bring back.
func (s *Store) OpenIssuesByRepoFull(repo string) ([]model.Issue, error) {
	var issues []model.Issue
	err := s.db.
		Select("repo, number, title, state, labels, updated_at").
		Where("repo = ? AND state = ?", repo, "OPEN").
		Order("number").
		Find(&issues).Error
	return issues, err
}
