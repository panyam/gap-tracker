package socialtrack

import (
	"net/url"
	"strings"
)

// xQueryLimit keeps a search under the length X accepts for one query, with
// room for the filter suffix.
const xQueryLimit = 450

// XSearchURLs builds live X searches showing the newest posts from these
// accounts, as few as fit X's query length. Replies are left out unless
// asked for, since a reply is usually half of someone else's thread.
func XSearchURLs(accounts []string, replies bool) []string {
	suffix := " -filter:replies"
	if replies {
		suffix = ""
	}
	var urls []string
	var terms []string
	flush := func() {
		if len(terms) == 0 {
			return
		}
		q := "(" + strings.Join(terms, " OR ") + ")" + suffix
		urls = append(urls, "https://x.com/search?q="+url.QueryEscape(q)+"&f=live")
		terms = nil
	}
	seen := map[string]bool{}
	for _, a := range accounts {
		h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a), "@"))
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		term := "from:" + h
		if len(strings.Join(append(terms, term), " OR "))+len(suffix)+2 > xQueryLimit {
			flush()
		}
		terms = append(terms, term)
	}
	flush()
	return urls
}

// XAccounts gathers the X accounts of the named topic, or of every topic.
func (c Config) XAccounts(topic string) []string {
	var out []string
	for _, t := range c.Topics {
		if topic == "" || t.Name == topic {
			out = append(out, t.X.Accounts...)
		}
	}
	return out
}
