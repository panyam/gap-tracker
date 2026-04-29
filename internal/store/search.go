package store

import "fmt"

// RebuildFTS rebuilds the FTS5 index from the issues table. This is the
// recommended approach for external-content FTS5 tables: rather than
// maintaining triggers, we rebuild after bulk writes. Cheap — the issues
// table is small (hundreds to low thousands of rows).
func (s *Store) RebuildFTS() error {
	return s.db.Exec("INSERT INTO issues_fts(issues_fts) VALUES('rebuild')").Error
}

// SearchFilter restricts a full-text search. Zero values mean "no filter".
type SearchFilter struct {
	Repo   string // restrict to one source repo
	Bucket string // restrict to issues classified in this bucket
	State  string // "OPEN" / "CLOSED"
	Limit  int    // max results (0 = default 50)
}

// SearchResult is one hit from a full-text search.
type SearchResult struct {
	Repo    string
	Number  int
	Title   string
	State   string
	Snippet string  // FTS5 snippet with match highlighting
	Rank    float64 // bm25 relevance (lower = more relevant)
	Bucket  string  // triage bucket, empty if unclassified
	Target  string  // triage target, empty if unclassified
}

// Search runs a full-text query against the FTS5 index and returns
// results ranked by bm25 relevance. The query uses FTS5 syntax:
// bare words, "phrases", boolean (AND/OR/NOT), prefix (word*).
//
// Results are joined against the triage table so callers see
// classification status alongside the match.
func (s *Store) Search(query string, f SearchFilter) ([]SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	// Build the query. We join issues_fts → issues (via rowid) → triage.
	// FTS5's snippet() highlights matches with [...] markers.
	sql := `
SELECT i.repo, i.number, i.title, i.state,
       snippet(issues_fts, 1, '[', ']', '...', 32) AS snippet,
       bm25(issues_fts) AS rank,
       COALESCE(t.bucket, '') AS bucket,
       COALESCE(t.target, '') AS target
FROM issues_fts
JOIN issues i ON i.rowid = issues_fts.rowid
LEFT JOIN triage t ON t.repo = i.repo AND t.number = i.number
WHERE issues_fts MATCH ?
`
	args := []any{query}

	if f.Repo != "" {
		sql += " AND i.repo = ?"
		args = append(args, f.Repo)
	}
	if f.State != "" {
		sql += " AND i.state = ?"
		args = append(args, f.State)
	}
	if f.Bucket != "" {
		sql += " AND t.bucket = ?"
		args = append(args, f.Bucket)
	}

	sql += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	var rows []SearchResult
	if err := s.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("fts5 search: %w", err)
	}
	return rows, nil
}
