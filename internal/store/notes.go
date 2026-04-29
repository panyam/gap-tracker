package store

import (
	"encoding/json"
	"fmt"
)

// Note is one ad-hoc note stored in the issues.notes JSON array column.
// Kept intentionally minimal — matches the Python `{text, at}` shape so
// the JSON is interchangeable between the two CLIs.
type Note struct {
	Text string `json:"text"`
	At   string `json:"at"`
}

// IssueNoteSummary is the shape returned by ListIssuesWithNotes for the
// `gapt notes` listing view. Notes are pre-decoded so the CLI doesn't
// re-parse JSON.
type IssueNoteSummary struct {
	Repo   string
	Number int
	Title  string
	State  string
	Notes  []Note
}

// GetNotes returns the parsed notes array for one issue. Returns nil
// (not error) if the issue has no notes, or if the notes column is the
// default empty-array sentinel. Returns (nil, err) if the issue itself
// doesn't exist in the DB.
func (s *Store) GetNotes(repo string, number int) ([]Note, error) {
	var raw string
	err := s.db.Table("issues").
		Select("notes").
		Where("repo = ? AND number = ?", repo, number).
		Row().
		Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("issue %s#%d not in DB — run sync first", repo, number)
	}
	return parseNotesJSON(raw), nil
}

// SetNotes replaces the entire notes array for one issue. Callers that
// want append-only behavior should GetNotes first, append, then SetNotes.
// This keeps the store layer dumb — no merge semantics.
func (s *Store) SetNotes(repo string, number int, notes []Note) error {
	// Confirm the issue exists so we return a nice error instead of
	// silently updating zero rows.
	var count int64
	if err := s.db.Table("issues").
		Where("repo = ? AND number = ?", repo, number).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("issue %s#%d not in DB — run sync first", repo, number)
	}

	encoded, err := json.Marshal(notes)
	if err != nil {
		return err
	}
	if err := s.db.Exec(
		"UPDATE issues SET notes = ? WHERE repo = ? AND number = ?",
		string(encoded), repo, number,
	).Error; err != nil {
		return err
	}
	return s.RebuildFTS()
}

// ListIssuesWithNotes returns every issue whose notes column is
// non-empty, optionally filtered by a substring match against the raw
// JSON. Used by `gapt notes [filter]`.
//
// The filter matches the raw JSON string — the same substring test
// Python uses. This means you can filter on "route:servicekit" or
// "[fail]" or any string that appears in a note's text.
func (s *Store) ListIssuesWithNotes(filter string) ([]IssueNoteSummary, error) {
	type row struct {
		Repo   string
		Number int
		Title  string
		State  string
		Notes  string
	}
	q := s.db.Table("issues").
		Select("repo, number, title, state, notes").
		Where("notes != '[]' AND notes != ''").
		Order("repo, number")
	if filter != "" {
		q = q.Where("notes LIKE ?", "%"+filter+"%")
	}

	var rows []row
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]IssueNoteSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, IssueNoteSummary{
			Repo:   r.Repo,
			Number: r.Number,
			Title:  r.Title,
			State:  r.State,
			Notes:  parseNotesJSON(r.Notes),
		})
	}
	return out, nil
}

// parseNotesJSON decodes the notes column into a slice of Note. Empty
// or malformed content returns nil so callers can len()-check safely.
// If the column is not a valid JSON array, we wrap the whole thing as
// a single note with empty date — matches Python's _load_notes fallback.
func parseNotesJSON(raw string) []Note {
	if raw == "" || raw == "[]" {
		return nil
	}
	var notes []Note
	if err := json.Unmarshal([]byte(raw), &notes); err == nil {
		return notes
	}
	return []Note{{Text: raw}}
}
