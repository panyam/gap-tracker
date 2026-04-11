package store

import (
	"github.com/panyam/gap-tracker/internal/model"
)

// ListTriageFilter restricts a triage list query to one bucket and/or one
// source repo. Zero values mean "no filter" on that field.
type ListTriageFilter struct {
	Bucket string // empty = all buckets
	Repo   string // empty = all repos
}

// ListTriage returns triage rows joined against issues so callers get the
// current issue title and state without a second query. Ordered by
// (bucket, repo, number) for deterministic output.
type TriageWithIssue struct {
	model.TriageEntry
	Title string
	State string
}

// ListTriage returns classified issues matching the filter.
func (s *Store) ListTriage(f ListTriageFilter) ([]TriageWithIssue, error) {
	q := s.db.Table("triage AS t").
		Select(`t.repo, t.number, t.bucket, t.target, t.rationale, t.test,
		        t.classified_at, t.updated_at,
		        i.title AS title, i.state AS state`).
		Joins("LEFT JOIN issues i ON i.repo = t.repo AND i.number = t.number").
		Order("t.bucket, t.repo, t.number")

	if f.Bucket != "" {
		q = q.Where("t.bucket = ?", f.Bucket)
	}
	if f.Repo != "" {
		q = q.Where("t.repo = ?", f.Repo)
	}

	var rows []TriageWithIssue
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetTriage fetches a single triage row by (repo, number). Returns
// (nil, nil) if no row matches — NOT an error, so callers can display a
// clean "not classified" message.
func (s *Store) GetTriage(repo string, number int) (*model.TriageEntry, error) {
	var t model.TriageEntry
	err := s.db.Where("repo = ? AND number = ?", repo, number).First(&t).Error
	if err != nil {
		// GORM returns gorm.ErrRecordNotFound; treat as "no row" not error.
		if err.Error() == "record not found" {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// BucketCountsBySource returns the per-bucket tally for each repo, used
// to fill the status summary table. Map is [repo][bucket] -> count.
func (s *Store) BucketCountsBySource() (map[string]map[string]int, error) {
	type row struct {
		Repo   string
		Bucket string
		N      int
	}
	var rows []row
	err := s.db.Table("triage").
		Select("repo, bucket, COUNT(*) AS n").
		Group("repo, bucket").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]int{}
	for _, r := range rows {
		if out[r.Repo] == nil {
			out[r.Repo] = map[string]int{}
		}
		out[r.Repo][r.Bucket] = r.N
	}
	return out, nil
}

// OpenIssuesBySource returns all OPEN issues for a repo, ordered by
// number. Used to compute "unclassified" as the set difference between
// this list and the triage classified set.
func (s *Store) OpenIssuesBySource(repo string) ([]model.Issue, error) {
	var issues []model.Issue
	err := s.db.
		Select("repo, number, title, state").
		Where("repo = ? AND state = ?", repo, "OPEN").
		Order("number").
		Find(&issues).Error
	return issues, err
}

// ClassifiedNumbers returns the set of issue numbers that have a triage
// row for a given repo. Used alongside OpenIssuesBySource to find
// unclassified open issues.
func (s *Store) ClassifiedNumbers(repo string) (map[int]bool, error) {
	var nums []int
	err := s.db.Table("triage").
		Where("repo = ?", repo).
		Pluck("number", &nums).Error
	if err != nil {
		return nil, err
	}
	set := make(map[int]bool, len(nums))
	for _, n := range nums {
		set[n] = true
	}
	return set, nil
}

// GetIssue fetches an issue by (repo, number). Returns (nil, nil) if not
// present — matches GetTriage semantics.
func (s *Store) GetIssue(repo string, number int) (*model.Issue, error) {
	var i model.Issue
	err := s.db.Where("repo = ? AND number = ?", repo, number).First(&i).Error
	if err != nil {
		if err.Error() == "record not found" {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

// ListGapsBySourceRef returns legacy gaps table rows whose source_evidence
// column mentions the given ref string (e.g. "go-sdk:148"). Used by `show`
// to surface the legacy gap alongside the triage row.
func (s *Store) ListGapsBySourceRef(ref string) ([]model.Gap, error) {
	var gaps []model.Gap
	err := s.db.
		Where("source_evidence LIKE ?", "%"+ref+"%").
		Find(&gaps).Error
	return gaps, err
}
