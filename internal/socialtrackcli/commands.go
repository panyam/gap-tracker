package socialtrackcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/panyam/gap-tracker/internal/socialtrack"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

func open() (*socialtrack.Resolved, *gorm.DB, error) {
	r, err := resolve()
	if err != nil {
		return nil, nil, err
	}
	db, err := r.OpenDB()
	return r, db, err
}

func syncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Read every subreddit any topic names, and store the new posts that match",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			return runSync(cmd, r, db)
		},
	}
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func progress(cmd *cobra.Command) func(string) {
	return func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), m) }
}

func runSync(cmd *cobra.Command, r *socialtrack.Resolved, db *gorm.DB) error {
	results := r.SyncReddit(cmd.Context(), db, httpClient, time.Now, progress(cmd))
	failed := 0
	for _, res := range results {
		if res.Err != nil {
			failed++
			fmt.Fprintf(cmd.OutOrStdout(), "r/%-22s failed: %v\n", res.Name, res.Err)
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "r/%-22s %3d read, %3d kept, %3d new\n", res.Name, res.Fetched, res.Kept, res.New)
	}
	if failed == len(results) && failed > 0 {
		return fmt.Errorf("every source failed")
	}
	return nil
}

func enrichCmd() *cobra.Command {
	var n int
	c := &cobra.Command{
		Use:   "enrich",
		Short: "Check the reply count of the top candidates, about a minute each",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			return runEnrich(cmd, r, db, n)
		},
	}
	c.Flags().IntVarP(&n, "count", "n", 0, "how many candidates (default: digest.enrich)")
	return c
}

func runEnrich(cmd *cobra.Command, r *socialtrack.Resolved, db *gorm.DB, n int) error {
	if n <= 0 {
		n = r.Config.Digest.Enrich
	}
	results, err := r.EnrichTop(cmd.Context(), db, httpClient, n, time.Now, progress(cmd))
	for _, res := range results {
		if res.Err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  failed %s: %v\n", res.Post.ID, res.Err)
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %3d replies  %s\n", res.Replies, snippet(res.Post.Title, 70))
	}
	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "  every top candidate was checked in the last three hours")
	}
	return err
}

func todayCmd() *cobra.Command {
	var topic, format string
	c := &cobra.Command{
		Use:   "today",
		Short: "sync, enrich the top candidates, then show the digest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "syncing")
			if err := runSync(cmd, r, db); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "checking reply counts")
			if err := runEnrich(cmd, r, db, 0); err != nil {
				return err
			}
			now := time.Now()
			ranked, err := digest(r, db, now, 0, topic)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return writeDigest(cmd.OutOrStdout(), ranked, now, format)
		},
	}
	c.Flags().StringVar(&topic, "topic", "", "only this topic")
	c.Flags().StringVarP(&format, "output", "o", "md", "md | text | json")
	return c
}

func loadNew(db *gorm.DB, topic string) ([]socialtrack.Post, error) {
	var posts []socialtrack.Post
	if err := db.Where("status = ?", socialtrack.StatusNew).Find(&posts).Error; err != nil {
		return nil, err
	}
	if topic == "" {
		return posts, nil
	}
	var out []socialtrack.Post
	for _, p := range posts {
		for _, t := range strings.Split(p.Topics, ",") {
			if t == topic {
				out = append(out, p)
				break
			}
		}
	}
	return out, nil
}

func digest(r *socialtrack.Resolved, db *gorm.DB, now time.Time, limit int, topic string) ([]socialtrack.Ranked, error) {
	cfg := r.Config
	if topic != "" {
		if _, ok := cfg.Topic(topic); !ok {
			return nil, fmt.Errorf("no topic %q in %s", topic, socialtrack.ConfigFile)
		}
	}
	posts, err := loadNew(db, topic)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = cfg.Digest.Limit
	}
	return socialtrack.Rank(posts, cfg.Topics, now, time.Duration(cfg.Digest.MaxAgeHours)*time.Hour, limit), nil
}

