package disctrackcli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func channelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "channel <name>",
		Short: "Print the channel_id from <channels-dir>/<name>.yaml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			name := args[0]
			m, available, err := r.LoadChannel(name)
			if err != nil {
				return err
			}
			if m == nil {
				msg := fmt.Sprintf("no channel manifest at %s/%s.yaml", r.ChannelsDir(), name)
				if len(available) > 0 {
					msg += "\navailable: " + strings.Join(available, ", ")
				}
				return fmt.Errorf("%s", msg)
			}
			if m.ChannelID == "" {
				return fmt.Errorf("%s/%s.yaml has no channel_id field", r.ChannelsDir(), name)
			}
			fmt.Println(m.ChannelID)
			return nil
		},
	}
}

func channelsDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "channels-dir",
		Short: "Print the absolute channels directory path",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			fmt.Println(r.ChannelsDir())
			return nil
		},
	}
}
