package socialtrack

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A synthetic feed in Reddit's Atom shape: one review request, one off-topic
// post, one mention of a watch term. Bodies are HTML-escaped as Reddit sends them.
const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>newest submissions : TestSub</title>
  <entry>
    <author><name>/u/alice</name></author>
    <content type="html">&lt;div&gt;&lt;p&gt;First board. Does the I2C bus need a &lt;b&gt;pull-up&lt;/b&gt; on SDA?&lt;/p&gt;&lt;/div&gt;</content>
    <id>t3_aaa111</id>
    <link href="https://www.reddit.com/r/TestSub/comments/aaa111/review/"/>
    <published>2026-10-29T10:00:00+00:00</published>
    <title>[Review Request] ESP32 sensor board</title>
  </entry>
  <entry>
    <author><name>/u/bob</name></author>
    <content type="html">&lt;p&gt;Commercial pricing for small runs?&lt;/p&gt;</content>
    <id>t3_bbb222</id>
    <link href="https://www.reddit.com/r/TestSub/comments/bbb222/pricing/"/>
    <published>2026-10-29T09:00:00+00:00</published>
    <title>Fab house pricing</title>
  </entry>
  <entry>
    <author><name>/u/carol</name></author>
    <content type="html">&lt;p&gt;Has anyone tried agni for schematic checks?&lt;/p&gt;</content>
    <id>t3_ccc333</id>
    <link href="https://www.reddit.com/r/TestSub/comments/ccc333/agni/"/>
    <published>2026-10-27T10:00:00+00:00</published>
    <title>Schematic checking tools</title>
  </entry>
</feed>`

var (
	keywords = []string{"pull-up", "erc", "schematic review"}
	watch    = []string{"agni"}

	// Two topics share TestSub: agni filters it by keyword, music reads all of it.
	agniTopic = Topic{Name: "agni", Subreddits: []Subreddit{{Name: "TestSub", Weight: 2}}, Keywords: keywords, Watch: watch}
	topics    = []Topic{agniTopic}
)

func parsed(t *testing.T) []Post {
	t.Helper()
	posts, err := ParseRedditFeed(strings.NewReader(sampleFeed), "TestSub")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 3 {
		t.Fatalf("parsed %d posts, want 3", len(posts))
	}
	return posts
}

func TestParseRedditFeed(t *testing.T) {
	p := parsed(t)[0]
	if p.ID != "reddit:t3_aaa111" || p.Community != "r/TestSub" || p.Author != "alice" {
		t.Errorf("identity: %+v", p)
	}
	if p.Body != "First board. Does the I2C bus need a pull-up on SDA?" {
		t.Errorf("body not reduced to text: %q", p.Body)
	}
	if !p.PostedAt.Equal(time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("posted at %v", p.PostedAt)
	}
}

func TestKeepFiltersUnlessTheSubKeepsAll(t *testing.T) {
	posts := parsed(t)
	var kept []string
	for _, p := range posts {
		if names, _ := Keep(p, topics); len(names) > 0 {
			kept = append(kept, p.ID)
		}
	}
	// "erc" must not match "Commercial", so the pricing post is dropped.
	if strings.Join(kept, " ") != "reddit:t3_aaa111 reddit:t3_ccc333" {
		t.Errorf("kept %v", kept)
	}
	all := Topic{Name: "boards", Subreddits: []Subreddit{{Name: "TestSub", All: true}}}
	names, _ := Keep(posts[1], []Topic{agniTopic, all})
	if strings.Join(names, ",") != "boards" {
		t.Errorf("unmatched post kept by %v, want only the all-posts topic", names)
	}
	names, terms := Keep(posts[0], []Topic{agniTopic, all})
	if strings.Join(names, ",") != "agni,boards" || strings.Join(terms, ",") != "pull-up" {
		t.Errorf("shared post: topics %v, terms %v", names, terms)
	}
	other := Topic{Name: "elsewhere", Subreddits: []Subreddit{{Name: "OtherSub", All: true}}, Keywords: keywords}
	if names, _ := Keep(posts[0], []Topic{other}); len(names) != 0 {
		t.Errorf("a topic kept a post from a subreddit it does not read: %v", names)
	}
}

func stored(t *testing.T) []Post {
	t.Helper()
	posts := parsed(t)
	for i := range posts {
		posts[i].Status = StatusNew
		posts[i].Replies = -1
		names, terms := Keep(posts[i], []Topic{agniTopic, {Name: "boards", Subreddits: []Subreddit{{Name: "TestSub", All: true}}}})
		posts[i].Topics, posts[i].Matched = strings.Join(names, ","), strings.Join(terms, ",")
	}
	return posts
}

func TestRankWeighsImpactAgainstUrgency(t *testing.T) {
	posts := stored(t)
	all := []Topic{agniTopic, {Name: "boards", Subreddits: []Subreddit{{Name: "TestSub", All: true}}}}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

	// Fresh keyword question (impact (1+1)*2*1.5=6, 2h old) leads; the two-day-old
	// watch-term post (impact (1+3)*2=8, quartered) comes next; the unmatched
	// pricing post (impact 1 under "boards") is last.
	got := Rank(posts, all, now, 72*time.Hour, 10)
	if strings.Join(ids(got), " ") != "reddit:t3_aaa111 reddit:t3_ccc333 reddit:t3_bbb222" {
		t.Fatalf("order %v", ids(got))
	}
	if got[0].Impact != 6 {
		t.Errorf("impact %v, want 6", got[0].Impact)
	}

	// Nine replies cut the leader's urgency to a quarter, below the watch post.
	posts[0].Replies = 9
	got = Rank(posts, all, now, 72*time.Hour, 10)
	if got[0].ID != "reddit:t3_ccc333" {
		t.Errorf("a crowded thread still leads: %v", ids(got))
	}

	// Past max age it drops out, and a post already handled never appears.
	posts[0].Status = StatusReplied
	got = Rank(posts, all, now, 24*time.Hour, 10)
	if strings.Join(ids(got), " ") != "reddit:t3_bbb222" {
		t.Errorf("after reply and age cut: %v", ids(got))
	}
}

func TestParseThreadSplitsPostFromComments(t *testing.T) {
	feed := `<feed xmlns="http://www.w3.org/2005/Atom">
  <entry><author><name>/u/alice</name></author><id>t3_aaa111</id><title>Pull-up on SDA?</title>
    <content type="html">&lt;p&gt;Is 4.7k right?&lt;/p&gt;</content><published>2026-10-29T10:00:00+00:00</published></entry>
  <entry><author><name>/u/bob</name></author><id>t1_c1</id><title>/u/bob on Pull-up on SDA?</title>
    <content type="html">&lt;p&gt;Depends on bus capacitance.&lt;/p&gt;</content><updated>2026-10-29T10:05:00+00:00</updated></entry>