func digestCmd() *cobra.Command {
	var format, topic string
	var limit int
	c := &cobra.Command{
		Use:   "digest",
		Short: "List the threads worth a reply now, best first",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			now := time.Now()
			ranked, err := digest(r, db, now, limit, topic)
			if err != nil {
				return err
			}
			return writeDigest(cmd.OutOrStdout(), ranked, now, format)
		},
	}
	c.Flags().StringVarP(&format, "output", "o", "md", "md | text | json")
	c.Flags().IntVarP(&limit, "limit", "n", 0, "how many threads (default: digest.limit)")
	c.Flags().StringVar(&topic, "topic", "", "only this topic")
	return c
}

func writeDigest(w io.Writer, ranked []socialtrack.Ranked, now time.Time, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(ranked)
	case "md", "text":
	default:
		return fmt.Errorf("unknown output %q (md | text | json)", format)
	}
	if len(ranked) == 0 {
		fmt.Fprintln(w, "Nothing new worth a reply. Run `socialtrack sync` first if it has been a while.")
		return nil
	}
	for i, p := range ranked {
		age := humanAge(now.Sub(p.PostedAt))
		matched := ""
		if p.Matched != "" {
			matched = " · matched " + p.Matched
		}
		replies := "replies unchecked"
		if p.Replies >= 0 {
			replies = fmt.Sprintf("%d replies", p.Replies)
		}
		if format == "md" {
			fmt.Fprintf(w, "%d. **%s** (%s, %s ago, %s · %s · impact %.1f × urgency %.2f%s)\n   %s\n   id `%s`\n",
				i+1, p.Title, p.Community, age, replies, p.Topics, p.Impact, p.Urgency, matched, p.URL, shortID(p.ID))
			if s := snippet(p.Body, 240); s != "" {
				fmt.Fprintf(w, "   > %s\n", s)
			}
			fmt.Fprintln(w)
		} else {
			fmt.Fprintf(w, "%2d. [%s, %s, %s] %s\n    %s  (%s)\n", i+1, p.Community, age, replies, p.Title, p.URL, shortID(p.ID))
		}
	}
	return nil
}

func snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := strings.LastIndex(s[:n], " ")
	if cut < n/2 {
		cut = n
	}
	return s[:cut] + "…"
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func markCmd() *cobra.Command {
	var note string
	c := &cobra.Command{
		Use:   "mark <n|id> replied|skipped|new",
		Short: "Record what happened to a thread",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			s := socialtrack.Status(args[1])
			switch s {
			case socialtrack.StatusReplied, socialtrack.StatusSkipped, socialtrack.StatusNew:
			default:
				return fmt.Errorf("status must be replied, skipped or new, not %q", args[1])
			}
			p, err := findPost(r, db, args[0])
			if err != nil {
				return err
			}
			if err := socialtrack.Mark(db, p.ID, s, note, time.Now()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s marked %s: %s\n", shortID(p.ID), s, p.Title)
			return nil
		},
	}
	c.Flags().StringVar(&note, "note", "", "a line on what you said or why you skipped")
	return c
}

func streak(r *socialtrack.Resolved, db *gorm.DB, now time.Time) (socialtrack.Streak, error) {
	var times []time.Time
	err := db.Model(&socialtrack.Post{}).Where("status = ?", socialtrack.StatusReplied).Pluck("status_at", &times).Error
	if err != nil {
		return socialtrack.Streak{}, err
	}
	return socialtrack.ComputeStreak(times, r.Config.Digest.DailyGoal, now, time.Local), nil
}

func streakCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "streak",
		Short: "Replies per day over the last two weeks, and the current streak",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			now := time.Now()
			s, err := streak(r, db, now)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "today: %d of %d · streak: %d day(s)\n\n", s.Today, s.Goal, s.Days)
			for i := 13; i >= 0; i-- {
				d := now.AddDate(0, 0, -i).Format("2006-01-02")
				n := s.PerDay[d]
				fmt.Fprintf(w, "%s %s %s\n", d, strings.Repeat("█", n), map[bool]string{true: "", false: "·"}[n > 0])
			}
			return nil
		},
	}
}

