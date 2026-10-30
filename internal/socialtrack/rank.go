package socialtrack

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Match returns the keyword and watch terms a post contains. A term matches
// as a whole word or phrase, case-insensitively, so "erc" does not match
// "commercial".
func Match(p Post, keywords, watch []string) (kw, w []string) {
	text := strings.ToLower(p.Title + " " + p.Body)
	for _, k := range keywords {
		if containsTerm(text, k) {
			kw = append(kw, k)
		}
	}
	for _, k := range watch {
		if containsTerm(text, k) {
			w = append(w, k)
		}
	}
	return kw, w
}

func containsTerm(text, term string) bool {
	re := regexp.MustCompile(`(^|[^a-z0-9])` + regexp.QuoteMeta(strings.ToLower(term)) + `($|[^a-z0-9])`)
	return re.MatchString(text)
}

// Keep reports which topics keep a fetched post, and the terms it matched.
// A topic keeps a post from a subreddit it reads when that subreddit keeps
// every post, or when the post matches one of the topic's keywords or watch
// terms. No topic keeping it means it is not stored.
func Keep(p Post, topics []Topic) (kept, terms []string) {
	seen := map[string]bool{}
	for _, t := range topics {
		var sub *Subreddit
		for i := range t.Subreddits {
			if "r/"+t.Subreddits[i].Name == p.Community {
				sub = &t.Subreddits[i]
			}
		}
		if sub == nil {
			continue
		}
		kw, w := Match(p, t.Keywords, t.Watch)
		if !sub.All && len(kw)+len(w) == 0 {
			continue
		}
		kept = append(kept, t.Name)
		for _, term := range append(w, kw...) {
			if !seen[term] {
				seen[term] = true
				terms = append(terms, term)
			}
		}
	}
	return kept, terms
}

// halfLifeHours is how fast a thread's value as a reply target decays: a
// reply in the first day is seen by the asker and the early readers, and a
// reply on day three mostly is not.
const halfLifeHours = 24.0

var questionRE = regexp.MustCompile(`\?|\b(review|help|feedback|check)\b`)

// Impact is how much a good reply to this post is worth, before time: the
// terms it matched (a watch term outweighs any number of keywords), the
// weight of the community it was asked in, and half again for a post that
// asks for something rather than shows something.
func Impact(p Post, keywords, watch []string, weight float64) float64 {
	kw, w := Match(p, keywords, watch)
	v := (1.0 + float64(len(kw)) + 3*float64(len(w))) * weight
	if questionRE.MatchString(strings.ToLower(p.Title)) {
		v *= 1.5
	}
	return v
}

// Urgency is how much of that value is still on the table now: it halves
// every day, and shrinks as replies pile up, since the fifth answer to a
// question is read far less than the first. An unchecked reply count counts
// as unanswered.
func Urgency(p Post, now time.Time) float64 {
	age := now.Sub(p.PostedAt).Hours()
	if age < 0 {
		age = 0
	}
	u := math.Pow(0.5, age/halfLifeHours)
	if p.Replies > 0 {
		u /= 1 + float64(p.Replies)/3
	}
	return u
}

// Ranked is a post with its digest score and the two halves of it.
type Ranked struct {
	Post
	Score   float64
	Impact  float64
	Urgency float64
}

// TopicImpact is a post's impact under the best of the topics that kept it.
func TopicImpact(p Post, topics []Topic) float64 {
	best := 0.0
	for _, name := range strings.Split(p.Topics, ",") {
		for _, t := range topics {
			if t.Name == name {
				best = math.Max(best, Impact(p, t.Keywords, t.Watch, t.WeightOf(p.Community)))
			}
		}
	}
	return best
}

// Rank orders new posts younger than maxAge by impact times urgency, highest
// first, and keeps the top limit.
func Rank(posts []Post, topics []Topic, now time.Time, maxAge time.Duration, limit int) []Ranked {
	var out []Ranked
	for _, p := range posts {
		if p.Status != StatusNew || now.Sub(p.PostedAt) > maxAge {
			continue
		}
		im, ur := TopicImpact(p, topics), Urgency(p, now)
		out = append(out, Ranked{Post: p, Score: im * ur, Impact: im, Urgency: ur})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].PostedAt.After(out[j].PostedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Streak is the reply habit as of a day: replies today, and how many days in
// a row up to today (or yesterday, if today has none yet) met the goal.
type Streak struct {
	Today   int
	Goal    int
	Days    int
	PerDay  map[string]int // YYYY-MM-DD in loc -> replies
	LastDay string
}

// ComputeStreak counts replies per local day from their status times.
func ComputeStreak(replied []time.Time, goal int, now time.Time, loc *time.Location) Streak {
	per := map[string]int{}
	for _, t := range replied {
		per[t.In(loc).Format("2006-01-02")]++
	}
	today := now.In(loc)
	s := Streak{Goal: goal, PerDay: per, Today: per[today.Format("2006-01-02")]}
	day := today
	if s.Today < goal {
		day = day.AddDate(0, 0, -1) // today is still open; count from yesterday
	}
	for per[day.Format("2006-01-02")] >= goal {
		s.Days++
		s.LastDay = day.Format("2006-01-02")
		day = day.AddDate(0, 0, -1)
	}
	return s
}