</feed>`
	th, err := ParseThread(strings.NewReader(feed))
	if err != nil {
		t.Fatal(err)
	}
	if th.Title != "Pull-up on SDA?" || th.Author != "alice" || th.Body != "Is 4.7k right?" {
		t.Errorf("post: %+v", th)
	}
	if len(th.Comments) != 1 || th.Comments[0].Author != "bob" || th.Comments[0].Body != "Depends on bus capacitance." {
		t.Errorf("comments: %+v", th.Comments)
	}
}

func TestCountRepliesCountsOnlyComments(t *testing.T) {
	feed := `<feed xmlns="http://www.w3.org/2005/Atom">
  <entry><id>t3_aaa111</id><title>the post</title><updated>2026-10-29T10:00:00+00:00</updated></entry>
  <entry><id>t1_c1</id><title>a reply</title><updated>2026-10-29T10:05:00+00:00</updated></entry>
  <entry><id>t1_c2</id><title>another</title><updated>2026-10-29T10:09:00+00:00</updated></entry>
</feed>`
	n, err := CountReplies(strings.NewReader(feed))
	if err != nil || n != 2 {
		t.Errorf("counted %d, %v; want 2", n, err)
	}
}

func ids(r []Ranked) []string {
	var out []string
	for _, p := range r {
		out = append(out, p.ID)
	}
	return out
}

func TestStreakCountsDaysThatMetTheGoal(t *testing.T) {
	loc := time.UTC
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, loc) }
	replied := []time.Time{day(26, 9), day(27, 9), day(27, 15), day(28, 20)}
	now := day(29, 8)

	// Today has none yet, so the streak counts back from yesterday: 28, 27, 26.
	s := ComputeStreak(replied, 1, now, loc)
	if s.Today != 0 || s.Days != 3 {
		t.Errorf("goal 1: today %d, days %d", s.Today, s.Days)
	}
	// With a goal of two, only the 27th met it and the 28th breaks the run.
	if s := ComputeStreak(replied, 2, now, loc); s.Days != 0 {
		t.Errorf("goal 2: days %d", s.Days)
	}
	// A reply today extends the run to four.
	if s := ComputeStreak(append(replied, day(29, 7)), 1, now, loc); s.Today != 1 || s.Days != 4 {
		t.Errorf("with today: today %d, days %d", s.Today, s.Days)
	}
}

func TestStoreKeepsStatusAcrossSyncs(t *testing.T) {
	root := t.TempDir()
	cfg := "reddit:\n  user_agent: test\ntopics:\n  - name: agni\n    subreddits:\n      - name: TestSub\n    keywords: [pull-up]\n"
	if err := os.WriteFile(filepath.Join(root, ConfigFile), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := ResolveFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	db, err := r.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	posts := parsed(t)
	if n, err := Upsert(db, posts, now); err != nil || n != 3 {
		t.Fatalf("first upsert: %d, %v", n, err)
	}
	if err := Mark(db, posts[0].ID, StatusReplied, "answered the pull-up question", now); err != nil {
		t.Fatal(err)
	}
	if n, err := Upsert(db, posts, now.Add(time.Hour)); err != nil || n != 0 {
		t.Fatalf("second upsert added %d, %v", n, err)
	}
	var got Post
	db.First(&got, "id = ?", posts[0].ID)
	if got.Status != StatusReplied || got.Note == "" {
		t.Errorf("status lost on resync: %+v", got)
	}
	db.First(&got, "id = ?", posts[1].ID)
	if got.Replies != -1 {
		t.Errorf("a new post's reply count is %d, want -1 (unchecked)", got.Replies)
	}
	if err := Mark(db, "reddit:t3_nope", StatusSkipped, "", now); err == nil {
		t.Error("marking an unknown id succeeded")
	}
}

func TestConfigRefusesUnknownKeysAndNoTopics(t *testing.T) {
	for _, tc := range []struct{ cfg, want string }{
		{"reddit:\n  user_agent: test\ntopics:\n  - name: agni\n    subredits: []\n", "subredits"},
		{"reddit:\n  user_agent: test\n", "no topics"},
		{"reddit:\n  user_agent: test\ntopics:\n  - name: a,b\n", "without commas"},
	} {
		root := t.TempDir()
		os.WriteFile(filepath.Join(root, ConfigFile), []byte(tc.cfg), 0o644)
		if _, err := Load(root); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("config %q: err %v, want one naming %q", tc.cfg, err, tc.want)
		}
	}
}

func TestRateWaitReadsRedditsWindow(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-remaining", "0.0")
	h.Set("x-ratelimit-reset", "56")
	if got := rateWait(h); got != 57*time.Second {
		t.Errorf("exhausted window: wait %v, want 57s", got)
	}
	h.Set("x-ratelimit-remaining", "4.0")
	if got := rateWait(h); got != 0 {
		t.Errorf("window with requests left: wait %v, want 0", got)
	}
	if got := rateWait(http.Header{}); got != 0 {
		t.Errorf("no headers: wait %v, want 0", got)
	}
}

func TestParseLinkRecognisesEachNetwork(t *testing.T) {
	for _, tc := range []struct{ in, source, id, community string }{
		{"https://www.reddit.com/r/KiCad/comments/1xj1bde/some_title/", "reddit", "reddit:t3_1xj1bde", "r/KiCad"},
		{"https://old.reddit.com/r/KiCad/comments/1xj1bde/", "reddit", "reddit:t3_1xj1bde", "r/KiCad"},
		{"https://x.com/someone/status/1850000000000000000?s=20", "x", "x:1850000000000000000", "@someone"},
		{"https://twitter.com/someone/status/1850000000000000000", "x", "x:1850000000000000000", "@someone"},
		{"https://news.ycombinator.com/item?id=41000000", "hn", "hn:41000000", "hn"},
	} {
		l, err := ParseLink(tc.in)
		if err != nil || l.Source != tc.source || l.ID != tc.id || l.Community != tc.community {
			t.Errorf("%s: %+v, %v", tc.in, l, err)
		}
	}
	a, _ := ParseLink("https://www.linkedin.com/posts/someone_activity-123/")
	b, _ := ParseLink("https://www.linkedin.com/posts/someone_activity-123")
	if a.Source != "linkedin" || a.ID != b.ID || !strings.HasPrefix(a.ID, "linkedin:") {
		t.Errorf("linkedin ids %q and %q should be one hashed id", a.ID, b.ID)
	}
	for _, bad := range []string{"not a url", "https://www.reddit.com/r/KiCad/", "https://x.com/someone", "ftp://example.com/x"} {
		if _, err := ParseLink(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
