package socialtrack

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Status is what happened to a post after it was found.
type Status string

const (
	StatusNew     Status = "new"
	StatusReplied Status = "replied"
	StatusSkipped Status = "skipped"
)

// Post is one thread found on a source. ID is the source's own id, prefixed
// with the source (reddit:t3_abc123), so ids from two sites never collide.
type Post struct {
	ID          string    `gorm:"primaryKey;column:id"`
	Source      string    `gorm:"column:source;not null;index"`
	Community   string    `gorm:"column:community;not null"`
	Title       string    `gorm:"column:title;not null"`
	Author      string    `gorm:"column:author"`
	URL         string    `gorm:"column:url;not null"`
	Body        string    `gorm:"column:body"`
	PostedAt    time.Time `gorm:"column:posted_at;not null;index"`
	FirstSeenAt time.Time `gorm:"column:first_seen_at;not null"`
	Matched     string    `gorm:"column:matched"`      // comma-separated terms that matched
	Topics      string    `gorm:"column:topics;index"` // comma-separated topics that kept the post
	Status      Status    `gorm:"column:status;not null;index"`
	StatusAt    time.Time `gorm:"column:status_at"`
	Note        string    `gorm:"column:note"`

	// Replies is the thread's comment count when last checked, or -1 when it
	// has not been checked. Only the top candidates are checked, since each
	// check costs a request in Reddit's rate window.
	Replies    int       `gorm:"column:replies;not null;default:-1"`
	EnrichedAt time.Time `gorm:"column:enriched_at"`
}

func (Post) TableName() string { return "posts" }

// Visit records the user opening a source to read it by hand (browse), so
// the nudge can say which curated feeds have not been looked at today.
type Visit struct {
	ID     uint      `gorm:"primaryKey"`
	Source string    `gorm:"column:source;not null;index"`
	Topic  string    `gorm:"column:topic"`
	At     time.Time `gorm:"column:at;not null;index"`
}

func (Visit) TableName() string { return "visits" }

// LastVisit is when the user last browsed a source, zero if never.
func LastVisit(db *gorm.DB, source string) (time.Time, error) {
	var v Visit
	err := db.Where("source = ?", source).Order("at desc").Limit(1).Find(&v).Error
	return v.At, err
}

// OpenDB opens (creating if needed) the store at <root>/social.db.
func (r *Resolved) OpenDB() (*gorm.DB, error) {
	path := r.DBFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Post{}, &Visit{}); err != nil {
		return nil, err
	}
	return db, nil
}

// Upsert inserts posts not yet in the store and leaves known ones alone, so a
// post's status and first-seen time survive every later sync. It returns how
// many were new.
func Upsert(db *gorm.DB, posts []Post, now time.Time) (int, error) {
	added := 0
	for _, p := range posts {
		var n int64
		if err := db.Model(&Post{}).Where("id = ?", p.ID).Count(&n).Error; err != nil {
			return added, err
		}
		if n > 0 {
			continue
		}
		p.FirstSeenAt = now
		p.Status = StatusNew
		p.Replies = -1
		if err := db.Create(&p).Error; err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// Mark records what happened to a post.
func Mark(db *gorm.DB, id string, s Status, note string, now time.Time) error {
	res := db.Model(&Post{}).Where("id = ?", id).Updates(map[string]any{"status": s, "status_at": now, "note": note})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errNoPost(id)
	}
	return nil
}

type errNoPost string

func (e errNoPost) Error() string { return "no post with id " + string(e) }

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
