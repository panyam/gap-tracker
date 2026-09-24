package disctrackcli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func dirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dir <channel>",
		Short: "Print the absolute docs folder of a tracked channel (<gaps-dir>/<name>, must exist)",
		Long: `Prints <gaps-dir>/<name> for a tracked non-thread channel. <channel> is a
manifest name or a channel id. Fails when the channel is untracked, when it
is a thread (threads read under their parent's folder), or when the folder
does not exist yet.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			dir, err := r.LookupChannelDir(args[0])
			if err != nil {
				return err
			}
			fmt.Println(dir)
			return nil
		},
	}
}

func gapsDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gaps-dir",
		Short: "Print the absolute gaps directory path (one folder per channel)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			fmt.Println(r.GapsDir())
			return nil
		},
	}
}
