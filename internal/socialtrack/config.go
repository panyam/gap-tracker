// Package socialtrack finds threads worth replying to on social sites and
// keeps a log of which ones got a reply, so replying becomes a daily habit
// rather than an occasional search. Reddit is the first source, read through
// each subreddit's public RSS feed; X is planned behind the same store.
//
// The layout mirrors disctrack: a tracking root holds .social-tracking.yaml
// and social.db, and the root is found by walking up from cwd.
package socialtrack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the filename at a tracking root.
const ConfigFile = ".social-tracking.yaml"

// EnvVarRoot is consulted when no root resolves from the starting directory.
const EnvVarRoot = "SOCIALTRACK_ROOT"

// Config is the schema of <tracking-root>/.social-tracking.yaml. A root is
// one person's account: the rate window, the daily goal and the streak are
// shared, and each project they reply about is a topic with its own matching.
type Config struct {
	Reddit RedditSection `yaml:"reddit"`
	Digest DigestSection `yaml:"digest"`
	Topics []Topic       `yaml:"topics"`
}

type RedditSection struct {
	// UserAgent identifies the tracker to Reddit. Reddit asks every client
	// for a descriptive one with contact details.
	UserAgent string `yaml:"user_agent"`

	// DelaySeconds is the least pause between two feed requests; Reddit's
	// own rate window usually asks for more.
	DelaySeconds int `yaml:"delay_seconds"`
}

// Topic is one project or interest the account replies about.
type Topic struct {
	Name string `yaml:"name"`

	Subreddits []Subreddit `yaml:"subreddits"`

	// Keywords mark a post as on-topic. Matched case-insensitively, as whole
	// words or phrases, against the title and body; each hit adds one to the
	// post's impact.
	Keywords []string `yaml:"keywords"`

	// Watch terms are mentions to never miss (the project's name, its repo).
	// Each hit adds three and keeps the post even in a keyword-filtered sub.
	Watch []string `yaml:"watch"`

	X XSection `yaml:"x"`
}

// XSection is a topic's hand-curated X accounts. socialtrack does not read X:
// browse opens a live search over these accounts in the user's own browser,
// and the posts worth answering come back in through add.
type XSection struct {
	Accounts []string `yaml:"accounts"`
}

type Subreddit struct {
	Name string `yaml:"name"`

	// All makes every new post a candidate, for a subreddit whose posts are
	// mostly questions in the project's domain (a review-request sub).
	// Without it only posts matching a keyword or watch term are kept.
	All bool `yaml:"all"`

	// Weight scales a post's impact by where it was asked: a reply in the
	// sub where the project's users are counts for more than one in a
	// general sub. Defaults to 1.
	Weight float64 `yaml:"weight"`
}

// WeightOf is a topic's impact weight for a community like "r/KiCad".
func (t Topic) WeightOf(community string) float64 {
	for _, s := range t.Subreddits {
		if "r/"+s.Name == community && s.Weight > 0 {
			return s.Weight
		}
	}
	return 1
}

// Subreddits lists every subreddit any topic reads, once each, in config order.
func (c Config) Subreddits() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range c.Topics {
		for _, s := range t.Subreddits {
			if !seen[s.Name] {
				seen[s.Name] = true
				out = append(out, s.Name)
			}
		}
	}
	return out
}

// Topic returns the topic with this name.
func (c Config) Topic(name string) (Topic, bool) {
	for _, t := range c.Topics {
		if t.Name == name {
			return t, true
		}
	}
	return Topic{}, false
}

type DigestSection struct {
	// MaxAgeHours drops posts older than this from the digest, since a reply
	// to a stale thread is rarely seen.
	MaxAgeHours int `yaml:"max_age_hours"`

	// Limit is how many threads the digest lists.
	Limit int `yaml:"limit"`

	// Enrich is how many top candidates get their reply count checked
	// before ranking. Each costs about a minute of Reddit's rate window.
	Enrich int `yaml:"enrich"`

	// DailyGoal is the number of replies a day that counts toward the streak.
	DailyGoal int `yaml:"daily_goal"`
}

// Resolved is a tracking root with its loaded config.
type Resolved struct {
	Root   string
	Config Config
}

// DBFile is the store's location for this tracking root.
func (r *Resolved) DBFile() string { return filepath.Join(r.Root, "social.db") }

// ResolveFrom walks up from start looking for .social-tracking.yaml, then
// falls back to $SOCIALTRACK_ROOT.
func ResolveFrom(start string) (*Resolved, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for cur := abs; ; cur = filepath.Dir(cur) {
		if _, err := os.Stat(filepath.Join(cur, ConfigFile)); err == nil {
			return Load(cur)
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	if env := os.Getenv(EnvVarRoot); env != "" {
		return Load(env)
	}
	return nil, fmt.Errorf("no %s found from %s, and $%s is unset", ConfigFile, abs, EnvVarRoot)
}

// Load reads the config at root and fills defaults.
func Load(root string) (*Resolved, error) {
	raw, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(bytesReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(root, ConfigFile), err)
	}
	if cfg.Reddit.UserAgent == "" {
		return nil, fmt.Errorf("%s: reddit.user_agent is required (Reddit asks for a descriptive one with contact details)", ConfigFile)
	}
	if len(cfg.Topics) == 0 {
		return nil, fmt.Errorf("%s: no topics", ConfigFile)
	}
	for _, t := range cfg.Topics {
		if t.Name == "" || strings.Contains(t.Name, ",") {
			return nil, fmt.Errorf("%s: a topic needs a name without commas", ConfigFile)
		}
	}
	if cfg.Reddit.DelaySeconds <= 0 {
		cfg.Reddit.DelaySeconds = 3
	}
	if cfg.Digest.MaxAgeHours <= 0 {
		cfg.Digest.MaxAgeHours = 72
	}
	if cfg.Digest.Limit <= 0 {
		cfg.Digest.Limit = 5
	}
	if cfg.Digest.Enrich <= 0 {
		cfg.Digest.Enrich = 10
	}
	if cfg.Digest.DailyGoal <= 0 {
		cfg.Digest.DailyGoal = 1
	}
	return &Resolved{Root: root, Config: cfg}, nil
}
