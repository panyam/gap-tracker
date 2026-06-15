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
	return cmd
}

func threadsListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "list <parent-channel-name-or-id>",
		Short: "List threads whose parent_channel matches the argument",
		Long: `Scans <channels-dir>/*.yaml and prints every manifest with
kind == "thread" and parent_channel matching the argument (either by name or
by channel_id).

Live discovery (Playwright-driven scrape of Discord's thread panel) is not
implemented yet — for now you add a thread by writing its channel.yaml by
hand:

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
