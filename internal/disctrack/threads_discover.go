package disctrack

import (
	"sort"
	"strings"

	"gorm.io/gorm"
)

// threadMarkerNeedle is the substring Discord puts in every "X started a
// thread: TITLE. See all threads." system message. Matching on content is the
// only way to find markers — there is no structured message-kind column.
const threadMarkerNeedle = "started a thread:"

// DiscoveredThread is one thread-start marker seen in a parent channel, joined
// against the tracked manifests so the caller knows whether it still needs a
// channel.yaml + sync.
type DiscoveredThread struct {
	// ThreadID is the thread's own channel_id, captured from the marker
	// anchor. Empty when the marker row predates thread-anchor capture; a
	// re-sync of the parent backfills it.
	ThreadID string `json:"thread_id,omitempty" yaml:"thread_id,omitempty"`
	// Title is the thread name as rendered in the marker (Discord truncates
	// long source-message text).
	Title string `json:"title" yaml:"title"`
	// StartedBy is the member who opened the thread, parsed from the marker.
	StartedBy string `json:"started_by,omitempty" yaml:"started_by,omitempty"`
	// TS is the marker timestamp (when the thread was started).
	TS string `json:"ts" yaml:"ts"`
	// MarkerMessageID is the parent-channel message id of the marker itself.
	// Deliberately distinct from ThreadID — they are never equal.
	MarkerMessageID string `json:"marker_message_id" yaml:"marker_message_id"`
	// Tracked is true when a thread manifest already covers this thread.
	Tracked bool `json:"tracked" yaml:"tracked"`
	// Manifest is the tracked manifest's name when Tracked is true.
	Manifest string `json:"manifest,omitempty" yaml:"manifest,omitempty"`
	// IDKnown is false when ThreadID could not be captured (pre-capture
	// row). Tracked status then falls back to a best-effort title match and
	// the caller should re-sync the parent to resolve the id.
	IDKnown bool `json:"id_known" yaml:"id_known"`
}

// ThreadMarkersIn returns the raw thread-start markers in parentChannelID,
// ordered by ts asc. Title / StartedBy are parsed from the marker content;
// ThreadID comes from the captured anchor (may be empty on old rows).
func ThreadMarkersIn(db *gorm.DB, parentChannelID string) ([]DiscoveredThread, error) {
	var rows []Message
	err := db.
		Where("channel_id = ? AND content LIKE ?", parentChannelID, "%"+threadMarkerNeedle+"%").
		Order("ts asc").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]DiscoveredThread, 0, len(rows))
	for _, r := range rows {
		startedBy, title := parseThreadMarker(r.Content)
		out = append(out, DiscoveredThread{
			ThreadID:        r.ThreadID,
			Title:           title,
			StartedBy:       startedBy,
			TS:              r.TS,
			MarkerMessageID: r.MessageID,
			IDKnown:         r.ThreadID != "",
		})
	}
	return out, nil
}

// parseThreadMarker pulls the starter name and thread title out of a marker's
// content, which renders as:
//
//	<starter>\n started a thread: <title>. See all threads.\n — ...
//
// Both halves are best-effort: Discord truncates long titles and the trailing
// metadata varies, so we cut at the stable "See all threads" sentinel and
// fall back gracefully when a piece is missing.
func parseThreadMarker(content string) (startedBy, title string) {
	idx := strings.Index(content, threadMarkerNeedle)
	if idx < 0 {
		return "", strings.TrimSpace(content)
	}
	startedBy = strings.TrimSpace(content[:idx])
	rest := content[idx+len(threadMarkerNeedle):]
	if cut := strings.Index(rest, "See all thread"); cut >= 0 {
		rest = rest[:cut]
	}
	title = strings.TrimSpace(rest)
	title = strings.TrimRight(title, ". ")
	return startedBy, title
}

// DiscoverThreads joins the markers in a parent channel against the tracked
// thread manifests. parent may be a channel name or a numeric channel_id.
// Threads already covered by a manifest are marked Tracked; the rest are the
// work list. Ordered by ts asc.
func (r *Resolved) DiscoverThreads(db *gorm.DB, parent string) ([]DiscoveredThread, error) {
	spec, err := r.ResolveChannelSpec(parent)
	if err != nil {
		return nil, err
	}
	markers, err := ThreadMarkersIn(db, spec.ChannelID)
	if err != nil {
		return nil, err
	}

	tracked, err := r.ListThreads(parent)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]string, len(tracked))         // thread channel_id -> manifest name
	byTitle := make(map[string]string, len(tracked))      // normalized display title -> manifest name
	for _, t := range tracked {
		if t.ChannelID != "" {
			byID[t.ChannelID] = t.Name
		}
		if dn := normalizeTitle(t.DisplayName); dn != "" {
			byTitle[dn] = t.Name
		}
	}

	for i := range markers {
		m := &markers[i]
		if m.IDKnown {
			// Authoritative: match on the captured thread_id.
			if name, ok := byID[m.ThreadID]; ok {
				m.Tracked = true
				m.Manifest = name
			}
			continue
		}
		// Pre-capture row: best-effort title match so a re-synced
		// manifest is still recognized. Re-sync the parent to get the id.
		if name, ok := byTitle[normalizeTitle(m.Title)]; ok {
			m.Tracked = true
			m.Manifest = name
		}
	}
	return markers, nil
}

// normalizeTitle lowercases and collapses a title (or a manifest display_name,
// which carries a trailing "(thread)") so the pre-capture title fallback can
// match across the two renderings.
func normalizeTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, "(thread)")
	s = strings.TrimSpace(s)
	return strings.Join(strings.Fields(s), " ")
}

// SortThreadsByTS keeps marker order stable for output (ts asc, then marker id).
func SortThreadsByTS(ts []DiscoveredThread) {
	sort.SliceStable(ts, func(i, j int) bool {
		if ts[i].TS != ts[j].TS {
			return ts[i].TS < ts[j].TS
		}
		return ts[i].MarkerMessageID < ts[j].MarkerMessageID
	})
}
