package disctrackcli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

// digitsOnlyCursorValue matches a positional cursor value that should be
// interpreted as N minutes ago (not as a literal cursor string). Anything
// containing dashes, colons, "T", or other ISO-8601 punctuation is passed
// through verbatim.
var digitsOnlyCursorValue = regexp.MustCompile(`^\d+$`)

// interpretCursorArg converts a user-supplied cursor value into the ISO 8601
// string stored in the cursors table.
//
//   - "60" / "1440" / "525600"  → minutes ago, converted to RFC3339 (UTC)
//   - "2026-06-01T00:00:00Z"    → used as-is
//   - "1970-01-01T00:00:00Z"    → effective "reset / backfill from epoch"
func interpretCursorArg(s string) string {
	if digitsOnlyCursorValue.MatchString(s) {
		minutes, _ := strconv.Atoi(s)
		return time.Now().UTC().Add(-time.Duration(minutes) * time.Minute).Format(time.RFC3339)
	}
	return s
}

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
		Use:   "set <channel-name-or-id> <value>",
		Short: "Set the Discord cursor for a channel",
		Long: `Sets the cursor row for source_type=discord, source_key=<channel-id>.

<value> is interpreted two ways:

  - All-digits (e.g. 60, 1440, 525600) → N minutes ago, expanded to
    an RFC3339 UTC timestamp at write time. Mirrors --since N on sync.
      disctrack cursor set general 60        # cursor = 60 minutes ago
      disctrack cursor set general 525600    # cursor = ~1 year ago

  - Anything else (ISO 8601 / arbitrary string) → used verbatim.
      disctrack cursor set general 2026-06-01T00:00:00Z
      disctrack cursor set general 1970-01-01T00:00:00Z   # effective full backfill

The next disctrack sync will scroll back until reaching this value.`,
		Args: cobra.ExactArgs(2),
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
			value := interpretCursorArg(args[1])
			if err := disctrack.SetDiscordCursor(db, spec.ChannelID, spec.Name, value); err != nil {
				return err
			}
			b, _ := json.Marshal(map[string]any{"ok": true, "channel": spec.Name, "cursor_value": value})
			fmt.Println(string(b))
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
					"channel":      c.ChannelName,
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
