package disctrackcli

import (
	"fmt"
	"strings"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

func urlCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "url <discord-url>",
		Short: `Parse a Discord channel URL; prints "<guild_id> <channel_id>"`,
		Long: `Parses a Discord channel or thread URL and prints "<guild_id> <channel_id>".

For a thread URL (.../channels/<guild>/<parent>/threads/<thread>) the
channel_id printed is the thread's own id, since that is the id you track and
sync. A trailing message id is ignored.

  disctrack url https://discord.com/channels/1358869848138059966/1376635661989449820`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !strings.Contains(args[0], "/channels/") {
				return fmt.Errorf("not a Discord channel URL: %s", args[0])
			}
			ref, err := disctrack.ParseDiscordURL(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("%s %s\n", ref.GuildID, ref.ChannelID)
			return nil
		},
	}
}
