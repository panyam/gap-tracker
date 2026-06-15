package disctrack

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"gorm.io/gorm"
)

// DefaultSnapshotScript is the absolute path to the Playwright scraper baked
// in at install time by the Makefile via -ldflags. Used when the project's
// .discord-tracking.yaml leaves discord.snapshot_script unset. Empty when
// disctrack was built without ldflags (developer go build, tests).
var DefaultSnapshotScript = ""

// SyncOptions controls one disctrack sync invocation.
type SyncOptions struct {
	Channel    ChannelSpec
	Since      string // "" = read cursor from DB
	MaxIters   int    // 0 = let snapshot.py default
	NoBackup   bool
	DryRun     bool
	Headless   bool
	Profile    string // named profile under PROFILE_BASE (snapshot.py-side)
	ProfileDir string // explicit override; takes precedence over Profile
}

// SyncResult is the aggregate report printed at the end of a sync.
type SyncResult struct {
	ChannelID  string `json:"channel_id"`
	Inserted   int    `json:"new"`
	Edits      int    `json:"edits"`
	Duplicates int    `json:"duplicates"`
	Errors     int    `json:"errors"`
	OldestTS   string `json:"oldest_ts"`
	NewestTS   string `json:"newest_ts"`
	DryRun     bool   `json:"dry_run"`
}

// Sync runs a full disctrack sync for one channel: reads cursor, optionally
// snapshots the DB, spawns snapshot.py, pipes JSONL into ingest, advances the
// cursor. All steps log progress to stderr; the SyncResult is what callers
// turn into stdout JSON.
//
// snapshot.py path resolution: opts overrides → config.Discord.SnapshotScript
// → DefaultSnapshotScript (Makefile ldflag) → cwd-relative python/snapshot.py.
func (r *Resolved) Sync(opts SyncOptions, scriptOverride string) (SyncResult, error) {
	if opts.Channel.ChannelID == "" {
		return SyncResult{}, errors.New("sync: channel_id is required")
	}
	db, err := r.OpenDB()
	if err != nil {
		return SyncResult{}, err
	}

	area := opts.Channel.Name
	if area == "" {
		area = opts.Channel.ChannelID
	}

	// Read the existing cursor up front for the advance-only-if-newer check
	// below. Separate from `since` so a --since override (a re-pull window)
	// can't regress the cursor when newest < currentCursor.
	var currentCursor string
	var c Cursor
	switch err := db.Where("source_type = ? AND source_key = ?", SourceTypeDiscord, opts.Channel.ChannelID).First(&c).Error; {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// cold start
	case err != nil:
		return SyncResult{}, fmt.Errorf("read cursor: %w", err)
	default:
		currentCursor = c.CursorValue
	}

	since := opts.Since
	if since == "" {
		since = currentCursor
		if since != "" {
			fmt.Fprintf(os.Stderr, "cursor: %s (last updated %s)\n", c.CursorValue, c.UpdatedAt)
		}
	} else {
		fmt.Fprintf(os.Stderr, "using --since override: %s (cursor stays at %s)\n", since, valueOrNone(currentCursor))
	}
	if since == "" {
		fmt.Fprintln(os.Stderr, "no cursor found; cold start")
	}

	if !opts.NoBackup && !opts.DryRun {
		if _, statErr := os.Stat(r.DBFile()); statErr == nil {
			if _, err := r.Backup(0); err != nil {
				fmt.Fprintln(os.Stderr, "warn: backup failed:", err)
			}
		}
	}

	scriptPath, err := r.resolveSnapshotScript(scriptOverride)
	if err != nil {
		return SyncResult{}, err
	}

	guildID := r.Config.Discord.GuildID
	if guildID == "" {
		return SyncResult{}, errors.New("sync: discord.guild_id is required in .discord-tracking.yaml")
	}

	profileDir := opts.ProfileDir
	if profileDir == "" {
		profileDir = expandPath(r.Config.Discord.ProfileDir)
	}
	if profileDir == "" {
		// Default to <tracking-root>/profiles/default and auto-create.
		profileDir = filepath.Join(r.Root, "profiles", "default")
		if err := os.MkdirAll(profileDir, 0o755); err != nil {
			return SyncResult{}, fmt.Errorf("ensure profile dir: %w", err)
		}
	}

	stats, oldest, newest, err := r.spawnSnapshotAndIngest(db, opts, scriptPath, guildID, profileDir, since)
	if err != nil {
		return SyncResult{}, err
	}

	// Advance only when we genuinely went past the existing cursor. Re-pulls
	// with --since (which may be older than currentCursor) must not regress.
	if !opts.DryRun && newest != "" && newest > currentCursor {
		if err := SetDiscordCursor(db, opts.Channel.ChannelID, opts.Channel.Name, newest); err != nil {
			return SyncResult{}, fmt.Errorf("advance cursor: %w", err)
		}
		fmt.Fprintf(os.Stderr, "cursor advanced: discord/%s → %s\n", opts.Channel.ChannelID, newest)
	} else if newest != "" && currentCursor != "" && newest <= currentCursor {
		fmt.Fprintf(os.Stderr, "cursor unchanged at %s (re-pull only; newest seen: %s)\n", currentCursor, newest)
	}

	return SyncResult{
		ChannelID:  opts.Channel.ChannelID,
		Inserted:   stats.Inserted,
		Edits:      stats.Edits,
		Duplicates: stats.Duplicates,
		Errors:     stats.Errors,
		OldestTS:   oldest,
		NewestTS:   newest,
		DryRun:     opts.DryRun,
	}, nil
}

