// Package disctrackcli is the cobra command tree for the disctrack binary.
//
// All subcommands that need a tracking root call resolveTracking(), which
// honors the persistent -C / --root flag (start the resolver from a
// specific directory instead of cwd). Useful for cron jobs and multi-project
// scripts that can't rely on cwd.
//
// Subcommands:
//
//	Resolver / config lookups:
//	  disctrack config
//	  disctrack channel <name>           prints channel_id
//	  disctrack channels-dir             prints absolute channels directory
//	  disctrack gaps-dir                 prints absolute gaps directory
//	  disctrack dir <channel>            prints <gaps-dir>/<channel> (must exist)
//	  disctrack url <discord-url>        prints "<guild_id> <channel_id>"
//	  disctrack db-path                  prints <tracking-root>/messages.db
//
//	Message-DB CRUD (ported from mcpcontribd):
//	  disctrack messages <channel-name-or-id> [--since N|ISO] [-o fmt]
//	  disctrack cursor get <channel-name-or-id>
//	  disctrack cursor set <channel-name-or-id> <value>
//	  disctrack cursor list [channel-name-or-id]
//	  disctrack ingest [--from-file path]
//	  disctrack backup [--retain N]
//	  disctrack threads list <parent-channel-name-or-id> [-o fmt]
//
//	Onboarding:
//	  disctrack onboard <name> <discord-url> [--parent P] [--sync]
//	    Writes the channel manifest (+ <gaps-dir>/<name>/README.md skeleton
//	    for non-thread channels).
//
//	Scrape orchestration:
//	  disctrack sync <channel-name-or-id> [--max-iters N] [--no-threads]
//	    Drives python/snapshot.py, ingests, advances the cursor, then
//	    auto-tracks and syncs the channel's threads in one browser session.
package disctrackcli

import (
	"fmt"
	"os"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

// Version metadata. Wired by the Makefile via -ldflags. Independent of gapt's
// to keep the two binaries versionable separately.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// rootDir is the -C / --root flag: when set, the resolver starts here
// instead of cwd. Empty (default) falls back to cwd + $DISCTRACK_ROOT.
var rootDir string

// EnvVarRoot is the env var consulted as a last-resort fallback when the
// cwd-based resolver can't find a tracking root. Useful for code repos
// (e.g. mcpkit worktrees) where adding a discord_tracking symlink in every
// worktree is annoying.
const EnvVarRoot = "DISCTRACK_ROOT"

var rootCmd = &cobra.Command{
	Use:   "disctrack",
	Short: "Discord-channel tracker — name lookups against .discord-tracking.yaml",
	Long: `disctrack does deterministic lookups against a project's tracked
Discord channels. Resolves the tracking root by walking up from cwd, looking
for .discord-tracking.yaml or a discord_tracking symlink that points at one.

Pass -C / --root <path> to start the walk somewhere other than cwd (useful
for cron jobs and scripts that batch over multiple tracked projects).

When neither -C nor a cwd-based resolution succeeds, $DISCTRACK_ROOT is
consulted as a fallback. Set it in your shell rc to attach a default
tracking root for any cwd that doesn't have its own.

See gap-tracker/plugins/gaps/reference/discord-tracking-playbook.md for the layout + config schema.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// resolveTracking is the canonical way subcommands obtain a *Resolved.
//
// Precedence:
//  1. -C / --root <path> flag
//  2. cwd-based walk (.discord-tracking.yaml or discord_tracking symlink at
//     this or any ancestor directory)
//  3. $DISCTRACK_ROOT env var as a final fallback
//
// The env var skips the cwd walk only when it has already failed, so
// being inside a tracked project still wins.
func resolveTracking() (*disctrack.Resolved, error) {
	if rootDir != "" {
		return disctrack.ResolveFrom(rootDir)
	}
	r, err := disctrack.Resolve()
	if err == nil {
		return r, nil
	}
	if envRoot := os.Getenv(EnvVarRoot); envRoot != "" {
		return disctrack.ResolveFrom(envRoot)
	}
	return nil, err
}

// Execute is the binary entrypoint.
func Execute() {
	rootCmd.PersistentFlags().StringVarP(&rootDir, "root", "C", "", "Start the resolver here instead of cwd (path to a tracking root or a project with discord_tracking)")
	rootCmd.AddCommand(
		// Resolver / config lookups
		configCmd(),
		channelCmd(),
		channelsDirCmd(),
		gapsDirCmd(),
		dirCmd(),
		urlCmd(),
		dbPathCmd(),

		// Message-DB CRUD (ported from mcpcontribd)
		messagesCmd(),
		cursorCmd(),
		ingestCmd(),
		backupCmd(),
		threadsCmd(),

		// Onboarding + scrape orchestration
		onboardCmd(),
		syncCmd(),

		versionCmd(),
	)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print disctrack version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("disctrack %s (%s, %s)\n", Version, Commit, BuildDate)
		},
	}
}
