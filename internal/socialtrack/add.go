package socialtrack

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Link is what a thread URL says on its own: which network, the id that
// network gives the thread, and where it was posted.
type Link struct {
	Source    string // reddit, x, hn, linkedin, yc, or the bare host
	ID        string // the store id, source-prefixed
	Community string // r/<sub>, @<handle>, hn, linkedin, ...
	URL       string
}

var (
	redditPath = regexp.MustCompile(`^/r/([^/]+)/comments/([a-z0-9]+)`)
	xPath      = regexp.MustCompile(`^/([^/]+)/status/(\d+)`)
)

// ParseLink recognises a thread URL from the networks socialtrack knows and
// falls back to the host for any other, so a thread from anywhere can be
// added by hand. The id is the network's own where the URL carries one, and
// a hash of the URL otherwise, so adding the same link twice finds the first.
func ParseLink(raw string) (Link, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Link{}, fmt.Errorf("%q is not a web link", raw)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	host = strings.TrimPrefix(host, "old.")
	l := Link{URL: u.String()}
	switch {
	case host == "reddit.com" || strings.HasSuffix(host, ".reddit.com"):
		m := redditPath.FindStringSubmatch(u.Path)
		if m == nil {
			return Link{}, fmt.Errorf("%s is a Reddit link but not a thread (/r/<sub>/comments/<id>)", raw)
		}
		l.Source, l.ID, l.Community = "reddit", "reddit:t3_"+m[2], "r/"+m[1]
		l.URL = fmt.Sprintf("https://www.reddit.com/r/%s/comments/%s/", m[1], m[2])
	case host == "x.com" || host == "twitter.com" || host == "mobile.twitter.com":
		m := xPath.FindStringSubmatch(u.Path)
		if m == nil {
			return Link{}, fmt.Errorf("%s is an X link but not a post (/<handle>/status/<id>)", raw)
		}
		l.Source, l.ID, l.Community = "x", "x:"+m[2], "@"+m[1]
		l.URL = fmt.Sprintf("https://x.com/%s/status/%s", m[1], m[2])
	case host == "news.ycombinator.com":
		id := u.Query().Get("id")
		if id == "" {
			return Link{}, fmt.Errorf("%s is an HN link but not an item (item?id=<n>)", raw)
		}
		l.Source, l.ID, l.Community = "hn", "hn:"+id, "hn"
	case strings.HasSuffix(host, "linkedin.com"):
		l.Source, l.Community = "linkedin", "linkedin"
	case host == "startupschool.org" || strings.HasSuffix(host, ".startupschool.org") || strings.HasSuffix(host, "ycombinator.com"):
		l.Source, l.Community = "yc", "yc"
	default:
		l.Source, l.Community = host, host
	}
	if l.ID == "" {
		sum := sha1.Sum([]byte(strings.TrimSuffix(l.URL, "/")))
		l.ID = l.Source + ":" + hex.EncodeToString(sum[:])[:12]
	}
	return l, nil
}