func notifyCmd() *cobra.Command {
	var dry bool
	c := &cobra.Command{
		Use:   "notify",
		Short: "Show a macOS notification summing up the digest and the streak",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			now := time.Now()
			ranked, err := digest(r, db, now, 0, "")
			if err != nil {
				return err
			}
			s, err := streak(r, db, now)
			if err != nil {
				return err
			}
			msg := notifyText(len(ranked), s)
			if len(r.Config.XAccounts("")) > 0 {
				last, err := socialtrack.LastVisit(db, "x")
				if err != nil {
					return err
				}
				if y, m, d := last.Date(); last.IsZero() || y != now.Year() || m != now.Month() || d != now.Day() {
					msg += " X not checked today (socialtrack browse)."
				}
			}
			if dry {
				fmt.Fprintln(cmd.OutOrStdout(), msg)
				return nil
			}
			script := fmt.Sprintf("display notification %q with title %q", msg, "socialtrack")
			return exec.CommandContext(context.Background(), "osascript", "-e", script).Run()
		},
	}
	c.Flags().BoolVar(&dry, "dry-run", false, "print the message instead of showing it")
	return c
}

func notifyText(threads int, s socialtrack.Streak) string {
	var b strings.Builder
	switch threads {
	case 0:
		b.WriteString("No new threads worth a reply.")
	case 1:
		b.WriteString("1 thread worth a reply.")
	default:
		fmt.Fprintf(&b, "%d threads worth a reply.", threads)
	}
	fmt.Fprintf(&b, " Today %d/%d, streak %d day(s).", s.Today, s.Goal, s.Days)
	return b.String()
}

func configCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print the resolved tracking root and config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := resolve()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "root:       %s\ndb:         %s\n", r.Root, r.DBFile())
			for _, t := range r.Config.Topics {
				var subs []string
				for _, s := range t.Subreddits {
					label := s.Name
					if s.All {
						label += " (all)"
					}
					if s.Weight > 0 && s.Weight != 1 {
						label += fmt.Sprintf(" ×%g", s.Weight)
					}
					subs = append(subs, label)
				}
				fmt.Fprintf(w, "topic %s\n  subreddits: %s\n  keywords:   %s\n  watch:      %s\n",
					t.Name, strings.Join(subs, ", "), strings.Join(t.Keywords, ", "), strings.Join(t.Watch, ", "))
			}
			fmt.Fprintf(w, "digest:     top %d, under %dh, %d reply checks, goal %d/day\n",
				r.Config.Digest.Limit, r.Config.Digest.MaxAgeHours, r.Config.Digest.Enrich, r.Config.Digest.DailyGoal)
			return nil
		},
	}
}

func showCmd() *cobra.Command {
	var max int
	c := &cobra.Command{
		Use:   "show <n|id>",
		Short: "Print a stored thread's post and comments, read from its feed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			p, err := findPost(r, db, args[0])
			if err != nil {
				return err
			}
			th, err := r.Thread(cmd.Context(), httpClient, p, progress(cmd))
			if err != nil {
				return err
			}
			return writeThread(cmd.OutOrStdout(), p, th, max)
		},
	}
	c.Flags().IntVar(&max, "max-comments", 30, "print at most this many comments")
	return c
}

func writeThread(w io.Writer, p socialtrack.Post, th socialtrack.Thread, max int) error {
	fmt.Fprintf(w, "# %s\n\n%s · u/%s · %s\n%s\n\n%s\n\n## %d comment(s)\n\n", th.Title, p.Community, th.Author, th.When.Format("2006-01-02 15:04"), p.URL, th.Body, len(th.Comments))
	for i, c := range th.Comments {
		if max > 0 && i == max {
			fmt.Fprintf(w, "(%d more not shown)\n", len(th.Comments)-max)
			break
		}
		fmt.Fprintf(w, "- **u/%s** (%s): %s\n", c.Author, c.When.Format("01-02 15:04"), c.Body)
	}
	return nil
}

// shortID drops the source and the kind prefix: reddit:t3_1xbv8ds -> 1xbv8ds.
func shortID(id string) string {
	_, rest, ok := strings.Cut(id, ":")
	if !ok {
		rest = id
	}
	if i := strings.Index(rest, "_"); i >= 0 {
		return rest[i+1:]
	}
	return rest
}

