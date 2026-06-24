package disctrackcli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var threadsListOutput string

func threadsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "threads",
		Short: "List Discord threads tracked locally",
	}
	cmd.AddCommand(threadsListCmd())
	cmd.AddCommand(threadsDiscoverCmd())
	return cmd
}

var (
	threadsDiscoverOutput    string
	threadsDiscoverUntracked bool
)

func threadsDiscoverCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "discover <parent-channel-name-or-id>",
		Short: "Find thread-start markers in a synced channel and flag which are untracked",
		Long: `Scans the synced messages of a parent channel for "started a thread"
markers and joins them against the tracked thread manifests, so you can see
which threads still need a channel.yaml + sync.

The thread's own channel_id (the value you put in the manifest and pass to
'disctrack sync') is captured from the marker's thread anchor by snapshot.py.
Marker rows ingested before that capture landed show id_known: false — re-sync
the parent channel once to backfill the id, then re-run discover.

This reads the local DB only; it never opens the browser. Run
'disctrack sync <parent>' first if the channel is stale.

  disctrack threads discover triggers-events-wg
  disctrack threads discover triggers-events-wg --untracked -o pretty`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if threadsDiscoverOutput != msgOutputYAML &&
				threadsDiscoverOutput != msgOutputJSON &&
				threadsDiscoverOutput != msgOutputPretty {
				return fmt.Errorf("unknown -o value: %q (valid: yaml, json, pretty)", threadsDiscoverOutput)
			}
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			db, err := r.OpenDB()
			if err != nil {
				return err
			}
			threads, err := r.DiscoverThreads(db, args[0])
			if err != nil {
				return err
			}
			disctrack.SortThreadsByTS(threads)
			if threadsDiscoverUntracked {
				kept := threads[:0]
				for _, t := range threads {
					if !t.Tracked {
						kept = append(kept, t)
					}
				}
				threads = kept
			}
			return emitDiscoveredThreads(threads, threadsDiscoverOutput)
		},
	}
	c.Flags().StringVarP(&threadsDiscoverOutput, "output", "o", msgOutputYAML, "Output format: yaml | json | pretty")
	c.Flags().BoolVar(&threadsDiscoverUntracked, "untracked", false, "Show only threads without a tracked manifest")
	return c
}

func emitDiscoveredThreads(threads []disctrack.DiscoveredThread, format string) error {
	switch format {
	case msgOutputJSON:
		b, err := json.MarshalIndent(threads, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	case msgOutputPretty:
		if len(threads) == 0 {
			fmt.Println("(no thread markers found — sync the parent channel first?)")
			return nil
		}
		for i, t := range threads {
			if i > 0 {
				fmt.Println()
			}
			status := "UNTRACKED"
			if t.Tracked {
				status = "tracked -> " + t.Manifest
			}
			fmt.Printf("%s  %s\n", t.TS, t.Title)
			fmt.Printf("  status:     %s\n", status)
			if t.StartedBy != "" {
				fmt.Printf("  started_by: %s\n", t.StartedBy)
			}
			if t.IDKnown {
				fmt.Printf("  thread_id:  %s\n", t.ThreadID)
			} else {
				fmt.Printf("  thread_id:  (unknown — re-sync the parent to capture it)\n")
			}
			fmt.Printf("  marker:     %s\n", t.MarkerMessageID)
		}
	default: // yaml
		view := make([]map[string]any, 0, len(threads))
		for _, t := range threads {
			row := map[string]any{
				"title":             t.Title,
				"ts":                t.TS,
				"tracked":           t.Tracked,
				"id_known":          t.IDKnown,
				"marker_message_id": t.MarkerMessageID,
			}
			if t.ThreadID != "" {
				row["thread_id"] = t.ThreadID
			}
			if t.StartedBy != "" {
				row["started_by"] = t.StartedBy
			}
			if t.Manifest != "" {
				row["manifest"] = t.Manifest
			}
			view = append(view, row)
		}
		b, err := yaml.Marshal(view)
		if err != nil {
			return err
		}
		out := strings.TrimRight(string(b), "\n")
		if out == "" {
			fmt.Println("[]")
		} else {
			fmt.Println(out)
		}
	}
	return nil
}

func threadsListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "list <parent-channel-name-or-id>",
		Short: "List threads whose parent_channel matches the argument",
		Long: `Scans <channels-dir>/*.yaml and prints every manifest with
kind == "thread" and parent_channel matching the argument (either by name or
by channel_id).

This lists threads you have ALREADY tracked. To find threads in a channel that
you have not tracked yet, use 'disctrack threads discover <parent>', which
scans the synced messages for "started a thread" markers and reports each
thread's channel_id.

You add a thread by writing its channel.yaml by hand:

  name: my-thread
  channel_id: "1234567890"
  kind: thread
  parent_channel: strategy

Then 'disctrack threads list strategy' will include it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if threadsListOutput != msgOutputYAML &&
				threadsListOutput != msgOutputJSON &&
				threadsListOutput != msgOutputPretty {
				return fmt.Errorf("unknown -o value: %q (valid: yaml, json, pretty)", threadsListOutput)
			}
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			threads, err := r.ListThreads(args[0])
			if err != nil {
				return err
			}
			return emitThreads(threads, threadsListOutput)
		},
	}
	c.Flags().StringVarP(&threadsListOutput, "output", "o", msgOutputYAML, "Output format: yaml | json | pretty")
	return c
}

func emitThreads(threads []disctrack.ChannelManifest, format string) error {
	switch format {
	case msgOutputJSON:
		b, err := json.MarshalIndent(threads, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	case msgOutputPretty:
		if len(threads) == 0 {
			fmt.Println("(no threads tracked)")
			return nil
		}
		for i, t := range threads {
			if i > 0 {
				fmt.Println()
			}
			displayName := t.DisplayName
			if displayName == "" {
				displayName = t.Name
			}
			fmt.Printf("%s\n", t.Name)
			fmt.Printf("  channel_id:     %s\n", t.ChannelID)
			fmt.Printf("  display_name:   %s\n", displayName)
			fmt.Printf("  parent_channel: %s\n", t.ParentChannel)
			if t.Status != "" {
				fmt.Printf("  status:         %s\n", t.Status)
			}
		}
	default: // yaml
		// Marshal only the fields relevant to "threads list" so the output
		// stays scannable. The full ChannelManifest carries some
		// channel-management fields ("area_folder", legacy aliases) that
		// aren't meaningful here.
		view := make([]map[string]any, 0, len(threads))
		for _, t := range threads {
			row := map[string]any{
				"name":           t.Name,
				"channel_id":     t.ChannelID,
				"parent_channel": t.ParentChannel,
			}
			if t.DisplayName != "" && t.DisplayName != t.Name {
				row["display_name"] = t.DisplayName
			}
			if t.Status != "" {
				row["status"] = t.Status
			}
			view = append(view, row)
		}
		b, err := yaml.Marshal(view)
		if err != nil {
			return err
		}
		out := strings.TrimRight(string(b), "\n")
		if out == "" {
			fmt.Println("[]")
		} else {
			fmt.Println(out)
		}
	}
	return nil
}
