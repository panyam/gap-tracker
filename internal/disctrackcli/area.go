package disctrackcli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func areaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "area <name>",
		Short: "Print the absolute area folder path (must exist)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			name := args[0]
			path := r.AreaPath(name)
			st, err := os.Stat(path)
			if err != nil || !st.IsDir() {
				msg := fmt.Sprintf("no area folder at %s", path)
				if avail := r.ListAreas(); len(avail) > 0 {
					msg += "\navailable: " + strings.Join(avail, ", ")
				}
				return fmt.Errorf("%s", msg)
			}
			fmt.Println(path)
			return nil
		},
	}
}

func areasDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "areas-dir",
		Short: "Print the absolute areas directory path",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			fmt.Println(r.AreasDir())
			return nil
		},
	}
}
