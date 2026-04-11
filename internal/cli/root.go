// Package cli builds the cobra command tree for gapt.
//
// Commands are split across two visible groups in `gapt --help`:
//
//   - Primary: the canonical daily-driver verbs. Use these.
//   - Legacy: verbs that write to the pre-DB-primary `gaps` table.
//     Superseded by `triage` and kept only for inspection / cleanup of
//     historical rows. See docs/STATE_MACHINE.md and the group IDs
//     below.
//
// Layout:
//
//	gapt                 root (persistent --root/--project/--output)
//	├── Primary:
//	│   ├── version
//	│   ├── sync                 (M3)
//	│   ├── sync-all             (M3)
//	│   ├── show                 (M2)
//	│   ├── note                 (M6)
//	│   ├── notes                (M6)
//	│   ├── crossref             (M6)
//	│   └── triage               (M2/M4/M5 — subcommand group)
//	│       ├── status / list / set / unset / close
//	│       └── export / import / verify
//	└── Legacy (prefer triage.*):
//	    ├── link
//	    ├── mark
//	    ├── report
//	    └── reconcile
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

// Command group IDs. Used both as the cobra Group.ID field and by the
// small `inGroup` helper in NewRootCmd that tags each subcommand on
// registration. Keep these few — the help output gets noisy if we
// fan out to more than ~3 groups.
const (
	groupPrimary = "primary"
	groupLegacy  = "legacy"
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

	// Register command groups before adding any subcommands so cobra's
	// help renders them in declaration order. The trailing ':' is a
	// cobra convention that makes the section look like a header.
	cmd.AddGroup(
		&cobra.Group{ID: groupPrimary, Title: "Primary:"},
		&cobra.Group{
			ID:    groupLegacy,
			Title: "Legacy (kept for historical gaps table; prefer triage.*):",
		},
	)

	// inGroup tags a subcommand with a group ID so it appears under the
	// correct section in `gapt --help`. Returning the command inline
	// keeps AddCommand calls one-liners.
	inGroup := func(id string, c *cobra.Command) *cobra.Command {
		c.GroupID = id
		return c
	}

	// Primary: the canonical daily-driver verbs.
	cmd.AddCommand(inGroup(groupPrimary, newVersionCmd()))
	cmd.AddCommand(inGroup(groupPrimary, newTriageCmd()))
	cmd.AddCommand(inGroup(groupPrimary, newShowCmd()))
	cmd.AddCommand(inGroup(groupPrimary, newSyncCmd()))
	cmd.AddCommand(inGroup(groupPrimary, newSyncAllCmd()))
	cmd.AddCommand(inGroup(groupPrimary, newNoteCmd()))
	cmd.AddCommand(inGroup(groupPrimary, newNotesCmd()))
	cmd.AddCommand(inGroup(groupPrimary, newCrossrefCmd()))

	// Legacy: operate on the pre-triage `gaps` table. Kept for
	// historical row inspection and cleanup. Each command's Short help
	// already calls out the recommended replacement (see link.go etc).
	cmd.AddCommand(inGroup(groupLegacy, newLinkCmd()))
	cmd.AddCommand(inGroup(groupLegacy, newMarkCmd()))
	cmd.AddCommand(inGroup(groupLegacy, newReportCmd()))
	cmd.AddCommand(inGroup(groupLegacy, newReconcileCmd()))

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
