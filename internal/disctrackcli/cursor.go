package disctrackcli

import (
	"encoding/json"
	"fmt"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

func cursorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cursor",
		Short: "Read or write cursor state",
	}
	cmd.AddCommand(cursorGetCmd(), cursorSetCmd(), cursorListCmd())
	return cmd
}

func cursorGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <channel-name-or-id>",
		Short: "Get the Discord cursor for a channel",
		Args:  cobra.ExactArgs(1),
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
			view, err := disctrack.GetDiscordCursor(db, spec.ChannelID, spec.Name)
			if err != nil {
				return err
			}
			b, _ := json.Marshal(view)
			fmt.Println(string(b))
			return nil
		},
	}
}

func cursorSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <channel-name-or-id> <cursor-value>",
		Short: "Set the Discord cursor for a channel",
		Args:  cobra.ExactArgs(2),
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
			if err := disctrack.SetDiscordCursor(db, spec.ChannelID, spec.Name, args[1]); err != nil {
				return err
			}
			fmt.Println(`{"ok": true}`)
			return nil
		},
	}
}

func cursorListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [channel-name-or-id]",
		Short: "List cursors, optionally filtered to one channel",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			db, err := r.OpenDB()
			if err != nil {
				return err
			}
			var sourceType, sourceKey string
			if len(args) == 1 {
				spec, err := r.ResolveChannelSpec(args[0])
				if err != nil {
					return err
				}
				sourceType, sourceKey = disctrack.SourceTypeDiscord, spec.ChannelID
			}
			rows, err := disctrack.ListCursors(db, sourceType, sourceKey)
			if err != nil {
				return err
			}
			out := make([]map[string]any, 0, len(rows))
			for _, c := range rows {
				out = append(out, map[string]any{
					"area":         c.Area,
					"source_type":  c.SourceType,
					"source_key":   c.SourceKey,
					"cursor_value": c.CursorValue,
					"updated_at":   c.UpdatedAt,
				})
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
}
