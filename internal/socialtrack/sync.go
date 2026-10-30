package socialtrack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

// SubredditResult is one subreddit's part of a sync.
type SubredditResult struct {
	Name    string
	Fetched int
	Kept    int
	New     int
	Err     error
}

// rateGate spaces requests to Reddit by its own rate window, and by at least
// the configured delay.
type rateGate struct {
	ctx      context.Context
	min      time.Duration
	next     time.Duration
	progress func(string)
	used     bool
}

// wait blocks until the next request may go, and reports false if the run was cancelled.
func (g *rateGate) wait(extra time.Duration) bool {
	d := g.next
	if extra > d {
		d = extra
	}
	if !g.used && extra == 0 {
		g.used = true
		return true
	}
	if d < g.min {
		d = g.min
	}
	if g.progress != nil && d > 5*time.Second {
		g.progress(fmt.Sprintf("waiting %s for Reddit's rate window", d.Round(time.Second)))
	}
	select {
	case <-g.ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// fetch makes one rate-gated request, retrying once after a 429.
func (g *rateGate) fetch(do func() (time.Duration, error)) error {
	if !g.wait(0) {
		return g.ctx.Err()
	}
	next, err := do()
	var limited *ErrRateLimited
	if errors.As(err, &limited) {
		// One retry after the window resets; a second 429 means something else
		// is spending the window, so report it and move on.
		if !g.wait(limited.Wait) {
			return g.ctx.Err()
		}
		next, err = do()
	}
	g.next = next
	return err
}

func (r *Resolved) gate(ctx context.Context, progress func(string)) *rateGate {
	return &rateGate{ctx: ctx, min: time.Duration(r.Config.Reddit.DelaySeconds) * time.Second, progress: progress}
}

// SyncReddit reads every subreddit any topic names, once each, keeps the posts
// some topic wants, and stores the ones not seen before. A failing subreddit is
// reported and skipped rather than ending the run.
func (r *Resolved) SyncReddit(ctx context.Context, db *gorm.DB, client *http.Client, now func() time.Time, progress func(string)) []SubredditResult {
	cfg := r.Config
	g := r.gate(ctx, progress)
	var out []SubredditResult
	for _, sub := range cfg.Subreddits() {
		res := SubredditResult{Name: sub}
		var posts []Post
		err := g.fetch(func() (time.Duration, error) {
			var next time.Duration
			var err error
			posts, next, err = FetchSubreddit(ctx, client, cfg.Reddit.UserAgent, sub)
			return next, err
		})
		if ctx.Err() != nil {
			return out
		}
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		res.Fetched = len(posts)
		var keep []Post
		for _, p := range posts {
			topics, terms := Keep(p, cfg.Topics)
			if len(topics) == 0 {
				continue
			}
			p.Topics = strings.Join(topics, ",")
			p.Matched = strings.Join(terms, ",")
			keep = append(keep, p)
		}
		res.Kept = len(keep)
		res.New, res.Err = Upsert(db, keep, now())
		out = append(out, res)
	}
	for _, user := range cfg.RedditUsers() {
		res := SubredditResult{Name: "u/" + user}
		var posts []Post
		err := g.fetch(func() (time.Duration, error) {
			var next time.Duration
			var err error
			posts, next, err = FetchUser(ctx, client, cfg.Reddit.UserAgent, user)
			return next, err
		})
		if ctx.Err() != nil {
			return out
		}
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		res.Fetched = len(posts)
		var keep []Post
		for _, p := range posts {
			topics, terms := Keep(p, cfg.Topics)
			if len(topics) == 0 {
				continue
			}
			p.Topics, p.Matched = strings.Join(topics, ","), strings.Join(terms, ",")
			keep = append(keep, p)
		}
		res.Kept = len(keep)
		res.New, res.Err = Upsert(db, keep, now())
		out = append(out, res)
	}
	return out
}

// EnrichResult is one candidate's reply check.
type EnrichResult struct {
	Post    Post
	Replies int
	Err     error
}

// EnrichTop checks the reply count of the top n candidates, best first, so
// the digest can tell an unanswered question from a crowded thread. A
// candidate checked within the last three hours is skipped, which keeps a
// second run in the same sitting cheap.
func (r *Resolved) EnrichTop(ctx context.Context, db *gorm.DB, client *http.Client, n int, now func() time.Time, progress func(string)) ([]EnrichResult, error) {
	var posts []Post
	if err := db.Where("status = ?", StatusNew).Find(&posts).Error; err != nil {
		return nil, err
	}
	cfg := r.Config
	g := r.gate(ctx, progress)
	var out []EnrichResult
	for _, c := range Rank(posts, cfg.Topics, now(), time.Duration(cfg.Digest.MaxAgeHours)*time.Hour, n) {
		if !c.EnrichedAt.IsZero() && now().Sub(c.EnrichedAt) < 3*time.Hour {
			continue
		}
		var count int
		err := g.fetch(func() (time.Duration, error) {
			var next time.Duration
			var err error
			count, next, err = FetchReplyCount(ctx, client, cfg.Reddit.UserAgent, c.URL)
			return next, err
		})
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		res := EnrichResult{Post: c.Post, Replies: count, Err: err}
		if err == nil {
			res.Err = db.Model(&Post{}).Where("id = ?", c.ID).Updates(map[string]any{"replies": count, "enriched_at": now()}).Error
		}
		out = append(out, res)
	}
	return out, nil
}

// Thread reads one stored post's thread, waiting out Reddit's rate window if
// another request just spent it.
func (r *Resolved) Thread(ctx context.Context, client *http.Client, p Post, progress func(string)) (Thread, error) {
	g := r.gate(ctx, progress)
	var th Thread
	err := g.fetch(func() (time.Duration, error) {
		var next time.Duration
		var err error
		th, next, err = FetchThread(ctx, client, r.Config.Reddit.UserAgent, p.URL)
		return next, err
	})
	return th, err
}
