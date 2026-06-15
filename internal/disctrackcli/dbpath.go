package disctrackcli

import (
	"fmt"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

func dbPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "db-path",
		Short: "Print the absolute path of messages.db for this tracking root",
		Long: `Useful for other tools (e.g. mcpcontribd) that need to discover
where disctrack will read/write its SQLite database, without re-implementing
the resolver themselves.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := disctrack.Resolve()
			if err != nil {
				return err
			}
			fmt.Println(r.DBFile())
			return nil
		},
	}
}
