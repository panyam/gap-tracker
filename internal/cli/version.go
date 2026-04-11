package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Populated at build time via -ldflags "-X github.com/panyam/gap-tracker/internal/cli.Version=..."
// Default values keep dev builds legible when ldflags aren't set.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print gapt version, commit, and build date",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"gapt %s (commit %s, built %s)\n", Version, Commit, BuildDate)
			return err
		},
	}
}
