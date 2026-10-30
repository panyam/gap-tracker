package socialtrack

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// redditFeedURL is a subreddit's newest-posts Atom feed. The feed answers a
// client that names itself, where the .json endpoints refuse anyone without
// an approved API key (Reddit's Responsible Builder Policy, November 2025).
const redditFeedURL = "https://www.reddit.com/r/%s/new/.rss?limit=50"

// redditUserFeedURL is the feed of posts a user submitted, newest first.
const redditUserFeedURL = "https://www.reddit.com/user/%s/submitted/.rss?limit=25"

var linkSub = regexp.MustCompile(`/r/([^/]+)/comments/`)

// FetchUser reads the posts a Reddit user submitted, wherever they posted them.
func FetchUser(ctx context.Context, client *http.Client, userAgent, user string) ([]Post, time.Duration, error) {
	body, wait, err := getFeed(ctx, client, userAgent, fmt.Sprintf(redditUserFeedURL, user), "u/"+user)
	if err != nil {
		return nil, wait, err
	}
	defer body.Close()
	posts, err := ParseRedditFeed(body, "")
	return posts, wait, err
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string `xml:"id"`
	Title     string `xml:"title"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Link struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Content string `xml:"content"`
}

// ErrRateLimited is a 429 from Reddit. Wait says how long until the window resets.
type ErrRateLimited struct {
	Sub  string
	Wait time.Duration
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("r/%s: rate limited, window resets in %s", e.Sub, e.Wait.Round(time.Second))
}

// FetchSubreddit reads one subreddit's newest posts. It also returns how long
// to wait before the next request, read off Reddit's x-ratelimit headers: the
// feeds allow about one request a minute to a client without an API key, and
// a client that ignores the window gets 429 on every later request.
func FetchSubreddit(ctx context.Context, client *http.Client, userAgent, sub string) ([]Post, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(redditFeedURL, sub), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	wait := rateWait(resp.Header)
	if resp.StatusCode == http.StatusTooManyRequests {
		if wait == 0 {
			wait = time.Minute
		}
		return nil, wait, &ErrRateLimited{Sub: sub, Wait: wait}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, wait, fmt.Errorf("r/%s: HTTP %d", sub, resp.StatusCode)
	}
	posts, err := ParseRedditFeed(resp.Body, sub)
	return posts, wait, err
}

// Comment is one reply in a thread's feed.
type Comment struct {
	Author string
	Body   string
	When   time.Time
}

// Thread is a post with the comments its feed carries. Reddit caps the feed,
// so a long thread is cut short; Truncated cannot be known from the feed and
// is left to the reader to judge from the count.
type Thread struct {
	Title    string
	Author   string
	Body     string
	When     time.Time
	Comments []Comment

	// Images are the picture links in the post: review requests are mostly
	// schematic screenshots, which the reader opens and looks at themselves.
	Images []string
}

var imageLink = regexp.MustCompile(`https://(?:i\.redd\.it|preview\.redd\.it|i\.imgur\.com|imgur\.com|(?:www\.)?reddit\.com/gallery)/[^"'<>\s]+`)

// ImageLinks lists the distinct picture links in a post's HTML, in order.
func ImageLinks(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range imageLink.FindAllString(html.UnescapeString(html.UnescapeString(content)), -1) {
		m = strings.TrimRight(m, ".,)")
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// FetchThread reads a thread's own feed: the post (a t3_ entry) and one t1_
// entry per comment, oldest first as Reddit sends them.
func FetchThread(ctx context.Context, client *http.Client, userAgent, postURL string) (Thread, time.Duration, error) {
	body, wait, err := getFeed(ctx, client, userAgent, strings.TrimSuffix(postURL, "/")+"/.rss", postURL)
	if err != nil {
		return Thread{}, wait, err
	}
	defer body.Close()
	th, err := ParseThread(body)
	return th, wait, err
}

// ParseThread turns a thread's Atom feed into the post and its comments.
func ParseThread(r io.Reader) (Thread, error) {
	var feed atomFeed
	if err := xml.NewDecoder(r).Decode(&feed); err != nil {
		return Thread{}, err
	}
	var th Thread
	for _, e := range feed.Entries {
		when, _ := time.Parse(time.RFC3339, firstNonEmpty(e.Published, e.Updated))
		author := strings.TrimPrefix(e.Author.Name, "/u/")
		switch {
		case strings.HasPrefix(e.ID, "t3_"):
			th.Title, th.Author, th.Body, th.When = e.Title, author, plainText(e.Content), when
			th.Images = ImageLinks(e.Content)
		case strings.HasPrefix(e.ID, "t1_"):
			th.Comments = append(th.Comments, Comment{Author: author, Body: plainText(e.Content), When: when})
		}
	}
	return th, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// getFeed GETs a feed with the tracker's user agent and turns Reddit's rate
// headers into a wait; label names the request in errors.
func getFeed(ctx context.Context, client *http.Client, userAgent, url, label string) (io.ReadCloser, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	wait := rateWait(resp.Header)
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		if wait == 0 {
			wait = time.Minute
		}
		return nil, wait, &ErrRateLimited{Sub: label, Wait: wait}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, wait, fmt.Errorf("%s: HTTP %d", label, resp.StatusCode)
	}
	return resp.Body, wait, nil
}

// FetchReplyCount reads a thread's comments feed and counts its comments. The
// feed holds the post (a t3_ entry) followed by one t1_ entry per comment, and
// is capped by Reddit, so a large count means "many" rather than an exact total.
func FetchReplyCount(ctx context.Context, client *http.Client, userAgent, postURL string) (int, time.Duration, error) {
	body, wait, err := getFeed(ctx, client, userAgent, strings.TrimSuffix(postURL, "/")+"/.rss", postURL)
	if err != nil {
		return 0, wait, err
	}
	defer body.Close()
	n, err := CountReplies(body)
	return n, wait, err
}

// CountReplies counts the comment entries in a thread's Atom feed.
func CountReplies(r io.Reader) (int, error) {
	var feed atomFeed
	if err := xml.NewDecoder(r).Decode(&feed); err != nil {
		return 0, err
	}
	n := 0
	for _, e := range feed.Entries {
		if strings.HasPrefix(e.ID, "t1_") {
			n++
		}
	}
	return n, nil
}

// rateWait is zero while the window has requests left, and otherwise the time
// until it resets, plus a second of slack.
func rateWait(h http.Header) time.Duration {
	remaining, err := strconv.ParseFloat(h.Get("x-ratelimit-remaining"), 64)
	if err != nil || remaining >= 1 {
		return 0
	}
	reset, err := strconv.ParseFloat(h.Get("x-ratelimit-reset"), 64)
	if err != nil {
		return time.Minute
	}
	return time.Duration(reset+1) * time.Second
}

// ParseRedditFeed turns a subreddit's Atom feed into posts.
func ParseRedditFeed(r io.Reader, sub string) ([]Post, error) {
	var feed atomFeed
	if err := xml.NewDecoder(r).Decode(&feed); err != nil {
		return nil, fmt.Errorf("r/%s: parse feed: %w", sub, err)
	}
	posts := make([]Post, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		when := e.Published
		if when == "" {
			when = e.Updated
		}
		t, err := time.Parse(time.RFC3339, when)
		if err != nil {
			return nil, fmt.Errorf("r/%s: entry %s: time %q: %w", sub, e.ID, when, err)
		}
		community := "r/" + sub
		if sub == "" {
			// A user's feed spans subreddits; each entry's link names its own.
			community = "reddit"
			if m := linkSub.FindStringSubmatch(e.Link.Href); m != nil {
				community = "r/" + m[1]
			}
		}
		posts = append(posts, Post{
			ID:        "reddit:" + e.ID,
			Source:    "reddit",
			Community: community,
			Title:     e.Title,
			Author:    strings.TrimPrefix(e.Author.Name, "/u/"),
			URL:       e.Link.Href,
			Body:      plainText(e.Content),
			PostedAt:  t.UTC(),
		})
	}
	return posts, nil
}

var tagRE = regexp.MustCompile(`<[^>]*>`)
var spaceRE = regexp.MustCompile(`\s+`)

// plainText strips the feed's HTML body down to text for matching and display.
func plainText(s string) string {
	s = html.UnescapeString(s)
	s = tagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
}
