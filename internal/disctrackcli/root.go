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
//	  disctrack area <name>              prints absolute area folder path
//	  disctrack areas-dir                prints absolute areas directory
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
//	Scrape orchestration:
//	  disctrack sync <channel-name-or-id> [--max-iters N]
//	    Thin wrapper over the configured sync_tool (default mcpcontribd).
//	    In a later cut disctrack absorbs the Playwright orchestration and
//	    sync_tool retires.
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
// instead of cwd. Empty (default) falls back to cwd.
var rootDir string

var rootCmd = &cobra.Command{
	Use:   "disctrack",
	Short: "Discord-channel tracker — name lookups against .discord-tracking.yaml",
	Long: `disctrack does deterministic lookups against a project's tracked
Discord channels. Resolves the tracking root by walking up from cwd, looking
for .discord-tracking.yaml or a discord_tracking symlink that points at one.

Pass -C / --root <path> to start the walk somewhere other than cwd (useful
for cron jobs and scripts that batch over multiple tracked projects).

See ~/.claude/discord-tracking-playbook.md for the layout + config schema.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// resolveTracking is the canonical way subcommands obtain a *Resolved. It
// honors the persistent -C / --root flag.
func resolveTracking() (*disctrack.Resolved, error) {
	if rootDir != "" {
		return disctrack.ResolveFrom(rootDir)
	}
	return disctrack.Resolve()
}

// Execute is the binary entrypoint.
func Execute() {
	rootCmd.PersistentFlags().StringVarP(&rootDir, "root", "C", "", "Start the resolver here instead of cwd (path to a tracking root or a project with discord_tracking)")
	rootCmd.AddCommand(
		// Resolver / config lookups
		configCmd(),
		channelCmd(),
		channelsDirCmd(),
		areaCmd(),
		areasDirCmd(),
		urlCmd(),
		dbPathCmd(),

		// Message-DB CRUD (ported from mcpcontribd)
		messagesCmd(),
		cursorCmd(),
		ingestCmd(),
		backupCmd(),
		threadsCmd(),

		// Scrape orchestration (thin wrapper over sync_tool for now)
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
