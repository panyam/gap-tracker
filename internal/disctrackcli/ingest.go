package disctrackcli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

var ingestFromFile string

func ingestCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest JSONL message records into the messages table",
		Long: `Reads JSONL records from --from-file (or stdin) and applies them
to the messages table.

For each (channel_id, message_id):
  - new row             → INSERT
  - existing, same content   → noop (duplicate, idempotent re-pull)
  - existing, different content → UPDATE content + last_edited_at

All work happens in a single transaction. Lookups are bulk pre-fetched
per channel, so one SELECT covers an entire batch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := disctrack.Resolve()
			if err != nil {
				return err
			}
			db, err := r.OpenDB()
			if err != nil {
				return err
			}
			var src io.Reader
			if ingestFromFile == "" || ingestFromFile == "-" {
				src = os.Stdin
			} else {
				f, err := os.Open(ingestFromFile)
				if err != nil {
					return fmt.Errorf("open %s: %w", ingestFromFile, err)
				}
				defer f.Close()
				src = f
			}
			stats, err := disctrack.Ingest(db, src)
			if err != nil {
				return err
			}
			b, _ := json.Marshal(stats)
			fmt.Println(string(b))
			return nil
		},
	}
	c.Flags().StringVar(&ingestFromFile, "from-file", "", "Path to JSONL input (defaults to stdin)")
	return c
}
