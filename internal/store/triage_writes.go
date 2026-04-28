package store

import (
	"fmt"
)

// ValidBuckets is the authoritative set of allowed triage bucket names.
// Kept here (not in a triage/ package) so store enforces the invariant
// at write time. CLI code uses a separate validator for early feedback
// but the DB write is the final gate.
var ValidBuckets = map[string]bool{
	"real-gap":        true,
	"in-progress":    true,
	"already-covered": true,
	"defer-spec-flux": true,
	"defer-scope":     true,
	"not-applicable":  true,
	"done":            true,
}

// UpsertTriageInput carries the fields that a set/upsert can touch.
// Fields are all value types so an empty-string Target/Test/Rationale
// means "set this column to empty". If you want to leave a column
// untouched on update, use a dedicated method instead of shoehorning
// "partial update" semantics into this struct.
type UpsertTriageInput struct {
	Repo      string
	Number    int
	Bucket    string
	Target    string
	Rationale string
	Test      string
}

// UpsertTriage inserts or updates a triage row. classified_at is only
// set on first insert (preserved on updates); updated_at bumps every
// write. Matches the Python cmd_triage_set semantics.
//
// Returns an error if the bucket is not in ValidBuckets or if the
// referenced issue doesn't exist (FK would fire at write time but we
// check upfront for a nicer error).
func (s *Store) UpsertTriage(in UpsertTriageInput) error {
	if !ValidBuckets[in.Bucket] {
		return fmt.Errorf("invalid bucket %q (must be one of: real-gap, already-covered, defer-spec-flux, defer-scope, not-applicable, done)", in.Bucket)
	}

	// Verify the issue exists so we surface "sync first" instead of a
	// cryptic FOREIGN KEY constraint failure.
	var count int64
	if err := s.db.Table("issues").
		Where("repo = ? AND number = ?", in.Repo, in.Number).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("issue %s#%d not in DB — run sync first", in.Repo, in.Number)
	}

	// Preserve test column if in.Test is empty on update (matches the
	// Python CLI which only overwrites test when --test is passed).
	// classified_at only written on first insert.
	const stmt = `
INSERT INTO triage (repo, number, bucket, target, rationale, test, classified_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
ON CONFLICT(repo, number) DO UPDATE SET
    bucket     = excluded.bucket,
    target     = excluded.target,
    rationale  = excluded.rationale,
    test       = CASE WHEN excluded.test != '' THEN excluded.test ELSE triage.test END,
    updated_at = datetime('now')
`
	return s.db.Exec(stmt,
		in.Repo, in.Number, in.Bucket,
		in.Target, in.Rationale, in.Test,
	).Error
}

// DeleteTriage removes a classification. Returns (true, nil) if a row
// was deleted, (false, nil) if no row matched (not an error — idempotent
// unset), or (false, err) on DB failures.
func (s *Store) DeleteTriage(repo string, number int) (bool, error) {
	result := s.db.Exec(
		"DELETE FROM triage WHERE repo = ? AND number = ?", repo, number,
	)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// CloseTriage flips the triage bucket to 'done' AND appends a closure
// stamp to the rationale so you can see at a glance when it was marked
// resolved.
//
// Crucially, callers are responsible for verifying the target issue is
// actually CLOSED before calling this — the store does not re-check.
// cli/triage.go's close subcommand owns the safety check so the error
// message is helpful ("run sync target:0 first", etc.).
func (s *Store) CloseTriage(repo string, number int, targetRef, today string) error {
	// Fetch current rationale so we can append, not replace.
	var existing struct {
		Rationale string
		Target    string
	}
	if err := s.db.Table("triage").
		Select("rationale, target").
		Where("repo = ? AND number = ?", repo, number).
		Scan(&existing).Error; err != nil {
		return err
	}

	newTarget := targetRef
	if newTarget == "" {
		newTarget = existing.Target
	}

	newRationale := existing.Rationale
	stamp := fmt.Sprintf(
		" [closed via triage close on %s — target %s is CLOSED]",
		today, newTarget,
	)
	if newRationale == "" {
		newRationale = stamp
	} else {
		newRationale += stamp
	}

	return s.db.Exec(`
UPDATE triage
   SET bucket = 'done',
       target = ?,
       rationale = ?,
       updated_at = datetime('now')
 WHERE repo = ? AND number = ?
`, newTarget, newRationale, repo, number).Error
}