// findPost resolves an argument to a stored post: a full id, a short id from
// ls, or a small number naming that rank in the current digest.
func findPost(r *socialtrack.Resolved, db *gorm.DB, arg string) (socialtrack.Post, error) {
	var p socialtrack.Post
	if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= 100 {
		ranked, err := digest(r, db, time.Now(), n, "")
		if err != nil {
			return p, err
		}
		if len(ranked) < n {
			return p, fmt.Errorf("the digest has %d thread(s), not %d", len(ranked), n)
		}
		return ranked[n-1].Post, nil
	}
	// A short id is the tail of a full one on any network: reddit:t3_<id>,
	// x:<id>, linkedin:<hash>.
	var found []socialtrack.Post
	if err := db.Where("id = ? OR id LIKE ? OR id LIKE ?", arg, "%:"+arg, "%:t3_"+arg).Limit(2).Find(&found).Error; err != nil {
		return p, err
	}
	switch len(found) {
	case 0:
		return p, fmt.Errorf("no stored post %s (try `socialtrack ls`)", arg)
	case 1:
		return found[0], nil
	default:
		return p, fmt.Errorf("%s names more than one post (%s, %s); use the full id", arg, found[0].ID, found[1].ID)
	}
}

func lsCmd() *cobra.Command {
	var topic, status string
	var all bool
	var hours int
	c := &cobra.Command{
		Use:   "ls",
		Short: "List stored threads, newest first, with the short ids open and mark take",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, db, err := open()
			if err != nil {
				return err
			}
			q := db.Order("posted_at desc")
			if !all {
				q = q.Where("status = ?", status)
			}
			if hours > 0 {
				q = q.Where("posted_at >= ?", time.Now().Add(-time.Duration(hours)*time.Hour))
			}
			var posts []socialtrack.Post
			if err := q.Find(&posts).Error; err != nil {
				return err
			}
			now := time.Now()
			w := cmd.OutOrStdout()
			shown := 0
			for _, p := range posts {
				if topic != "" && !strings.Contains(","+p.Topics+",", ","+topic+",") {
					continue
				}
				replies := "  ?"
				if p.Replies >= 0 {
					replies = fmt.Sprintf("%3d", p.Replies)
				}
				fmt.Fprintf(w, "%-8s %4s %s💬 %-8s %-24s %s\n", shortID(p.ID), humanAge(now.Sub(p.PostedAt)), replies, p.Status, p.Community, snippet(p.Title, 70))
				shown++
			}
			if shown == 0 {
				fmt.Fprintln(w, "nothing stored matches")
			}
			return nil
		},
	}
	c.Flags().StringVar(&topic, "topic", "", "only this topic")
	c.Flags().StringVar(&status, "status", string(socialtrack.StatusNew), "new | replied | skipped")
	c.Flags().BoolVarP(&all, "all", "a", false, "every status")
	c.Flags().IntVar(&hours, "hours", 0, "only threads posted in the last N hours")
	return c
}

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <n|id>",
		Short: "Open a thread in the default browser: n is that rank in the digest, or an id from ls",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			p, err := findPost(r, db, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", shortID(p.ID), p.Title)
			return exec.CommandContext(cmd.Context(), "open", p.URL).Run()
		},
	}
}

