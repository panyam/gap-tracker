package disctrackcli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
)

var syncMaxIters int

func syncCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sync <channel-name-or-id>",
		Short: "Scrape new Discord messages for a channel into the local DB",
		Long: `Resolves the channel name → channel_id and invokes the configured
sync_tool (default mcpcontribd) to do the actual Playwright-driven scrape.

This is a thin orchestration wrapper: disctrack owns name resolution and DB
location, the sync_tool owns the browser session and message scraping. In a
later cut, disctrack will absorb the scrape orchestration directly and the
sync_tool indirection goes away.

Exit code and stdout/stderr from the sync_tool are passed through.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := disctrack.Resolve()
			if err != nil {
				return err
			}
			spec, err := r.ResolveChannelSpec(args[0])
			if err != nil {
				return err
			}
			tool := r.Config.SyncTool.Command
			if tool == "" {
				tool = "mcpcontribd"
			}
			binArgs := []string{"sync", spec.ChannelID}
			if syncMaxIters > 0 {
				binArgs = append(binArgs, "--max-iters", strconv.Itoa(syncMaxIters))
			}
			c := exec.Command(tool, binArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			c.Stdin = os.Stdin
			if err := c.Run(); err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					os.Exit(exitErr.ExitCode())
				}
				return fmt.Errorf("run %s sync: %w", tool, err)
			}
			return nil
		},
	}
	c.Flags().IntVar(&syncMaxIters, "max-iters", 0, "Max scroll iterations; passed through to sync_tool (0 = tool default)")
	return c
}
