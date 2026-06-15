package disctrackcli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

var messagesSince string

func messagesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "messages <channel-name-or-id> [--since N]",
		Short: "Query synced Discord messages for a channel",
		Long: `Returns a JSON array of messages for the channel, ordered by ts asc.

<channel-name-or-id> resolves like every other disctrack command: all-digits
is treated as an ID, otherwise looked up via the resolver's channels dir.

--since accepts the same dual form as ` + "`cursor set`" + `:
  - All-digits (e.g. 60, 1440)  → N minutes ago
  - Anything else (ISO 8601)    → used verbatim

Omit --since to fetch every synced message for the channel.

When the result is empty, an explanatory stderr line is emitted so callers
know whether the channel is genuinely quiet or just stale (no sync since
the requested timestamp).`,
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
			db, err := r.OpenDB()
			if err != nil {
				return err
			}
			since := ""
			if messagesSince != "" {
				since = interpretCursorArg(messagesSince)
			}
			rows, err := disctrack.MessagesSince(db, spec.ChannelID, since)
			if err != nil {
				return err
			}
			// Empty result is ambiguous — could be a quiet channel or a stale
			// DB. Emit a stderr hint so the skill (or human) knows the
			// difference. JSON pipe on stdout stays clean.
			if len(rows) == 0 {
				cursor, _ := disctrack.GetDiscordCursor(db, spec.ChannelID, spec.Name)
				label := spec.Name
				if label == "" {
					label = spec.ChannelID
				}
				if !cursor.Found {
					fmt.Fprintf(os.Stderr,
						"warn: %s has no sync history. Run `disctrack sync %s` to bootstrap.\n",
						label, label)
				} else {
					sinceMsg := "any time"
					if since != "" {
						sinceMsg = since
					}
					fmt.Fprintf(os.Stderr,
						"warn: 0 messages since %s. Last synced: %s (cursor: %s). Run `disctrack sync %s` if you expect newer messages.\n",
						sinceMsg, cursor.UpdatedAt, cursor.CursorValue, label)
				}
			}
			b, err := json.MarshalIndent(rows, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		},
	}
	c.Flags().StringVar(&messagesSince, "since", "", "Filter to messages strictly newer than: N (minutes ago) or an ISO8601 timestamp")
	return c
}
