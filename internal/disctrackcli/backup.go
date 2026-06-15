package disctrackcli

import (
	"encoding/json"
	"fmt"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

var backupRetain int

func backupCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "backup",
		Short: "Snapshot messages.db into <tracking-root>/backups/",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := disctrack.Resolve()
			if err != nil {
				return err
			}
			result, err := r.Backup(backupRetain)
			if err != nil {
				return err
			}
			b, _ := json.Marshal(result)
			fmt.Println(string(b))
			return nil
		},
	}
	c.Flags().IntVar(&backupRetain, "retain", disctrack.BackupRetainDefault, "Number of backups to keep; older ones pruned")
	return c
}
