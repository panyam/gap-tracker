package disctrack

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestThreadSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Webhook Verification", "webhook-verification"},
		{"@panyam How are you modeling multiple", "panyam-how-are-you-modeling-multiple"},
		{"SEP-3118: Elicitations & UI!!", "sep-3118-elicitations-ui"},
		{"alice\n started a thread: Gateway friendliness. See all threads.", "gateway-friendliness"},
		{"Gateway friendliness. See all threads", "gateway-friendliness"},
		{"Café résumé naïve", "caf-r-sum-na-ve"},
		{"🎉🎉", "untitled"},
		{"", "untitled"},
		// Long titles cut at a word boundary, within the cap.
		{"Interactive and state CSS variables for hosts that render apps inline", "interactive-and-state-css-variables-for"},
		// One long word cannot cut at a boundary; hard cut.
		{strings.Repeat("a", 60), strings.Repeat("a", 40)},
	}
	for _, c := range cases {
		got := ThreadSlug(c.in)
		if got != c.want {
			t.Errorf("ThreadSlug(%q) = %q, want %q", c.in, got, c.want)
		}
		if len(got) > threadSlugMax {
			t.Errorf("ThreadSlug(%q) too long: %d", c.in, len(got))
		}
	}
}

func TestThreadManifestName_Collision(t *testing.T) {
	taken := map[string]bool{}
	if got := ThreadManifestName("wg", "Charter", "1234567890", taken); got != "wg-charter-thread" {
		t.Errorf("free name: %q", got)
	}
	taken["wg-charter-thread"] = true
	if got := ThreadManifestName("wg", "Charter", "1234567890", taken); got != "wg-charter-7890-thread" {
		t.Errorf("first collision: %q", got)
	}
	taken["wg-charter-7890-thread"] = true
	if got := ThreadManifestName("wg", "Charter", "1234567890", taken); got != "wg-charter-1234567890-thread" {
		t.Errorf("second collision: %q", got)
	}
}

