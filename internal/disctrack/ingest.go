package disctrack

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gorm.io/gorm"
)

// IncomingMessage is the JSONL record shape snapshot.py emits (or any
// other producer that writes one Discord message per line).
type IncomingMessage struct {
	ChannelID string   `json:"channel_id"`
	MessageID string   `json:"message_id"`
	TS        string   `json:"ts"`
	Author    string   `json:"author"`
	Content   string   `json:"content"`
	URLs      []string `json:"urls"`
	EditedTS  string   `json:"edited_ts"`
	ReplyTo   string   `json:"reply_to"`
	ThreadID  string   `json:"thread_id"`
}

// IngestStats summarises one batch of ingested messages. Matches mcpcontribd's
// shape so /channel-refresh's expected output parsing stays identical.
type IngestStats struct {
	Inserted   int    `json:"new"`
	Edits      int    `json:"edits"`
	Duplicates int    `json:"duplicates"`
	Errors     int    `json:"errors"`
	OldestTS   string `json:"oldest_ts"`
	NewestTS   string `json:"newest_ts"`
}

// Ingest reads JSONL from r and upserts rows into the messages table. One
// transaction per channel batch. Returns aggregate stats.
//
// Semantics per (channel_id, message_id):
//   - new row → INSERT
//   - existing, same content → noop (duplicates count)
//   - existing, different content → UPDATE content + last_edited_at
//
// Pass a non-nil resolver to denormalize parent_channel_id from each
// channel's manifest onto the inserted rows. nil resolver still works for
// callers that don't care about thread parentage.
func Ingest(db *gorm.DB, r io.Reader, resolved *Resolved) (IngestStats, error) {
	incoming, parseErrors, err := readJSONL(r)
	if err != nil {
		return IngestStats{}, err
	}
	stats, err := IngestMessages(db, incoming, resolved)
	stats.Errors += parseErrors
	return stats, err
}

// IngestMessages is Ingest over already-parsed records. The batched thread
// sync uses it to ingest each target's records separately, so every thread
// gets its own stats and its own transaction.
func IngestMessages(db *gorm.DB, incoming []IncomingMessage, resolved *Resolved) (IngestStats, error) {
	var stats IngestStats
	err := db.Transaction(func(tx *gorm.DB) error {
		for channelID, msgs := range groupByChannel(incoming) {
			parentID := ""
			if resolved != nil {
				parentID = lookupParentChannelID(resolved, channelID)
			}
			if err := applyBatch(tx, channelID, parentID, msgs, &stats); err != nil {
				return err
			}
		}
		return nil
	})
	return stats, err
}

// lookupParentChannelID finds the manifest for channelID via reverse lookup,
// then resolves its parent. Empty result when the channel isn't a configured
// thread or the manifest is missing.
func lookupParentChannelID(r *Resolved, channelID string) string {
	name := r.ReverseLookupChannelName(channelID)
	if name == "" {
		return ""
	}
	m, _, err := r.LoadChannel(name)
	if err != nil || m == nil {
		return ""
	}
	return r.ResolveParentChannelID(m)
}

func readJSONL(r io.Reader) ([]IncomingMessage, int, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []IncomingMessage
	parseErrors := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var m IncomingMessage
		if err := json.Unmarshal(line, &m); err != nil {
			parseErrors++
			fmt.Fprintln(os.Stderr, "warn: invalid JSON line:", err)
			continue
		}
		if m.ChannelID == "" || m.MessageID == "" || m.TS == "" {
			parseErrors++
			fmt.Fprintln(os.Stderr, "warn: missing required field (channel_id, message_id, ts):", string(line))
			continue
		}
		out = append(out, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, parseErrors, fmt.Errorf("scan: %w", err)
	}
	return out, parseErrors, nil
}

func groupByChannel(msgs []IncomingMessage) map[string][]IncomingMessage {
	out := map[string][]IncomingMessage{}
	for _, m := range msgs {
		out[m.ChannelID] = append(out[m.ChannelID], m)
	}
	return out
}

func applyBatch(tx *gorm.DB, channelID, parentChannelID string, msgs []IncomingMessage, stats *IngestStats) error {
	now := NowISO()
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.MessageID)
		if stats.OldestTS == "" || m.TS < stats.OldestTS {
			stats.OldestTS = m.TS
		}
		if stats.NewestTS == "" || m.TS > stats.NewestTS {
			stats.NewestTS = m.TS
		}
	}

	var existingRows []Message
	if err := tx.Where("channel_id = ? AND message_id IN ?", channelID, ids).Find(&existingRows).Error; err != nil {
		return fmt.Errorf("bulk lookup for channel %s: %w", channelID, err)
	}
	existing := make(map[string]Message, len(existingRows))
	for _, e := range existingRows {
		existing[e.MessageID] = e
	}

	var inserts []Message
	var updates []Message
	for _, m := range msgs {
		urlsJSON := ""
		if len(m.URLs) > 0 {
			b, _ := json.Marshal(m.URLs)
			urlsJSON = string(b)
		}
		if e, ok := existing[m.MessageID]; ok {
			contentSame := e.Content == m.Content
			// Backfill thread_id onto marker rows ingested before
			// thread-anchor capture landed. The marker content never
			// changes, so without this the row would stay a "duplicate"
			// forever and never gain its thread_id on re-sync.
			threadIDNew := m.ThreadID != "" && e.ThreadID != m.ThreadID
			if contentSame && !threadIDNew {
				stats.Duplicates++
				continue
			}
			if !contentSame {
				e.Content = m.Content
				e.URLs = urlsJSON
				if m.EditedTS != "" {
					e.LastEditedAt = m.EditedTS
				} else {
					e.LastEditedAt = now
				}
			}
			if threadIDNew {
				e.ThreadID = m.ThreadID
			}
			updates = append(updates, e)
		} else {
			inserts = append(inserts, Message{
				ChannelID:       m.ChannelID,
				MessageID:       m.MessageID,
				TS:              m.TS,
				Author:          m.Author,
				Content:         m.Content,
				URLs:            urlsJSON,
				ReplyTo:         m.ReplyTo,
				FirstSeenAt:     now,
				ParentChannelID: parentChannelID,
				ThreadID:        m.ThreadID,
			})
		}
	}

	if len(inserts) > 0 {
		if err := tx.CreateInBatches(inserts, 200).Error; err != nil {
			return fmt.Errorf("bulk insert for channel %s: %w", channelID, err)
		}
		stats.Inserted += len(inserts)
	}
	for _, row := range updates {
		if err := tx.Save(&row).Error; err != nil {
			return fmt.Errorf("update for channel %s message %s: %w", channelID, row.MessageID, err)
		}
		stats.Edits++
	}
	return nil
}