func addCmd() *cobra.Command {
	var title, topic, note string
	c := &cobra.Command{
		Use:   "add <url>",
		Short: "Add a thread by hand, from Reddit, X, HN, LinkedIn, YC or anywhere",
		Long: "Add a thread you found yourself, so it joins the digest, ls, open, mark and the streak. " +
			"A Reddit link is read from the thread's feed for its title, author, time and reply count. " +
			"Any other link needs --title, and is dated now.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, db, err := open()
			if err != nil {
				return err
			}
			l, err := socialtrack.ParseLink(args[0])
			if err != nil {
				return err
			}
			var existing socialtrack.Post
			if db.First(&existing, "id = ?", l.ID).Error == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "already stored: %s  %s (%s)\n", shortID(existing.ID), existing.Title, existing.Status)
				return nil
			}
			now := time.Now()
			p := socialtrack.Post{ID: l.ID, Source: l.Source, Community: l.Community, URL: l.URL, Title: title, PostedAt: now, Note: note}
			replies, enriched := -1, time.Time{}
			if l.Source == "reddit" {
				th, err := r.Thread(cmd.Context(), httpClient, p, progress(cmd))
				if err != nil {
					return fmt.Errorf("read the thread's feed: %w", err)
				}
				if p.Title == "" {
					p.Title = th.Title
				}
				p.Author, p.Body, p.PostedAt = th.Author, th.Body, th.When
				replies, enriched = len(th.Comments), now
			}
			if p.Title == "" {
				return fmt.Errorf("a %s link needs --title, since socialtrack cannot read %s threads", l.Source, l.Source)
			}
			topics, terms := addTopics(r.Config, p, topic)
			if len(topics) == 0 {
				return fmt.Errorf("no topic matches this thread; name one with --topic (%s)", topicNames(r.Config))
			}
			p.Topics, p.Matched = strings.Join(topics, ","), strings.Join(terms, ",")
			if _, err := socialtrack.Upsert(db, []socialtrack.Post{p}, now); err != nil {
				return err
			}
			if replies >= 0 {
				db.Model(&socialtrack.Post{}).Where("id = ?", p.ID).Updates(map[string]any{"replies": replies, "enriched_at": enriched})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %s  %s (%s, topic %s)\n", shortID(p.ID), p.Title, p.Community, p.Topics)
			return nil
		},
	}
	c.Flags().StringVar(&title, "title", "", "the thread's title (required for anything but Reddit)")
	c.Flags().StringVar(&topic, "topic", "", "the topic it belongs to (default: the topics whose keywords it matches, or the only topic)")
	c.Flags().StringVar(&note, "note", "", "why it is worth a reply")
	return c
}

// addTopics picks the topics a hand-added thread belongs to: the one named,
// else every topic whose keywords or watch terms it matches, else the only
// topic when there is just one.
func addTopics(cfg socialtrack.Config, p socialtrack.Post, named string) (topics, terms []string) {
	if named != "" {
		t, ok := cfg.Topic(named)
		if !ok {
			return nil, nil
		}
		kw, w := socialtrack.Match(p, t.Keywords, t.Watch)
		return []string{named}, append(w, kw...)
	}
	for _, t := range cfg.Topics {
		kw, w := socialtrack.Match(p, t.Keywords, t.Watch)
		if len(kw)+len(w) > 0 {
			topics = append(topics, t.Name)
			terms = append(terms, append(w, kw...)...)
		}
	}
	if len(topics) == 0 && len(cfg.Topics) == 1 {
		topics = []string{cfg.Topics[0].Name}
	}
	return topics, terms
}

func topicNames(cfg socialtrack.Config) string {
	var names []string
	for _, t := range cfg.Topics {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func browseCmd() *cobra.Command {
	var topic string
	var replies, printOnly bool
	c := &cobra.Command{
		Use:   "browse [x]",
		Short: "Open a live X search over the topic's curated accounts in your browser",
		Long: "Open the newest posts from the accounts listed under a topic's x.accounts, as a live X search " +
			"in your default browser. socialtrack does not read X; you do, and `socialtrack add <url> --title ...` " +
			"brings a post worth answering into the digest. The visit is recorded for the daily nudge.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] != "x" {
				return fmt.Errorf("only x can be browsed; Reddit is read by sync")
			}
			r, db, err := open()
			if err != nil {
				return err
			}
			if topic != "" {
				if _, ok := r.Config.Topic(topic); !ok {
					return fmt.Errorf("no topic %q (%s)", topic, topicNames(r.Config))
				}
			}
			urls := socialtrack.XSearchURLs(r.Config.XAccounts(topic), replies)
			if len(urls) == 0 {
				return fmt.Errorf("no X accounts listed; add handles under a topic's x.accounts in %s", socialtrack.ConfigFile)
			}
			for _, u := range urls {
				fmt.Fprintln(cmd.OutOrStdout(), u)
				if !printOnly {
					if err := exec.CommandContext(cmd.Context(), "open", u).Run(); err != nil {
						return err
					}
				}
			}
			if printOnly {
				return nil
			}
			return db.Create(&socialtrack.Visit{Source: "x", Topic: topic, At: time.Now()}).Error
		},
	}
	c.Flags().StringVar(&topic, "topic", "", "only this topic's accounts")
	c.Flags().BoolVar(&replies, "replies", false, "include the accounts' replies to others")
	c.Flags().BoolVar(&printOnly, "print", false, "print the search links instead of opening them")
	return c
}
