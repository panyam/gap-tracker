package disctrackcli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

var (
	syncSinceMinutes   int
	syncMaxIters       int
	syncNoBackup       bool
	syncDryRun         bool
	syncHeadless       bool
	syncProfile        string
	syncProfileDir     string
	syncScriptOverride string
)

func syncCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sync <channel-name-or-id>",
		Short: "Sync a Discord channel into the local DB (drives snapshot.py)",
		Long: `disctrack sync is the end-to-end scrape verb. It reads the channel
cursor from the local DB (or honors --since), spawns the Playwright scraper
(default: python/snapshot.py from this repo), ingests the resulting JSONL
into the messages table, and advances the cursor on success.

snapshot.py path resolution (first hit wins):
  1. --script <path>
  2. discord.snapshot_script in .discord-tracking.yaml
  3. ldflag-baked default (set by 'make install-disctrack')
  4. python/snapshot.py relative to cwd

profile_dir resolution (the Chromium user-data-dir that holds the Discord
login session):
  1. --profile-dir <path>
  2. discord.profile_dir in .discord-tracking.yaml
  3. <tracking-root>/profiles/default (auto-created)

The browser launches headed by default so an expired login is visible. Pass
--headless once the profile is known-good.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			spec, err := r.ResolveChannelSpec(args[0])
			if err != nil {
				return err
			}
			// --since N minutes → ISO timestamp. 0 (default) leaves Since
			// empty so Sync() falls back to the cursor in the DB.
			since := ""
			if syncSinceMinutes > 0 {
				cutoff := time.Now().UTC().Add(-time.Duration(syncSinceMinutes) * time.Minute)
				since = cutoff.Format(time.RFC3339)
			}
			opts := disctrack.SyncOptions{
				Channel:    *spec,
				Since:      since,
				MaxIters:   syncMaxIters,
				NoBackup:   syncNoBackup,
				DryRun:     syncDryRun,
				Headless:   syncHeadless,
				Profile:    syncProfile,
				ProfileDir: syncProfileDir,
			}
			result, err := r.Sync(opts, syncScriptOverride)
			if err != nil {
				return err
			}
			b, _ := json.Marshal(result)
			fmt.Println(string(b))
			return nil
		},
	}
	c.Flags().IntVar(&syncSinceMinutes, "since", 0, "Sync messages from the last N minutes (0 = use cursor from DB)")
	c.Flags().IntVar(&syncMaxIters, "max-iters", 0, "Max scroll iterations (0 = snapshot.py default)")
	c.Flags().BoolVar(&syncNoBackup, "no-backup", false, "Skip pre-sync DB snapshot")
	c.Flags().BoolVar(&syncDryRun, "dry-run", false, "Run snapshot but don't write to DB")
	c.Flags().BoolVar(&syncHeadless, "headless", false, "Run Chromium without a visible window")
	c.Flags().StringVar(&syncProfile, "profile", "", "Named profile under the snapshot.py-side default base (snapshot.py interprets)")
	c.Flags().StringVar(&syncProfileDir, "profile-dir", "", "Explicit Chromium user-data-dir (overrides config + --profile)")
	c.Flags().StringVar(&syncScriptOverride, "script", "", "Override the snapshot.py path (debug)")
	return c
}
