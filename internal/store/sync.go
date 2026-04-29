package store

import (
	"fmt"
	"strings"

	"github.com/panyam/gap-tracker/internal/github"
)

// UpsertIssues writes a batch of fetched issues to the issues table.
//
// Semantics match Python's sync_repo: on conflict (repo, number) we
// update title/state/labels/updated_at/url/body/synced_at, but we DO NOT
// overwrite the notes column — notes are manually curated and must
// survive re-sync. This is the whole reason we bypass GORM's Save and
// write raw SQL: Save would happily clobber notes on conflict.
//
// Returns the count of rows affected (insert + update combined).
func (s *Store) UpsertIssues(repo string, issues []github.Issue) (int, error) {
	const stmt = `
INSERT INTO issues (repo, number, title, state, labels, created_at, updated_at, url, body, synced_at, notes)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), '[]')
ON CONFLICT(repo, number) DO UPDATE SET
    title      = excluded.title,
    state      = excluded.state,
    labels     = excluded.labels,
    updated_at = excluded.updated_at,
    url        = excluded.url,
    body       = excluded.body,
    synced_at  = excluded.synced_at
    /* notes column intentionally NOT overwritten */
`
	count := 0
	for _, i := range issues {
		labels := strings.Join(i.Labels, ",")
		result := s.db.Exec(
			stmt,
			repo, i.Number, i.Title, i.State, labels,
			i.CreatedAt, i.UpdatedAt, i.URL, i.Body,
		)
		if result.Error != nil {
			return count, result.Error
		}
		count++
	}
	// Rebuild FTS index after bulk upsert.
	if err := s.RebuildFTS(); err != nil {
		return count, fmt.Errorf("rebuild fts after sync: %w", err)
	}
	return count, nil
}

// LogSync records one sync_repo invocation in the sync_log table.
// Observability-only — nothing reads this yet but it helps with
// debugging when something goes wrong.
func (s *Store) LogSync(repo string, issueCount, offset, count int) error {
	return s.db.Exec(
		`INSERT INTO sync_log (repo, issue_count, offset_used, count_used)
		 VALUES (?, ?, ?, ?)`,
		repo, issueCount, offset, count,
	).Error
}

// CountIssuesByRepo returns the number of issues we've synced for a repo.
// Used by sync for the "n issues (#lo-#hi)" progress line.
func (s *Store) CountIssuesByRepo(repo string) (int, error) {
	var n int64
	err := s.db.Table("issues").Where("repo = ?", repo).Count(&n).Error
	return int(n), err
}
