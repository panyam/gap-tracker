package model

// Gap is a feature-level mapping between a local target issue and one or
// more source issue references. Legacy table — the primary classification
// surface is now TriageEntry, but Gap survives for link/mark/report.
//
// Matches the Python-era `gaps` table:
//
//	CREATE TABLE gaps (
//	    id              INTEGER PRIMARY KEY AUTOINCREMENT,
//	    feature         TEXT NOT NULL UNIQUE,
//	    description     TEXT DEFAULT '',
//	    target_issue    INTEGER,
//	    source_evidence TEXT DEFAULT '',
//	    status          TEXT NOT NULL DEFAULT 'open',
//	    priority        TEXT DEFAULT '',
//	    notes           TEXT DEFAULT '[]',
//	    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
//	    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
//	);
type Gap struct {
	ID             int64  `gorm:"column:id;primaryKey;autoIncrement"                  json:"id"             yaml:"id"`
	Feature        string `gorm:"column:feature;not null;unique"                      json:"feature"        yaml:"feature"`
	Description    string `gorm:"column:description;default:''"                       json:"description"    yaml:"description"`
	TargetIssue    *int   `gorm:"column:target_issue"                                 json:"targetIssue"    yaml:"target_issue"`
	SourceEvidence string `gorm:"column:source_evidence;default:''"                   json:"sourceEvidence" yaml:"source_evidence"`
	Status         string `gorm:"column:status;not null;default:'open'"               json:"status"         yaml:"status"`
	Priority       string `gorm:"column:priority;default:''"                          json:"priority"       yaml:"priority"`
	Notes          string `gorm:"column:notes;default:'[]'"                           json:"notes"          yaml:"notes"`
	CreatedAt      string `gorm:"column:created_at;not null;default:(datetime('now'))" json:"createdAt"     yaml:"created_at"`
	UpdatedAt      string `gorm:"column:updated_at;not null;default:(datetime('now'))" json:"updatedAt"     yaml:"updated_at"`
}

func (Gap) TableName() string { return "gaps" }