func TestThreadSince(t *testing.T) {
	cases := []struct {
		name, cursor, marker, parentSince, want string
	}{
		{"cursor wins", "2026-09-01T00:00:00Z", "2026-08-01T00:00:00Z", "2026-09-20T00:00:00Z", "2026-09-01T00:00:00Z"},
		{"marker minus margin", "", "2026-08-01T10:00:00.123Z", "", "2026-08-01T09:55:00Z"},
		{"parent since later than marker", "", "2026-08-01T10:00:00Z", "2026-09-20T00:00:00Z", "2026-09-20T00:00:00Z"},
		{"marker later than parent since", "", "2026-09-21T00:00:00Z", "2026-09-20T00:00:00Z", "2026-09-20T23:55:00Z"},
		{"parent since only", "", "", "2026-09-20T00:00:00Z", "2026-09-20T00:00:00Z"},
		{"cold start", "", "", "", ""},
		{"garbage marker ignored", "", "yesterday", "", ""},
	}
	for _, c := range cases {
		if got := threadSince(c.cursor, c.marker, c.parentSince); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// fakeScraper records the targets it was handed and returns canned results.
type fakeScraper struct {
	got     []ScrapeTarget
	results map[string]*TargetScrape
	err     error
}

func (f *fakeScraper) Scrape(targets []ScrapeTarget) (map[string]*TargetScrape, error) {
	f.got = append(f.got, targets...)
	return f.results, f.err
}

func msg(channel, id, ts string) IncomingMessage {
	return IncomingMessage{ChannelID: channel, MessageID: id, TS: ts, Author: "a", Content: "c" + id}
}

// threadFixture: parent "wg" (100) with
//   - tracked current-schema thread wg-old-thread (200), cursor set
//   - tracked legacy-schema thread legacy-thread (201), no cursor
//   - dormant thread wg-dormant-thread (202)
//   - markers: one for untracked thread 300 ("Webhook Verification"), one
//     for tracked 200, one unresolved (no thread id), one for 999 which is
//     already tracked as a text channel elsewhere
func threadFixture(t *testing.T) (*Resolved, *gorm.DB) {
	t.Helper()
	r, channels := newTestRoot(t)
	writeManifest(t, channels, "wg", "name: wg\nchannel_id: \"100\"\n")
	writeManifest(t, channels, "wg-old-thread", "name: wg-old-thread\nchannel_id: \"200\"\nkind: thread\nparent_channel: wg\nstatus: active\n")
	writeManifest(t, channels, "legacy-thread", "name: legacy-thread\nchannel_id: \"201\"\nparent:\n  channel_id: \"100\"\n  channel_name: wg\n")
	writeManifest(t, channels, "wg-dormant-thread", "name: wg-dormant-thread\nchannel_id: \"202\"\nkind: thread\nparent_channel: wg\nstatus: dormant\n")
	writeManifest(t, channels, "misfiled", "name: misfiled\nchannel_id: \"999\"\n")
	// A name the new thread's slug would collide with.
	writeManifest(t, channels, "wg-webhook-verification-thread", "name: wg-webhook-verification-thread\nchannel_id: \"888\"\n")

	db, err := r.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	rows := []Message{
		{ChannelID: "100", MessageID: "1001", TS: "2026-09-10T10:00:00.000Z", Author: "a",
			Content: "Casey\n started a thread: Webhook Verification. See all threads.", FirstSeenAt: "x", ThreadID: "300"},
		{ChannelID: "100", MessageID: "1002", TS: "2026-09-01T00:00:00Z", Author: "b",
			Content: "Bob\n started a thread: Old. See all threads.", FirstSeenAt: "x", ThreadID: "200"},
		{ChannelID: "100", MessageID: "1003", TS: "2026-09-02T00:00:00Z", Author: "c",
			Content: "Carol\n started a thread: Mystery. See all threads.", FirstSeenAt: "x"},
		{ChannelID: "100", MessageID: "1004", TS: "2026-09-03T00:00:00Z", Author: "d",
			Content: "Dan\n started a thread: Misfiled. See all threads.", FirstSeenAt: "x", ThreadID: "999"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := SetDiscordCursor(db, "200", "wg-old-thread", "2026-09-15T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	return r, db
}

func TestSyncThreads_AutoTrackAndIsolation(t *testing.T) {
	r, db := threadFixture(t)
	scraper := &fakeScraper{results: map[string]*TargetScrape{
		// New thread: clean.
		"300": {Messages: []IncomingMessage{msg("300", "1", "2026-09-10T10:01:00Z"), msg("300", "2", "2026-09-11T00:00:00Z")}},
		// Existing thread: scraper failed part way. Messages kept, cursor held.
		"200": {Messages: []IncomingMessage{msg("200", "3", "2026-09-16T00:00:00Z")}, Err: errors.New("no messages rendered")},
		// Legacy thread: no result at all (missing from map).
	}}
	now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	rep, err := r.SyncThreads(db, ThreadSyncOptions{Parent: ChannelSpec{Name: "wg", ChannelID: "100"}, Now: now}, scraper)
	if err != nil {
		t.Fatal(err)
	}

	// Auto-track: only 300, with the collision suffix (a 3-digit id is its
	// own last four); 999 is tracked elsewhere; the unresolved marker is
	// counted.
	if len(rep.AutoTracked) != 1 || rep.AutoTracked[0] != "wg-webhook-verification-300-thread" {
		t.Fatalf("auto-tracked: %v", rep.AutoTracked)
	}
	if rep.Unresolved != 1 {
		t.Errorf("unresolved: %d", rep.Unresolved)
	}
	if strings.Join(rep.Skipped, ",") != "wg-dormant-thread" {
		t.Errorf("skipped: %v", rep.Skipped)
	}
	written, _, err := r.LoadChannel("wg-webhook-verification-300-thread")
	if err != nil || written == nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if !written.IsThread() || written.ParentChannel != "wg" || written.ChannelID != "300" ||
		written.DisplayName != "Webhook Verification (thread)" {
		t.Errorf("written manifest: %+v", written)
	}
	raw, _ := os.ReadFile(filepath.Join(r.ChannelsDir(), "wg-webhook-verification-300-thread.yaml"))
	for _, want := range []string{"Thread: Webhook Verification", "Started by Casey at 2026-09-10T10:00:00.000Z", "Auto-tracked on 2026-09-24 by disctrack sync."} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("notes missing %q:\n%s", want, raw)
		}
	}

	// Targets and their since values.
	since := map[string]string{}
	for _, tg := range scraper.got {
		since[tg.ChannelID] = tg.Since
	}
	if len(since) != 3 {
		t.Fatalf("targets: %+v", scraper.got)
	}
	if since["300"] != "2026-09-10T09:55:00Z" {
		t.Errorf("new thread since: %q", since["300"])
	}
	if since["200"] != "2026-09-15T00:00:00Z" {
		t.Errorf("existing thread since (cursor): %q", since["200"])
	}
	if since["201"] != "" {
		t.Errorf("legacy thread with no cursor or marker: %q", since["201"])
	}

	byName := map[string]ThreadSyncResult{}
	for _, th := range rep.Threads {
		byName[th.Channel] = th
	}
	if th := byName["wg-webhook-verification-300-thread"]; th.Status != "ok" || th.Inserted != 2 || !th.AutoTracked {
		t.Errorf("new thread result: %+v", th)
	}
	if th := byName["wg-old-thread"]; th.Status != "failed" || th.Inserted != 1 || !strings.Contains(th.Error, "no messages rendered") {
		t.Errorf("failed thread result: %+v", th)
	}
	if th := byName["legacy-thread"]; th.Status != "failed" || th.Error == "" {
		t.Errorf("missing-result thread: %+v", th)
	}

	// Cursors: advanced for the clean one, held for the failed one.
	c, _ := GetDiscordCursor(db, "300", "")
	if c.CursorValue != "2026-09-11T00:00:00Z" {
		t.Errorf("new thread cursor: %+v", c)
	}
	c, _ = GetDiscordCursor(db, "200", "")
	if c.CursorValue != "2026-09-15T00:00:00Z" {
		t.Errorf("failed thread cursor moved: %+v", c)
	}
	// Failed thread's messages were still ingested, with parent denormalized.
	var got Message
	if err := db.Where("channel_id = ? AND message_id = ?", "200", "3").First(&got).Error; err != nil {
		t.Fatalf("partial messages not ingested: %v", err)
	}
	if got.ParentChannelID != "100" {
		t.Errorf("parent_channel_id = %q", got.ParentChannelID)
	}

	tot := rep.Totals("wg", false)
	if tot.Threads != 3 || tot.OK != 1 || tot.Failed != 2 || tot.New != 3 || tot.AutoTracked != 1 || tot.Unresolved != 1 || tot.Skipped != 1 {
		t.Errorf("totals: %+v", tot)
	}

	// A second run finds nothing new to track and does not duplicate.
	scraper2 := &fakeScraper{results: map[string]*TargetScrape{}}
	rep2, err := r.SyncThreads(db, ThreadSyncOptions{Parent: ChannelSpec{Name: "wg", ChannelID: "100"}, Now: now}, scraper2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep2.AutoTracked) != 0 {
		t.Errorf("re-run auto-tracked again: %v", rep2.AutoTracked)
	}
	for _, tg := range scraper2.got {
		if tg.ChannelID == "300" && tg.Since != "2026-09-11T00:00:00Z" {
			t.Errorf("re-run should resume from cursor, got %q", tg.Since)
		}
	}
}

func TestSyncThreads_BatchError(t *testing.T) {
	r, db := threadFixture(t)
	scraper := &fakeScraper{err: errors.New("start snapshot.py: exec: uv not found")}
	rep, err := r.SyncThreads(db, ThreadSyncOptions{Parent: ChannelSpec{Name: "wg", ChannelID: "100"}}, scraper)
	if err != nil {
		t.Fatalf("batch failure must be per-thread, not a phase error: %v", err)
	}
	if len(rep.Threads) != 3 {
		t.Fatalf("threads: %+v", rep.Threads)
	}
	for _, th := range rep.Threads {
		if th.Status != "failed" || !strings.Contains(th.Error, "uv not found") {
			t.Errorf("%s: %+v", th.Channel, th)
		}
	}
}

func TestSyncThreads_DryRunWritesNothing(t *testing.T) {
	r, db := threadFixture(t)
	scraper := &fakeScraper{results: map[string]*TargetScrape{
		"300": {Messages: []IncomingMessage{msg("300", "1", "2026-09-10T10:01:00Z")}},
	}}
	rep, err := r.SyncThreads(db, ThreadSyncOptions{Parent: ChannelSpec{Name: "wg", ChannelID: "100"}, DryRun: true}, scraper)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.AutoTracked) != 1 {
		t.Fatalf("dry run should still plan the new thread: %v", rep.AutoTracked)
	}
	if _, err := os.Stat(filepath.Join(r.ChannelsDir(), rep.AutoTracked[0]+".yaml")); !os.IsNotExist(err) {
		t.Errorf("dry run wrote a manifest")
	}
	var n int64
	db.Model(&Message{}).Where("channel_id = ?", "300").Count(&n)
	if n != 0 {
		t.Errorf("dry run ingested %d rows", n)
	}
	if c, _ := GetDiscordCursor(db, "300", ""); c.Found {
		t.Errorf("dry run set a cursor")
	}
}

func TestSyncThreads_NoThreads(t *testing.T) {
	r, channels := newTestRoot(t)
	writeManifest(t, channels, "quiet", "name: quiet\nchannel_id: \"100\"\n")
	db, err := r.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	scraper := &fakeScraper{}
	rep, err := r.SyncThreads(db, ThreadSyncOptions{Parent: ChannelSpec{Name: "quiet", ChannelID: "100"}}, scraper)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Threads) != 0 || scraper.got != nil {
		t.Errorf("a channel with no threads should not scrape: %+v %+v", rep, scraper.got)
	}
}
