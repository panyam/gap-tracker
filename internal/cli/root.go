// Package cli builds the cobra command tree for gapt.
//
// Layout:
//
//	gapt                 root (persistent --output flag)
//	├── version          print build info
//	├── sync             (M3)
//	├── sync-all         (M3)
//	├── show             (M2)
//	├── triage           (M2/M4 — subcommand group)
//	│   ├── status
//	│   ├── list
//	│   ├── set
//	│   ├── unset
//	│   ├── close
//	│   ├── export
//	│   ├── import
//	│   └── verify
//	└── (legacy: link, mark, report, note, notes, crossref, reconcile — M6)
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// Globals populated by root flags. Subcommands read these via package-level
// accessors instead of carrying config through every signature.
var (
	rootDir    string
	project    string
	outputMode string
)

// NewRootCmd builds the top-level gapt command. Separate constructor so
// tests can instantiate a fresh tree and inject fakes later.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gapt",
		Short: "gap-tracker CLI: classify upstream issues against a local project",
		Long: `gapt tracks issues from upstream reference repos ("sources") that might
map to missing capabilities in our own project ("target").

The SQLite DB under <root>/<project>/gaps.db is the source of truth. YAML
snapshots are export-on-demand via 'triage export'. See docs/STATE_MACHINE.md
for the full flow model and developer entry points.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	defaultRoot, _ := defaultGapTrackerRoot()

	cmd.PersistentFlags().StringVar(&rootDir, "root", defaultRoot,
		"gap-tracker repo root (where <project>/ directories live)")
	cmd.PersistentFlags().StringVarP(&project, "project", "p", "",
		"project name (subdirectory under root; required for most commands)")
	cmd.PersistentFlags().StringVarP(&outputMode, "output", "o", "auto",
		"output format: auto, pretty, text, yaml, json")

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newTriageCmd())
	cmd.AddCommand(newShowCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newSyncAllCmd())
	cmd.AddCommand(newNoteCmd())
	cmd.AddCommand(newNotesCmd())
	cmd.AddCommand(newLinkCmd())
	cmd.AddCommand(newMarkCmd())
	cmd.AddCommand(newReconcileCmd())
	cmd.AddCommand(newCrossrefCmd())
	cmd.AddCommand(newReportCmd())

	return cmd
}

// Execute runs the root command and maps errors to non-zero exit codes.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "gapt: %v\n", err)
		os.Exit(1)
	}
}

// defaultGapTrackerRoot returns ~/newstack/gap-tracker as a convenience
// default so callers rarely need to pass --root. Falls back to cwd.
func defaultGapTrackerRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".", err
	}
	return filepath.Join(home, "newstack", "gap-tracker"), nil
}
