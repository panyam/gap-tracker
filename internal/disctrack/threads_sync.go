package disctrack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

// threadSinceMargin is how far before a thread's start marker a first sync
// of that thread begins, so the thread's opening messages (posted in the
// seconds around the marker) are not cut off by clock skew.
const threadSinceMargin = 5 * time.Minute

// threadSlugMax caps the slug portion of an auto-tracked thread's name.
const threadSlugMax = 40

// ScrapeTarget is one channel in a batched scrape.
type ScrapeTarget struct {
	ChannelID string
	Since     string // "" = cold start
}

// TargetScrape is what the scraper produced for one target.
type TargetScrape struct {
	Messages    []IncomingMessage
	ParseErrors int
	// Err is set when the scraper reported this target failed, or ended
	// before finishing it. Messages seen before the failure are still
	// ingested; only the cursor stays put.
	Err error
}

// BatchScraper scrapes several channels in one go and returns each target's
// records keyed by channel_id. An error return means the batch as a whole
// failed; per-target failures go in TargetScrape.Err.
type BatchScraper interface {
	Scrape(targets []ScrapeTarget) (map[string]*TargetScrape, error)
}

// ThreadSyncOptions controls the thread phase that runs after a parent sync.
type ThreadSyncOptions struct {
	// Parent is the parent channel just synced. Name is required: it is the
	// prefix for auto-tracked thread names and their parent_channel.
	Parent ChannelSpec
	// ParentSince is the parent sync's --since override (ISO), or "".
	ParentSince string
	DryRun      bool
	Now         time.Time
}

// ThreadSyncResult is one thread's line in the sync summary.
type ThreadSyncResult struct {
	Channel string `json:"channel"`
	SyncResult
	Parent      string `json:"parent"`
	AutoTracked bool   `json:"auto_tracked"`
	Since       string `json:"since"`
	Status      string `json:"status"` // "ok" or "failed"
	Error       string `json:"error,omitempty"`
}

// ThreadSyncReport is the whole thread phase.
type ThreadSyncReport struct {
	Threads []ThreadSyncResult `json:"threads"`
	// AutoTracked names the manifests written this run (or that would be,
	// under --dry-run).
	AutoTracked []string `json:"auto_tracked"`
	// Unresolved counts thread markers whose thread id snapshot.py could not
	// capture. Those threads cannot be tracked until a later parent sync
	// resolves the id.
	Unresolved int `json:"unresolved_markers"`
	// Skipped names tracked threads left out because their status is not
	// active (dormant, parked, resolved).
	Skipped []string `json:"skipped"`
}

// ThreadSyncTotals is the final summary line.
type ThreadSyncTotals struct {
	Parent      string `json:"parent"`
	Threads     int    `json:"threads"`
	OK          int    `json:"ok"`
	Failed      int    `json:"failed"`
	New         int    `json:"new"`
	AutoTracked int    `json:"auto_tracked"`
	Unresolved  int    `json:"unresolved_markers"`
	Skipped     int    `json:"skipped"`
	DryRun      bool   `json:"dry_run"`
}

// Totals folds the report into one summary line.
func (rep ThreadSyncReport) Totals(parent string, dryRun bool) ThreadSyncTotals {
	t := ThreadSyncTotals{
		Parent:      parent,
		Threads:     len(rep.Threads),
		AutoTracked: len(rep.AutoTracked),
		Unresolved:  rep.Unresolved,
		Skipped:     len(rep.Skipped),
		DryRun:      dryRun,
	}
	for _, th := range rep.Threads {
		if th.Status == "ok" {
			t.OK++
		} else {
			t.Failed++
		}
		t.New += th.Inserted
	}
	return t
}

// AutoTrackPlan is one thread manifest the thread phase will write.
type AutoTrackPlan struct {
	Name         string
	ChannelID    string
	Title        string
	MarkerTS     string
	ManifestPath string
	Manifest     string
}

