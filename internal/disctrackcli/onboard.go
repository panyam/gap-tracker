package disctrackcli

import (
	"fmt"
	"strings"
	"time"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

var (
	onboardParent   string
	onboardDryRun   bool
	onboardSync     bool
	onboardSince    int
	onboardHeadless bool
	onboardMaxIters int
)

func onboardCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "onboard <name> <discord-url>",
		Short: "Start tracking a Discord channel or thread: write its manifest and docs folder",
		Long: `Registers a Discord channel (or thread) under <name>. Writes
<channels-dir>/<name>.yaml and, for a non-thread channel, creates
<gaps-dir>/<name>/ with a heading-only README.md if the folder does not
exist yet. Every tracked channel owns exactly that one folder.

<discord-url> is what Discord's "Copy Link" gives you:

  https://discord.com/channels/<guild>/<channel>
  https://discord.com/channels/<guild>/<parent>/threads/<thread>

A bare channel id is accepted too. The URL's guild must match
discord.guild_id in .discord-tracking.yaml.

A /threads/ URL writes a thread manifest (kind: thread) whose parent_channel
is the tracked manifest for the parent id. The parent must already be
tracked. For a thread URL without the parent segment, or a bare thread id,
pass --parent <name>. Threads get no docs folder: they read under their
parent's. Once a parent is tracked, 'disctrack sync <parent>' tracks and
syncs its new threads on its own, so onboarding threads by hand is rarely
needed.

onboard refuses when <name>.yaml exists or when another manifest already
tracks the channel id. It never overwrites a file.

--sync runs 'disctrack sync <name>' right after writing, with --since,
--headless and --max-iters passed through. --dry-run prints the manifest and
paths without writing anything or opening the browser.

  disctrack onboard mcp-apps-wg https://discord.com/channels/1358869848138059966/1376635661989449820
  disctrack onboard mcp-apps-wg-pip-mode-thread https://discord.com/channels/<g>/<parent>/threads/<t>
  disctrack onboard gateways-ig <url> --sync --headless --since 40320`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			if onboardDryRun && onboardSync {
				return fmt.Errorf("--dry-run and --sync are exclusive: a dry run writes no manifest to sync")
			}
			plan, err := r.PlanOnboard(disctrack.OnboardOptions{
				Name:   args[0],
				Ref:    args[1],
				Parent: onboardParent,
			})
			if err != nil {
				return err
			}
			if onboardDryRun {
				printOnboardPlan(plan, true)
				return nil
			}
			if err := r.ApplyOnboard(plan); err != nil {
				return err
			}
			printOnboardPlan(plan, false)

			if !onboardSync {
				fmt.Printf("\nnext step: disctrack sync %s\n", plan.Name)
				if plan.Kind != disctrack.ChannelKindThread {
					fmt.Printf("then:      /gaps channel %s\n", plan.Name)
				}
				return nil
			}

			fmt.Println()
			since := ""
			if onboardSince > 0 {
				since = time.Now().UTC().Add(-time.Duration(onboardSince) * time.Minute).Format(time.RFC3339)
			}
			opts := disctrack.SyncOptions{
				Channel:  disctrack.ChannelSpec{ChannelID: plan.ChannelID, Name: plan.Name},
				Since:    since,
				MaxIters: onboardMaxIters,
				Headless: onboardHeadless,
			}
			return runSync(r, opts, "", true)
		},
	}
	c.Flags().StringVar(&onboardParent, "parent", "", "Tracked parent channel (name or id); makes this a thread")
	c.Flags().BoolVar(&onboardDryRun, "dry-run", false, "Print the manifest and paths; write nothing")
	c.Flags().BoolVar(&onboardSync, "sync", false, "Run 'disctrack sync <name>' after writing")
	c.Flags().IntVar(&onboardSince, "since", 0, "With --sync: sync messages from the last N minutes (0 = cold start)")
	c.Flags().BoolVar(&onboardHeadless, "headless", false, "With --sync: run Chromium without a visible window")
	c.Flags().IntVar(&onboardMaxIters, "max-iters", 0, "With --sync: max scroll iterations (0 = snapshot.py default)")
	return c
}

func printOnboardPlan(plan *disctrack.OnboardPlan, dryRun bool) {
	verb := "wrote"
	if dryRun {
		verb = "would write"
	}
	fmt.Printf("%s channel manifest: %s\n", verb, plan.ManifestPath)
	switch {
	case plan.Kind == disctrack.ChannelKindThread:
		fmt.Printf("thread of:          %s (threads read under the parent's folder)\n", plan.ParentName)
	case plan.ChannelDirCreate:
		verb := "created"
		if dryRun {
			verb = "would create"
		}
		fmt.Printf("%s docs folder: %s (README.md skeleton)\n", verb, plan.ChannelDir)
	default:
		fmt.Printf("docs folder:        %s (exists, left as is)\n", plan.ChannelDir)
	}
	fmt.Println()
	fmt.Print(indent(plan.Manifest, "  "))
}

func indent(s, prefix string) string {
	lines := strings.SplitAfter(s, "\n")
	var b strings.Builder
	for _, l := range lines {
		if l == "" {
			continue
		}
		if l != "\n" {
			b.WriteString(prefix)
		}
		b.WriteString(l)
	}
	return b.String()
}
