package model

// SyncLog records each sync_repo call for observability. Never read by the
// tool today — kept for future debugging and to round out schema parity.
//
// Matches the Python-era `sync_log` table:
//
//	CREATE TABLE sync_log (
//	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
//	    repo        TEXT NOT NULL,
//	    synced_at   TEXT NOT NULL DEFAULT (datetime('now')),
//	    issue_count INTEGER NOT NULL,
//	    offset_used INTEGER DEFAULT 0,
//	    count_used  INTEGER DEFAULT 100
//	);
type SyncLog struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"                    json:"id"          yaml:"id"`
	Repo       string `gorm:"column:repo;not null"                                  json:"repo"        yaml:"repo"`
	SyncedAt   string `gorm:"column:synced_at;not null;default:(datetime('now'))"   json:"syncedAt"    yaml:"synced_at"`
	IssueCount int    `gorm:"column:issue_count;not null"                           json:"issueCount"  yaml:"issue_count"`
	OffsetUsed int    `gorm:"column:offset_used;default:0"                          json:"offsetUsed"  yaml:"offset_used"`
	CountUsed  int    `gorm:"column:count_used;default:100"                         json:"countUsed"   yaml:"count_used"`
}

func (SyncLog) TableName() string { return "sync_log" }
