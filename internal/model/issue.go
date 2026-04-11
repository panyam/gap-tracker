// Package model defines the GORM models used by gap-tracker.
//
// These structs map to tables in the SQLite DB (or in the future, Postgres
// via a driver swap). Field tags are chosen so GORM's AutoMigrate produces
// a schema byte-compatible with the Python-era schema defined in main.py's
// _create_tables function — same column names, types, defaults, and
// nullability. If you change a field here, run the M1 schema drift check
// before committing.
package model

// Issue is one GitHub issue synced from any repo (source or target).
// Primary key is the (repo, number) tuple.
//
// Matches the Python-era `issues` table:
//
//	CREATE TABLE issues (
//	    repo        TEXT NOT NULL,
//	    number      INTEGER NOT NULL,
//	    title       TEXT NOT NULL,
//	    state       TEXT NOT NULL,
//	    labels      TEXT DEFAULT '',
//	    created_at  TEXT NOT NULL,
//	    updated_at  TEXT NOT NULL,
//	    url         TEXT NOT NULL,
//	    body        TEXT DEFAULT '',
//	    synced_at   TEXT NOT NULL DEFAULT (datetime('now')),
//	    notes       TEXT DEFAULT '[]',
//	    PRIMARY KEY (repo, number)
//	);
type Issue struct {
	Repo      string `gorm:"column:repo;primaryKey;not null"                     json:"repo"       yaml:"repo"`
	Number    int    `gorm:"column:number;primaryKey;not null"                   json:"number"     yaml:"number"`
	Title     string `gorm:"column:title;not null"                               json:"title"      yaml:"title"`
	State     string `gorm:"column:state;not null"                               json:"state"      yaml:"state"`
	Labels    string `gorm:"column:labels;default:''"                            json:"labels"     yaml:"labels"`
	CreatedAt string `gorm:"column:created_at;not null"                          json:"createdAt"  yaml:"created_at"`
	UpdatedAt string `gorm:"column:updated_at;not null"                          json:"updatedAt"  yaml:"updated_at"`
	URL       string `gorm:"column:url;not null"                                 json:"url"        yaml:"url"`
	Body      string `gorm:"column:body;default:''"                              json:"body"       yaml:"body"`
	SyncedAt  string `gorm:"column:synced_at;not null;default:(datetime('now'))" json:"syncedAt"   yaml:"synced_at"`
	Notes     string `gorm:"column:notes;default:'[]'"                           json:"notes"      yaml:"notes"`
}

// TableName overrides GORM's default pluralization ("issues" is already plural).
func (Issue) TableName() string { return "issues" }
