package model

// TriageEntry is one classified source issue. The primary source of truth
// for all triage state — see docs/STATE_MACHINE.md for the full flow.
//
// Matches the schema we added in the DB-primary refactor (see main.py):
//
//	CREATE TABLE triage (
//	    repo          TEXT NOT NULL,
//	    number        INTEGER NOT NULL,
//	    bucket        TEXT NOT NULL,
//	    target        TEXT DEFAULT '',
//	    rationale     TEXT DEFAULT '',
//	    test          TEXT DEFAULT '',
//	    classified_at TEXT NOT NULL DEFAULT (datetime('now')),
//	    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
//	    PRIMARY KEY (repo, number),
//	    FOREIGN KEY (repo, number) REFERENCES issues(repo, number)
//	);
//
// Buckets are one of: real-gap, already-covered, defer-spec-flux,
// defer-scope, not-applicable, done. Validation lives in internal/triage.
type TriageEntry struct {
	Repo         string `gorm:"column:repo;primaryKey;not null"                           json:"repo"         yaml:"repo"`
	Number       int    `gorm:"column:number;primaryKey;not null"                         json:"number"       yaml:"number"`
	Bucket       string `gorm:"column:bucket;not null"                                    json:"bucket"       yaml:"bucket"`
	Target       string `gorm:"column:target;default:''"                                  json:"target"       yaml:"target,omitempty"`
	Rationale    string `gorm:"column:rationale;default:''"                               json:"rationale"    yaml:"rationale"`
	Test         string `gorm:"column:test;default:''"                                    json:"test"         yaml:"test,omitempty"`
	ClassifiedAt string `gorm:"column:classified_at;not null;default:(datetime('now'))"   json:"classifiedAt" yaml:"classified_at"`
	UpdatedAt    string `gorm:"column:updated_at;not null;default:(datetime('now'))"      json:"updatedAt"    yaml:"updated_at"`
}

func (TriageEntry) TableName() string { return "triage" }
