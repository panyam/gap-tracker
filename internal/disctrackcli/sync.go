package disctrackcli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	syncNoThreads      bool
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

snapshot.py runs via 'uv run' from its own directory with VIRTUAL_ENV
unset, so a .venv (or an activated venv) where you run disctrack cannot
change the interpreter uv picks.

profile_dir resolution (the Chromium user-data-dir that holds the Discord
login session):
  1. --profile-dir <path>
  2. discord.profile_dir in .discord-tracking.yaml
  3. <tracking-root>/profiles/default (auto-created)

The browser launches headed by default so an expired login is visible. Pass
--headless once the profile is known-good.

Threads are synced too. After the parent channel syncs, disctrack scans it
for "started a thread" markers and writes a thread manifest for every thread
that has none (named <parent>-<title-slug>-thread, kind: thread). Then it
syncs every active thread of the parent, new and previously tracked, in one
browser session. Each thread keeps its own cursor. A new thread starts a few
minutes before its marker (or at --since, if later).

A thread that fails is reported and skipped. It never fails the parent or
the other threads. Output is one JSON line for the parent, one per thread,
and a closing {"summary": ...} line with totals.

Pass --no-threads to sync only the named channel. Syncing a thread manifest
by name syncs just that thread.

  disctrack sync mcp-apps-wg --headless
  disctrack sync mcp-apps-wg --no-threads
  disctrack sync mcp-apps-wg-css-variables-thread`,
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
			return runSync(r, opts, syncScriptOverride, !syncNoThreads)
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
	c.Flags().BoolVar(&syncNoThreads, "no-threads", false, "Sync only this channel; skip thread discovery, auto-tracking and thread sync")
	return c
}

// runSync is the sync pipeline shared by `sync` and `onboard --sync`: the
// channel itself, then (when withThreads and the channel is not a thread)
// its threads. The parent's JSON line prints before the thread phase starts
// so a thread problem can never hide the parent's result.
func runSync(r *disctrack.Resolved, opts disctrack.SyncOptions, scriptOverride string, withThreads bool) error {
	result, err := r.Sync(opts, scriptOverride)
	if err != nil {
		return err
	}
	printJSONLine(result)
	if !withThreads {
		return nil
	}

	if opts.Channel.Name == "" {
		fmt.Fprintln(os.Stderr, "threads: skipped (no manifest for this channel id, so auto-tracked threads would have no parent name)")
		return nil
	}
	m, _, err := r.LoadChannel(opts.Channel.Name)
	if err != nil || m == nil {
		return reportThreadPhaseError(opts.Channel.Name, fmt.Errorf("load manifest: %v", err))
	}
	if m.IsThread() {
		return nil // Discord threads do not nest
	}

	db, err := r.OpenDB()
	if err != nil {
		return reportThreadPhaseError(opts.Channel.Name, err)
	}
	scraper, err := r.NewSnapshotBatchScraper(opts.ProfileDir, scriptOverride, opts.MaxIters, opts.Headless)
	if err != nil {
		return reportThreadPhaseError(opts.Channel.Name, err)
	}
	rep, err := r.SyncThreads(db, disctrack.ThreadSyncOptions{
		Parent:      opts.Channel,
		ParentSince: opts.Since,
		DryRun:      opts.DryRun,
	}, scraper)
	if err != nil {
		return reportThreadPhaseError(opts.Channel.Name, err)
	}
	emitThreadReport(os.Stdout, rep, opts.Channel.Name, opts.DryRun)
	return nil
}

// emitThreadReport prints one JSON line per thread, then the totals line,
// and warns on stderr when any thread failed.
func emitThreadReport(w io.Writer, rep disctrack.ThreadSyncReport, parent string, dryRun bool) {
	for _, th := range rep.Threads {
		b, _ := json.Marshal(th)
		fmt.Fprintln(w, string(b))
	}
	totals := rep.Totals(parent, dryRun)
	b, _ := json.Marshal(map[string]any{"summary": totals})
	fmt.Fprintln(w, string(b))
	if totals.Failed > 0 {
		fmt.Fprintf(os.Stderr, "warn: %d of %d thread(s) of %s failed; the parent sync and the other threads are unaffected. Re-run 'disctrack sync <thread-name>' to retry one.\n",
			totals.Failed, totals.Threads, parent)
	}
	if totals.Unresolved > 0 {
		fmt.Fprintf(os.Stderr, "note: %d thread marker(s) in %s have no captured thread id; the next parent sync usually resolves them ('disctrack threads discover %s --untracked').\n",
			totals.Unresolved, parent, parent)
	}
}

// reportThreadPhaseError keeps a thread-phase setup failure from failing a
// parent sync that already succeeded: warn, print an empty summary, exit 0.
func reportThreadPhaseError(parent string, err error) error {
	fmt.Fprintf(os.Stderr, "warn: thread sync for %s did not run: %v\n", parent, err)
	printJSONLine(map[string]any{"summary": map[string]any{"parent": parent, "threads": 0, "error": err.Error()}})
	return nil
}

func printJSONLine(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}
