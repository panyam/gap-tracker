package disctrack

import (
	"encoding/json"

	"gorm.io/gorm"
)

// MessageView is the shape returned by `disctrack messages`. URLs is decoded
// back from the JSON-encoded string stored in the messages table.
type MessageView struct {
	ChannelID    string   `json:"channel_id" yaml:"channel_id"`
	MessageID    string   `json:"message_id" yaml:"message_id"`
	TS           string   `json:"ts" yaml:"ts"`
	Author       string   `json:"author" yaml:"author"`
	Content      string   `json:"content" yaml:"content"`
	URLs         []string `json:"urls" yaml:"urls"`
	ReplyTo      string   `json:"reply_to,omitempty" yaml:"reply_to,omitempty"`
	LastEditedAt string   `json:"last_edited_at,omitempty" yaml:"last_edited_at,omitempty"`
	ThreadID     string   `json:"thread_id,omitempty" yaml:"thread_id,omitempty"`
}

// MessagesSince returns messages for channelID strictly newer than sinceTS,
// ordered by ts asc. Pass empty sinceTS to fetch every message for the channel.
func MessagesSince(db *gorm.DB, channelID, sinceTS string) ([]MessageView, error) {
	q := db.Where("channel_id = ?", channelID).Order("ts asc")
	if sinceTS != "" {
		q = q.Where("ts > ?", sinceTS)
	}
	var rows []Message
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]MessageView, 0, len(rows))
	for _, r := range rows {
		var urls []string
		if r.URLs != "" {
			_ = json.Unmarshal([]byte(r.URLs), &urls)
		}
		out = append(out, MessageView{
			ChannelID:    r.ChannelID,
			MessageID:    r.MessageID,
			TS:           r.TS,
			Author:       r.Author,
			Content:      r.Content,
			URLs:         urls,
			ReplyTo:      r.ReplyTo,
			LastEditedAt: r.LastEditedAt,
			ThreadID:     r.ThreadID,
		})
	}
	return out, nil
}
