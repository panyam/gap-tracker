// Package socialtrackcli is the cobra command tree for the socialtrack binary.
//
//	socialtrack today [--topic t]       sync, enrich, then the digest
//	socialtrack sync                    read every subreddit any topic names, store new matches
//	socialtrack enrich [-n N]           check reply counts of the top N candidates
//	socialtrack digest [--topic t]      the threads worth a reply now, best first
//	socialtrack ls [--topic t] [--status s|-a] [--hours N]   stored threads with short ids
//	socialtrack open <n|id>             open a thread in the browser (n = digest rank)
//	socialtrack show <n|id>             a thread's post and comments, from its feed
//	socialtrack add <url> [--title t] [--topic t]   a thread found by hand, from any network
//	socialtrack mark <n|id> replied|skipped [--note text]
//	socialtrack streak                  replies per day and the current streak
//	socialtrack notify                  a macOS notification summing up the digest
//	socialtrack config                  the resolved root and config
//
// Every command resolves the tracking root from -C (or cwd), then
// $SOCIALTRACK_ROOT.
package socialtrackcli

import (
	"fmt"
	"os"

	"github.com/panyam/gap-tracker/internal/socialtrack"
	"github.com/spf13/cobra"
)

// Version metadata, wired by the Makefile via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var rootDir string

func resolve() (*socialtrack.Resolved, error) {
	start := rootDir
	if start == "" {
		var err error
		if start, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	return socialtrack.ResolveFrom(start)
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "socialtrack",
		Short:         "Find threads worth replying to, and keep a reply habit",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (%s, %s)", Version, Commit, BuildDate),
	}
	root.PersistentFlags().StringVarP(&rootDir, "root", "C", "", "start resolving the tracking root here instead of cwd")
	root.AddCommand(todayCmd(), syncCmd(), enrichCmd(), digestCmd(), lsCmd(), openCmd(), showCmd(), addCmd(), markCmd(), streakCmd(), notifyCmd(), configCmd())
	return root
}

// Execute runs the command tree and exits non-zero on error.
func Execute() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "socialtrack:", err)
		os.Exit(1)
	}
}
