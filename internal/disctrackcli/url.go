package disctrackcli

import (
	"fmt"
	"regexp"

	"github.com/spf13/cobra"
)

// discordChannelURL matches https://discord.com/channels/<guild>/<channel>
// and the discordapp.com variant. Trailing path segments (thread/message id)
// are tolerated.
var discordChannelURL = regexp.MustCompile(`^https?://(?:discord|discordapp)\.com/channels/(\d+)/(\d+)`)

func urlCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "url <discord-url>",
		Short: `Parse a Discord channel URL; prints "<guild_id> <channel_id>"`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m := discordChannelURL.FindStringSubmatch(args[0])
			if m == nil {
				return fmt.Errorf("not a Discord channel URL: %s", args[0])
			}
			fmt.Printf("%s %s\n", m[1], m[2])
			return nil
		},
	}
}