// PlanAutoTrack picks the discovered threads that have no manifest anywhere
// and renders a manifest for each. Markers without a captured thread id are
// counted, not planned. A thread id already tracked under any parent (or as
// a text channel) is left alone, so a hand-written manifest never gets a
// duplicate.
func (r *Resolved) PlanAutoTrack(parentName string, discovered []DiscoveredThread, now time.Time) (plans []AutoTrackPlan, unresolved int) {
	taken := map[string]bool{}
	trackedIDs := map[string]bool{}
	for _, name := range r.ListChannels() {
		taken[name] = true
		if m, _, err := r.LoadChannel(name); err == nil && m != nil && m.ChannelID != "" {
			trackedIDs[m.ChannelID] = true
		}
	}

	for _, d := range discovered {
		if d.Tracked {
			continue
		}
		if !d.IDKnown {
			unresolved++
			continue
		}
		if trackedIDs[d.ThreadID] {
			continue
		}
		trackedIDs[d.ThreadID] = true

		name := ThreadManifestName(parentName, d.Title, d.ThreadID, taken)
		taken[name] = true

		title := d.Title
		if title == "" {
			title = "(untitled)"
		}
		notes := fmt.Sprintf("Thread: %s\n", title)
		if d.StartedBy != "" {
			notes += fmt.Sprintf("Started by %s at %s (marker %s in %s).\n", d.StartedBy, d.TS, d.MarkerMessageID, parentName)
		} else {
			notes += fmt.Sprintf("Started at %s (marker %s in %s).\n", d.TS, d.MarkerMessageID, parentName)
		}
		notes += fmt.Sprintf("Auto-tracked on %s by disctrack sync.", now.Format("2006-01-02"))

		plans = append(plans, AutoTrackPlan{
			Name:         name,
			ChannelID:    d.ThreadID,
			Title:        d.Title,
			MarkerTS:     d.TS,
			ManifestPath: filepath.Join(r.ChannelsDir(), name+".yaml"),
			Manifest: renderManifest(manifestFields{
				Name:          name,
				ChannelID:     d.ThreadID,
				DisplayName:   title + " (thread)",
				Kind:          ChannelKindThread,
				ParentChannel: parentName,
				Tags:          []string{"thread"},
				Notes:         notes,
			}),
		})
	}
	return plans, unresolved
}

// ThreadManifestName is <parent>-<slug>-thread. On a collision with a name
// in taken it inserts the last four digits of the thread id, then the whole
// id.
func ThreadManifestName(parent, title, threadID string, taken map[string]bool) string {
	slug := ThreadSlug(title)
	name := parent + "-" + slug + "-thread"
	if !taken[name] {
		return name
	}
	short := threadID
	if len(short) > 4 {
		short = short[len(short)-4:]
	}
	name = parent + "-" + slug + "-" + short + "-thread"
	if !taken[name] {
		return name
	}
	return parent + "-" + slug + "-" + threadID + "-thread"
}

// ThreadSlug turns a thread title into a filename-safe slug: lowercase ascii
// letters and digits joined by single hyphens, at most threadSlugMax
// characters, cut at a word boundary where possible. Marker boilerplate
// ("X started a thread:", "See all threads") is stripped if the caller
// passes raw marker text. Returns "untitled" when nothing survives.
func ThreadSlug(title string) string {
	if strings.Contains(title, threadMarkerNeedle) {
		_, title = parseThreadMarker(title)
	}
	if i := strings.Index(strings.ToLower(title), "see all thread"); i >= 0 {
		title = title[:i]
	}
	var b strings.Builder
	hyphen := false
	for _, c := range strings.ToLower(title) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
			hyphen = false
		case b.Len() > 0 && !hyphen:
			b.WriteByte('-')
			hyphen = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > threadSlugMax {
		cut := slug[:threadSlugMax]
		if i := strings.LastIndexByte(cut, '-'); i >= threadSlugMax/2 {
			cut = cut[:i]
		}
		slug = strings.Trim(cut, "-")
	}
	if slug == "" {
		return "untitled"
	}
	return slug
}

