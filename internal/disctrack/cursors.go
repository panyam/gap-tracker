package disctrack

import (
	"errors"

	"gorm.io/gorm"
)

const SourceTypeDiscord = "discord"

// CursorView is what get/list return: a Cursor plus the resolved channel
// name (when known) so JSON output carries both.
type CursorView struct {
	Found       bool   `json:"found"`
	Channel     string `json:"channel,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	SourceKey   string `json:"source_key,omitempty"`
	CursorValue string `json:"cursor_value,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// GetDiscordCursor reads the cursor row for source_type=discord, source_key=channelID.
// Returns Found=false (no error) when no row exists.
func GetDiscordCursor(db *gorm.DB, channelID, channelName string) (CursorView, error) {
	var c Cursor
	err := db.Where("source_type = ? AND source_key = ?", SourceTypeDiscord, channelID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CursorView{Found: false}, nil
	}
	if err != nil {
		return CursorView{}, err
	}
	return CursorView{
		Found:       true,
		Channel:     channelName,
		ChannelID:   channelID,
		SourceType:  c.SourceType,
		SourceKey:   c.SourceKey,
		CursorValue: c.CursorValue,
		UpdatedAt:   c.UpdatedAt,
	}, nil
}

// SetDiscordCursor upserts the cursor row for source_type=discord. The
// ChannelName field defaults to channelName (or channelID when name is unknown).
func SetDiscordCursor(db *gorm.DB, channelID, channelName, value string) error {
	name := channelName
	if name == "" {
		name = channelID
	}
	c := Cursor{
		ChannelName: name,
		SourceType:  SourceTypeDiscord,
		SourceKey:   channelID,
		CursorValue: value,
		UpdatedAt:   NowISO(),
	}
	return db.Save(&c).Error
}

// ListCursors returns every cursor row, optionally filtered to one
// (source_type, source_key) pair. Ordered by (channel name, source_type, source_key).
func ListCursors(db *gorm.DB, sourceType, sourceKey string) ([]Cursor, error) {
	q := db.Order("area, source_type, source_key") // area is the channel-name column
	if sourceType != "" && sourceKey != "" {
		q = q.Where("source_type = ? AND source_key = ?", sourceType, sourceKey)
	}
	var rows []Cursor
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
