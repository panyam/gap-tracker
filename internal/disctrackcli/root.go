// Package disctrackcli is the cobra command tree for the disctrack binary.
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
//	  disctrack messages since <channel-name-or-id> [--ts ISO]
//	  disctrack cursor get <channel-name-or-id>
//	  disctrack cursor set <channel-name-or-id> <value>
//	  disctrack cursor list [channel-name-or-id]
//	  disctrack ingest [--from-file path]
//	  disctrack backup [--retain N]
//
// `disctrack sync` (Playwright orchestration) currently lives in mcpcontribd
// and will migrate in a later cut.
package disctrackcli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version metadata. Wired by the Makefile via -ldflags. Independent of gapt's
// to keep the two binaries versionable separately.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "disctrack",
	Short: "Discord-channel tracker — name lookups against .discord-tracking.yaml",
	Long: `disctrack does deterministic lookups against a project's tracked
Discord channels. Resolves the tracking root by walking up from cwd, looking
for .discord-tracking.yaml or a discord_tracking symlink that points at one.

See ~/.claude/discord-tracking-playbook.md for the layout + config schema.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute is the binary entrypoint.
func Execute() {
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
