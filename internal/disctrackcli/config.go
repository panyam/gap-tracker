package disctrackcli

import (
	"fmt"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

func configCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print the resolved tracking root and key config fields",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := disctrack.Resolve()
			if err != nil {
				return err
			}
			c := r.Config
			fmt.Printf("tracking_root:    %s\n", r.Root)
			fmt.Printf("channels_dir:     %s\n", r.ChannelsDir())
			fmt.Printf("areas_dir:        %s\n", r.AreasDir())
			fmt.Printf("code.name:        %s\n", c.Code.Name)
			fmt.Printf("code.repo:        %s\n", c.Code.Repo)
			fmt.Printf("code.worktree:    %s\n", c.Code.DefaultWorktree)
			fmt.Printf("discord.guild_id: %s\n", c.Discord.GuildID)
			fmt.Printf("sync_tool:        %s\n", c.SyncTool.Command)
			fmt.Printf("gapt.enabled:     %t\n", c.Gapt.Enabled)
			return nil
		},
	}
}