// syncableStatus reports whether a tracked thread takes part in the
// automatic thread phase. Threads marked dormant, parked or resolved are
// still syncable directly by name.
func syncableStatus(status string) bool {
	return status == "" || status == "active"
}

// threadTarget is one thread the phase will sync, with the cursor state
// needed after the scrape.
type threadTarget struct {
	manifest      ChannelManifest
	autoTracked   bool
	since         string
	currentCursor string
}

// SyncThreads is the thread phase of `disctrack sync <parent>`. It tracks
// any thread whose start marker is in the parent but has no manifest, then
// syncs every active thread of the parent in one scraper batch. One thread
// failing never fails another: each gets its own ingest transaction, its own
// cursor, and its own line in the report. The returned error is reserved for
// failures that stop the phase before any thread is attempted.
func (r *Resolved) SyncThreads(db *gorm.DB, opts ThreadSyncOptions, scraper BatchScraper) (ThreadSyncReport, error) {
	rep := ThreadSyncReport{AutoTracked: []string{}, Skipped: []string{}, Threads: []ThreadSyncResult{}}
	parent := opts.Parent
	if parent.Name == "" {
		return rep, errors.New("thread sync needs the parent's manifest name; sync it by name rather than raw id")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	discovered, err := r.DiscoverThreads(db, parent.Name)
	if err != nil {
		return rep, fmt.Errorf("discover threads: %w", err)
	}
	markerTS := map[string]string{}
	for _, d := range discovered {
		if d.IDKnown {
			markerTS[d.ThreadID] = d.TS
		}
	}

	plans, unresolved := r.PlanAutoTrack(parent.Name, discovered, now)
	rep.Unresolved = unresolved
	var targets []threadTarget
	for _, p := range plans {
		if !opts.DryRun {
			if err := writeNewFile(p.ManifestPath, p.Manifest); err != nil {
				rep.Threads = append(rep.Threads, ThreadSyncResult{
					Channel: p.Name, SyncResult: SyncResult{ChannelID: p.ChannelID},
					Parent: parent.Name, AutoTracked: true, Status: "failed",
					Error: "write manifest: " + err.Error(),
				})
				continue
			}
			fmt.Fprintf(os.Stderr, "auto-tracked thread %q -> %s\n", p.Title, p.ManifestPath)
		} else {
			fmt.Fprintf(os.Stderr, "dry-run: would auto-track thread %q -> %s\n", p.Title, p.ManifestPath)
		}
		rep.AutoTracked = append(rep.AutoTracked, p.Name)
		targets = append(targets, threadTarget{
			manifest: ChannelManifest{
				Name: p.Name, ChannelID: p.ChannelID, Kind: ChannelKindThread,
				ParentChannel: parent.Name, Status: "active",
			},
			autoTracked: true,
		})
	}

	existing, err := r.ListThreads(parent.Name)
	if err != nil {
		return rep, fmt.Errorf("list threads: %w", err)
	}
	planned := map[string]bool{}
	for _, t := range targets {
		planned[t.manifest.ChannelID] = true
	}
	for _, m := range existing {
		if m.ChannelID == "" || planned[m.ChannelID] {
			continue
		}
		if !syncableStatus(m.Status) {
			rep.Skipped = append(rep.Skipped, m.Name)
			continue
		}
		planned[m.ChannelID] = true
		targets = append(targets, threadTarget{manifest: m})
	}

	if len(targets) == 0 {
		return rep, nil
	}

	for i := range targets {
		t := &targets[i]
		cur, err := GetDiscordCursor(db, t.manifest.ChannelID, t.manifest.Name)
		if err != nil {
			return rep, fmt.Errorf("read cursor for %s: %w", t.manifest.Name, err)
		}
		t.currentCursor = cur.CursorValue
		t.since = threadSince(cur.CursorValue, markerTS[t.manifest.ChannelID], opts.ParentSince)
	}

	scrapeTargets := make([]ScrapeTarget, len(targets))
	for i, t := range targets {
		scrapeTargets[i] = ScrapeTarget{ChannelID: t.manifest.ChannelID, Since: t.since}
	}
	fmt.Fprintf(os.Stderr, "syncing %d thread(s) of %s in one scraper session\n", len(targets), parent.Name)
	results, batchErr := scraper.Scrape(scrapeTargets)

	for _, t := range targets {
		res := ThreadSyncResult{
			Channel:     t.manifest.Name,
			SyncResult:  SyncResult{ChannelID: t.manifest.ChannelID, DryRun: opts.DryRun},
			Parent:      parent.Name,
			AutoTracked: t.autoTracked,
			Since:       t.since,
			Status:      "ok",
		}
		out := results[t.manifest.ChannelID]
		if out == nil {
			res.Status = "failed"
			res.Error = "scraper produced no result for this thread"
			if batchErr != nil {
				res.Error = batchErr.Error()
			}
			rep.Threads = append(rep.Threads, res)
			continue
		}
		res.SyncResult = r.ingestThread(db, t, out, opts.DryRun, &res)
		rep.Threads = append(rep.Threads, res)
	}
	return rep, nil
}

// ingestThread ingests one target's records and advances its cursor when the
// scrape finished cleanly. Failures land on res rather than propagating.
func (r *Resolved) ingestThread(db *gorm.DB, t threadTarget, out *TargetScrape, dryRun bool, res *ThreadSyncResult) SyncResult {
	sr := SyncResult{ChannelID: t.manifest.ChannelID, DryRun: dryRun}
	var stats IngestStats
	var err error
	if dryRun {
		stats = statsOf(out.Messages)
	} else {
		stats, err = IngestMessages(db, out.Messages, r)
	}
	stats.Errors += out.ParseErrors
	sr.Inserted, sr.Edits, sr.Duplicates, sr.Errors = stats.Inserted, stats.Edits, stats.Duplicates, stats.Errors
	sr.OldestTS, sr.NewestTS = stats.OldestTS, stats.NewestTS

	switch {
	case err != nil:
		res.Status, res.Error = "failed", "ingest: "+err.Error()
	case out.Err != nil:
		// Keep what was ingested, but leave the cursor where it was: the
		// scrape may have stopped short of the window.
		res.Status, res.Error = "failed", out.Err.Error()
	case !dryRun && stats.NewestTS != "" && stats.NewestTS > t.currentCursor:
		if err := SetDiscordCursor(db, t.manifest.ChannelID, t.manifest.Name, stats.NewestTS); err != nil {
			res.Status, res.Error = "failed", "advance cursor: "+err.Error()
		}
	}
	return sr
}

// statsOf counts records without touching the DB (the --dry-run path).
func statsOf(msgs []IncomingMessage) IngestStats {
	var s IngestStats
	for _, m := range msgs {
		s.Inserted++
		if s.OldestTS == "" || m.TS < s.OldestTS {
			s.OldestTS = m.TS
		}
		if s.NewestTS == "" || m.TS > s.NewestTS {
			s.NewestTS = m.TS
		}
	}
	return s
}

// threadSince picks where a thread's scrape starts. A thread with a cursor
// resumes from it. A thread with none starts shortly before its start
// marker, or at the parent's --since if that is later. With neither it is a
// cold start ("").
func threadSince(cursor, markerTS, parentSince string) string {
	if cursor != "" {
		return cursor
	}
	var from time.Time
	if t, ok := parseTS(markerTS); ok {
		from = t.Add(-threadSinceMargin)
	}
	if t, ok := parseTS(parentSince); ok && t.After(from) {
		from = t
	}
	if from.IsZero() {
		return ""
	}
	return from.UTC().Format(time.RFC3339)
}

func parseTS(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
