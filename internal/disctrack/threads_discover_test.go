package disctrack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseThreadMarker(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantStarter string
		wantTitle   string
	}{
		{
			name:        "typical marker",
			content:     "Casey Chow (OpenAI)\n started a thread: Webhook Verification. See all threads.\n — \n5/29/26",
			wantStarter: "Casey Chow (OpenAI)",
			wantTitle:   "Webhook Verification",
		},
		{
			name:        "title truncated mid phrase",
			content:     "panyam\n started a thread: @panyam How are you modeling multiple. See all threads.",
			wantStarter: "panyam",
			wantTitle:   "@panyam How are you modeling multiple",
		},
		{
			name:        "no see-all-threads tail",
			content:     "Luca (AWS)\n started a thread: Hey folks reminder",
			wantStarter: "Luca (AWS)",
			wantTitle:   "Hey folks reminder",
		},
		{
			name:        "not a marker",
			content:     "just a normal message",
			wantStarter: "",
			wantTitle:   "just a normal message",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotStarter, gotTitle := parseThreadMarker(c.content)
			if gotStarter != c.wantStarter {
				t.Errorf("starter: got %q want %q", gotStarter, c.wantStarter)
			}
			if gotTitle != c.wantTitle {
				t.Errorf("title: got %q want %q", gotTitle, c.wantTitle)
			}
		})
	}
}

func TestNormalizeTitle(t *testing.T) {
	if got := normalizeTitle("Webhook Verification (thread)"); got != "webhook verification" {
		t.Errorf("got %q", got)
	}
	if got := normalizeTitle("  Foo   Bar  "); got != "foo bar" {
		t.Errorf("got %q", got)
	}
}

// writeManifest is a tiny helper for the discover test.
func writeManifest(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverThreads(t *testing.T) {
	root := t.TempDir()
	channels := filepath.Join(root, "channels")
	if err := os.MkdirAll(channels, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Resolved{Root: root, Config: Config{ChannelsDir: "channels", AreasDir: "areas"}}

	writeManifest(t, channels, "parent", "name: parent\nchannel_id: \"100\"\n")
	// One tracked thread (id 200) and a manifest that should be matched by id.
	writeManifest(t, channels, "parent-tracked-thread",
		"name: parent-tracked-thread\nchannel_id: \"200\"\nkind: thread\nparent_channel: parent\ndisplay_name: \"Tracked One (thread)\"\n")

	db, err := r.OpenDB()
	if err != nil {
		t.Fatal(err)
	}

	rows := []Message{
		// tracked, id captured
		{ChannelID: "100", MessageID: "1001", TS: "2026-06-01T00:00:00Z", Author: "a",
			Content: "alice\n started a thread: Tracked One. See all threads.", FirstSeenAt: "x", ThreadID: "200"},
		// untracked, id captured
		{ChannelID: "100", MessageID: "1002", TS: "2026-06-02T00:00:00Z", Author: "b",
			Content: "bob\n started a thread: Untracked Two. See all threads.", FirstSeenAt: "x", ThreadID: "300"},
		// pre-capture row: no thread_id; title matches nothing tracked
		{ChannelID: "100", MessageID: "1003", TS: "2026-06-03T00:00:00Z", Author: "c",
			Content: "carol\n started a thread: Old Three. See all threads.", FirstSeenAt: "x", ThreadID: ""},
		// a plain message in the same channel (must be ignored)
		{ChannelID: "100", MessageID: "1004", TS: "2026-06-04T00:00:00Z", Author: "d",
			Content: "just chatting", FirstSeenAt: "x"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	got, err := r.DiscoverThreads(db, "parent")
	if err != nil {
		t.Fatal(err)
	}
	SortThreadsByTS(got)

	if len(got) != 3 {
		t.Fatalf("want 3 markers, got %d: %+v", len(got), got)
	}

	// 200: tracked by id
	if !got[0].Tracked || got[0].Manifest != "parent-tracked-thread" || got[0].ThreadID != "200" || !got[0].IDKnown {
		t.Errorf("marker 0 wrong: %+v", got[0])
	}
	// 300: untracked, id known
	if got[1].Tracked || got[1].ThreadID != "300" || !got[1].IDKnown {
		t.Errorf("marker 1 wrong: %+v", got[1])
	}
	// pre-capture: id unknown, untracked
	if got[2].Tracked || got[2].IDKnown || got[2].ThreadID != "" {
		t.Errorf("marker 2 wrong: %+v", got[2])
	}
	if got[2].Title != "Old Three" {
		t.Errorf("marker 2 title: %q", got[2].Title)
	}
}

func TestDiscoverThreads_TitleFallbackMatch(t *testing.T) {
	root := t.TempDir()
	channels := filepath.Join(root, "channels")
	if err := os.MkdirAll(channels, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Resolved{Root: root, Config: Config{ChannelsDir: "channels", AreasDir: "areas"}}
	writeManifest(t, channels, "parent", "name: parent\nchannel_id: \"100\"\n")
	// Tracked manifest exists but the marker row predates id capture; match by title.
	writeManifest(t, channels, "parent-vthread",
		"name: parent-vthread\nchannel_id: \"200\"\nkind: thread\nparent_channel: parent\ndisplay_name: \"Verification Thread (thread)\"\n")

	db, err := r.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Message{ChannelID: "100", MessageID: "1001", TS: "2026-06-01T00:00:00Z",
		Author: "a", Content: "alice\n started a thread: Verification Thread. See all threads.",
		FirstSeenAt: "x", ThreadID: ""}).Error; err != nil {
		t.Fatal(err)
	}

	got, err := r.DiscoverThreads(db, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Tracked || got[0].Manifest != "parent-vthread" || got[0].IDKnown {
		t.Fatalf("expected title-fallback tracked match, got %+v", got)
	}
}
