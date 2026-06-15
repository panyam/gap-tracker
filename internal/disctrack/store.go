package disctrack

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Message is one Discord message synced via snapshot.py. Schema ported
// verbatim from mcpcontribd's store.go (commit 005694c era) so existing
// data files migrate without conversion.
type Message struct {
	ChannelID    string `gorm:"primaryKey;column:channel_id"`
	MessageID    string `gorm:"primaryKey;column:message_id"`
	TS           string `gorm:"column:ts;not null;index:idx_messages_channel_ts,priority:2"`
	Author       string `gorm:"column:author;not null"`
	Content      string `gorm:"column:content;not null"`
	URLs         string `gorm:"column:urls"`
	ReplyTo      string `gorm:"column:reply_to"`
	FirstSeenAt  string `gorm:"column:first_seen_at;not null"`
	LastEditedAt string `gorm:"column:last_edited_at"`
}

func (Message) TableName() string { return "messages" }

// Cursor tracks how far a given (area, source_type, source_key) tuple has
// been synced. For Discord the source_type is "discord" and source_key is
// the channel_id; area is the channel name (or id when unknown). PR-side
// cursors are tracked elsewhere (gapt).
type Cursor struct {
	Area        string `gorm:"primaryKey;column:area"`
	SourceType  string `gorm:"primaryKey;column:source_type"`
	SourceKey   string `gorm:"primaryKey;column:source_key"`
	CursorValue string `gorm:"column:cursor_value;not null"`
	UpdatedAt   string `gorm:"column:updated_at;not null"`
}

func (Cursor) TableName() string { return "cursors" }

// DBFile is the canonical messages.db location for this tracking root.
func (r *Resolved) DBFile() string {
	return filepath.Join(r.Root, "messages.db")
}

// BackupDir is where rotated DB snapshots live.
func (r *Resolved) BackupDir() string {
	return filepath.Join(r.Root, "backups")
}

// OpenDB opens (creating if needed) the SQLite database at <root>/messages.db
// and runs AutoMigrate for the Message + Cursor tables. Callers should defer
// nothing — the underlying gorm.DB pools internally; closing on every command
// adds latency and gorm has no public Close anyway.
func (r *Resolved) OpenDB() (*gorm.DB, error) {
	path := r.DBFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("ensure db dir: %w", err)
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.AutoMigrate(&Message{}, &Cursor{}); err != nil {
		return nil, fmt.Errorf("automigrate: %w", err)
	}
	return db, nil
}

// NowISO returns the current UTC time as an RFC3339 string. Cursor.UpdatedAt
// and Message.FirstSeenAt/LastEditedAt all use this format.
func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
