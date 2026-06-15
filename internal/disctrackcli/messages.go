package disctrackcli

import (
	"encoding/json"
	"fmt"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

var messagesSinceTS string

func messagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "Query synced Discord messages",
	}
	cmd.AddCommand(messagesSinceCmd())
	return cmd
}

func messagesSinceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "since <channel-name-or-id>",
		Short: "Messages strictly newer than --ts, ordered by ts asc",
		Long: `Returns a JSON array of messages for the channel.

<channel-name-or-id> resolves like every other disctrack command: all-digits
is treated as an ID, otherwise looked up via the resolver's channels dir.

Omit --ts to fetch every synced message for the channel.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := disctrack.Resolve()
			if err != nil {
				return err
			}
			spec, err := r.ResolveChannelSpec(args[0])
			if err != nil {
				return err
			}
			db, err := r.OpenDB()
			if err != nil {
				return err
			}
			rows, err := disctrack.MessagesSince(db, spec.ChannelID, messagesSinceTS)
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(rows, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		},
	}
	c.Flags().StringVar(&messagesSinceTS, "ts", "", "ISO8601 timestamp; messages strictly newer than this are returned (omit for all)")
	return c
}
