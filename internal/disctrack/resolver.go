// Package disctrack holds the resolver, config types, and manifest types for
// the disctrack binary. Kept separate from the cli package so other gap-tracker
// code can reuse the resolver if needed.
package disctrack

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the filename at a tracking root.
const ConfigFile = ".discord-tracking.yaml"

// SymlinkName is the conventional name code repos use to point at a
// sibling tracking repo.
const SymlinkName = "discord_tracking"

// Config is the schema of <tracking-root>/.discord-tracking.yaml. Fields
// match the playbook (gap-tracker/plugins/gaps/reference/discord-tracking-playbook.md).
type Config struct {
	ChannelsDir string `yaml:"channels_dir"`

	// GapsDir holds one docs folder per tracked non-thread channel, at
	// <gaps_dir>/<channel-name>/. Defaults to "proposals".
	GapsDir string `yaml:"gaps_dir"`

	// LegacyAreasDir is the pre-channel-model name of GapsDir. Read only as
	// a fallback when gaps_dir is unset, with a deprecation warning.
	LegacyAreasDir string `yaml:"areas_dir"`

	Discord  DiscordSection `yaml:"discord"`
	Code     CodeSection    `yaml:"code"`
	Gapt     GaptSection    `yaml:"gapt"`
	SyncTool SyncToolSec    `yaml:"sync_tool"`
}

type DiscordSection struct {
	GuildID string `yaml:"guild_id"`

	// SnapshotScript is the absolute path to the Playwright scraper. disctrack
	// sync invokes it as `uv run <SnapshotScript>`. When empty, falls back to
	// the value baked in at build time (Makefile -ldflags) and finally to
	// "python/snapshot.py" relative to cwd.
	SnapshotScript string `yaml:"snapshot_script"`

	// ProfileDir is the Chromium user-data-dir to launch Playwright against.
	// When empty, disctrack defaults to <tracking-root>/profiles/default and
	// auto-creates it. mcpcontrib points this at its historical login profile
	// so the existing session keeps working.
	ProfileDir string `yaml:"profile_dir"`

	SetupDoc string `yaml:"setup_doc"`
}

type CodeSection struct {
	Name            string `yaml:"name"`
	Repo            string `yaml:"repo"`
	DefaultWorktree string `yaml:"default_worktree"`
}

type GaptSection struct {
	Enabled    bool   `yaml:"enabled"`
	ProjectID  string `yaml:"project_id"`
	ConfigPath string `yaml:"config_path"`
}

type SyncToolSec struct {
	Command string `yaml:"command"`
}

// Resolved holds the resolved tracking root and loaded config together.
type Resolved struct {
	Root   string
	Config Config
}

// Resolve walks up from cwd looking for a tracking root. Rules in order:
//
//  1. <ancestor>/.discord-tracking.yaml exists. Ancestor is the root.
//  2. <ancestor>/discord_tracking exists (file, dir, or symlink). Follow it;
//     if the target contains .discord-tracking.yaml, that target is the root.
//
// Returns nil with a clear error if no root resolves.
func Resolve() (*Resolved, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cwd: %w", err)
	}
	return ResolveFrom(cwd)
}

// ResolveFrom is Resolve seeded with an explicit starting directory.
func ResolveFrom(start string) (*Resolved, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	cur := abs
	for {
		if fileExists(filepath.Join(cur, ConfigFile)) {
			return loadResolved(cur)
		}
		link := filepath.Join(cur, SymlinkName)
		if pathExists(link) {
			target, err := filepath.EvalSymlinks(link)
			if err == nil && fileExists(filepath.Join(target, ConfigFile)) {
				return loadResolved(target)
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return nil, fmt.Errorf(
		"no Discord-tracking root found from %s\n"+
			"  rules:\n"+
			"    1. .discord-tracking.yaml at this or an ancestor directory\n"+
			"    2. a discord_tracking symlink that resolves to one\n"+
			"  see gap-tracker/plugins/gaps/reference/discord-tracking-playbook.md for setup",
		abs,
	)
}

func loadResolved(root string) (*Resolved, error) {
	raw, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ConfigFile, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ConfigFile, err)
	}
	if w := applyDefaults(&cfg); w != "" {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", filepath.Join(root, ConfigFile), w)
	}
	return &Resolved{Root: root, Config: cfg}, nil
}

// DefaultGapsDir is gaps_dir when neither it nor the legacy areas_dir is set.
const DefaultGapsDir = "proposals"

// applyDefaults fills unset fields. It returns a deprecation warning when
// the legacy areas_dir key supplied the gaps dir, empty otherwise.
func applyDefaults(c *Config) string {
	var warning string
	if c.ChannelsDir == "" {
		c.ChannelsDir = "channels"
	}
	if c.GapsDir == "" && c.LegacyAreasDir != "" {
		c.GapsDir = c.LegacyAreasDir
		warning = "areas_dir is deprecated, rename it to gaps_dir"
	}
	if c.GapsDir == "" {
		c.GapsDir = DefaultGapsDir
	}
	if c.SyncTool.Command == "" {
		c.SyncTool.Command = "mcpcontribd"
	}
	return warning
}

// ChannelsDir returns the absolute channels directory path.
func (r *Resolved) ChannelsDir() string {
	return filepath.Join(r.Root, r.Config.ChannelsDir)
}

// GapsDir returns the absolute gaps directory path.
func (r *Resolved) GapsDir() string {
	return filepath.Join(r.Root, r.Config.GapsDir)
}

// ChannelDir returns <gaps-dir>/<name>, the docs folder of a non-thread
// channel. Does NOT verify existence.
func (r *Resolved) ChannelDir(name string) string {
	return filepath.Join(r.GapsDir(), name)
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
