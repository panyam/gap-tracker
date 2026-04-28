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
//
// Note: project is NOT a flag — every verb takes it as args[0]. See
// loadAppFromArgs in helpers.go. Matches the Python CLI shape.
var (
	rootDir    string
	outputMode string
)

// Command group IDs. Keep these few — the help output gets noisy if
// we fan out to more than ~4 groups.
const (
	groupClassify = "classify"
	groupData     = "data"
	groupInspect  = "inspect"
	groupLegacy   = "legacy"
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
	cmd.PersistentFlags().StringVarP(&outputMode, "output", "o", "auto",
		"output format: auto, pretty, text, yaml, json")

	cmd.AddGroup(
		&cobra.Group{ID: groupClassify, Title: "Classification:"},
		&cobra.Group{ID: groupData, Title: "Data:"},
		&cobra.Group{ID: groupInspect, Title: "Inspect:"},
		&cobra.Group{ID: groupLegacy, Title: "Legacy (gaps table; prefer classification verbs):"},
	)

	g := func(id string, c *cobra.Command) *cobra.Command {
		c.GroupID = id
		return c
	}

	// Classification — the core workflow, formerly nested under `triage`.
	cmd.AddCommand(g(groupClassify, newTriageStatusCmd()))
	cmd.AddCommand(g(groupClassify, newTriageListCmd()))
	cmd.AddCommand(g(groupClassify, newTriageSetCmd()))
	cmd.AddCommand(g(groupClassify, newTriageUnsetCmd()))
	cmd.AddCommand(g(groupClassify, newTriageCloseCmd()))
	cmd.AddCommand(g(groupClassify, newTriageVerifyCmd()))
	cmd.AddCommand(g(groupClassify, newSettleCmd()))

	// Data — ingest + YAML snapshot I/O.
	cmd.AddCommand(g(groupData, newSyncCmd()))
	cmd.AddCommand(g(groupData, newSyncAllCmd()))
	cmd.AddCommand(g(groupData, newTriageExportCmd()))
	cmd.AddCommand(g(groupData, newTriageImportCmd()))

	// Proposals — upstream enhancement proposals driving the roadmap.
	cmd.AddCommand(g(groupData, newPropCmd()))

	// Inspect — read-only views + ad-hoc notes.
	cmd.AddCommand(g(groupInspect, newShowCmd()))
	cmd.AddCommand(g(groupInspect, newNoteCmd()))
	cmd.AddCommand(g(groupInspect, newNotesCmd()))
	cmd.AddCommand(g(groupInspect, newCrossrefCmd()))
	cmd.AddCommand(g(groupInspect, newSearchCmd()))
	cmd.AddCommand(g(groupInspect, newQueryCmd()))
	cmd.AddCommand(g(groupInspect, newVersionCmd()))

	// Legacy — pre-triage gaps table. Kept for historical rows.
	cmd.AddCommand(g(groupLegacy, newLinkCmd()))
	cmd.AddCommand(g(groupLegacy, newMarkCmd()))
	cmd.AddCommand(g(groupLegacy, newReconcileCmd()))

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