func (r *Resolved) resolveSnapshotScript(override string) (string, error) {
	candidates := []string{
		override,
		expandPath(r.Config.Discord.SnapshotScript),
		DefaultSnapshotScript,
		"python/snapshot.py",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("snapshot.py not found. Tried: %v. Set discord.snapshot_script in .discord-tracking.yaml or reinstall disctrack via 'make install-disctrack'", candidates)
}

func (r *Resolved) spawnSnapshotAndIngest(
	db *gorm.DB,
	opts SyncOptions,
	scriptPath, guildID, profileDir, since string,
) (IngestStats, string, string, error) {
	args := []string{
		"run", scriptPath,
		"--server-id", guildID,
		"--channel", opts.Channel.ChannelID,
		"--profile-dir", profileDir,
	}
	if opts.MaxIters > 0 {
		args = append(args, "--max-iters", strconv.Itoa(opts.MaxIters))
	}
	if since != "" {
		args = append(args, "--since", since)
	}
	if opts.Headless {
		args = append(args, "--headless")
	}

	cmd := exec.Command("uv", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return IngestStats{}, "", "", fmt.Errorf("pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return IngestStats{}, "", "", fmt.Errorf("start snapshot.py: %w", err)
	}

	var stats IngestStats
	if opts.DryRun {
		stats, err = countJSONL(stdout)
	} else {
		stats, err = Ingest(db, stdout)
	}
	waitErr := cmd.Wait()
	if err != nil {
		return stats, stats.OldestTS, stats.NewestTS, err
	}
	if waitErr != nil {
		return stats, stats.OldestTS, stats.NewestTS, fmt.Errorf("snapshot.py exited: %w", waitErr)
	}
	return stats, stats.OldestTS, stats.NewestTS, nil
}

// countJSONL drains the scraper's output in --dry-run mode: just counts lines
// and tracks ts extrema. No DB writes.
func countJSONL(r io.Reader) (IngestStats, error) {
	dec := json.NewDecoder(r)
	var stats IngestStats
	for {
		var m IncomingMessage
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				return stats, nil
			}
			stats.Errors++
			continue
		}
		if m.ChannelID == "" || m.MessageID == "" || m.TS == "" {
			stats.Errors++
			continue
		}
		stats.Inserted++
		if stats.OldestTS == "" || m.TS < stats.OldestTS {
			stats.OldestTS = m.TS
		}
		if stats.NewestTS == "" || m.TS > stats.NewestTS {
			stats.NewestTS = m.TS
		}
	}
}

// valueOrNone returns the input or the string "none" when empty. Used in
// stderr logs so the cursor display reads "(cursor stays at none)" instead
// of trailing whitespace on a cold-start re-pull.
func valueOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// expandPath resolves a leading "~" to $HOME. Returns unchanged for empty
// strings or paths that don't start with ~.
func expandPath(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if len(p) > 1 && p[1] == '/' {
		return filepath.Join(home, p[2:])
	}
	return p
}
