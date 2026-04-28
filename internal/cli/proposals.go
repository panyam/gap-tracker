package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/panyam/gap-tracker/internal/config"
	"github.com/panyam/gap-tracker/internal/render"
	"github.com/panyam/gap-tracker/internal/store"
)

// newPropCmd is the parent `gapt prop` command with subcommands.
func newPropCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prop",
		Short: "Manage upstream proposals (SEPs, PEPs, JEPs, ...)",
		Long: `Track upstream enhancement proposals that drive your project roadmap.

Proposals overlay the issues table — a proposal is always backed by a synced
issue, but carries extra lifecycle metadata: kind, stage, priority, theme,
release targeting, and sponsor.`,
	}
	cmd.AddCommand(newPropListCmd())
	cmd.AddCommand(newPropSetCmd())
	cmd.AddCommand(newPropUnsetCmd())
	cmd.AddCommand(newPropImportCmd())
	return cmd
}

// newPropListCmd is `gapt prop list <project> [--kind SEP] [--release ...] [--stage ...] [--priority ...]`.
func newPropListCmd() *cobra.Command {
	var (
		kind     string
		stage    string
		release  string
		priority string
		theme    string
		source   string
	)

	cmd := &cobra.Command{
		Use:   "list <project>",
		Short: "List tracked proposals (SEPs, PEPs, JEPs, etc.)",
		Long: `List proposals with optional filters. Proposals are upstream enhancement
proposals that drive your project roadmap.

Examples:
  gapt prop-list mcpkit
  gapt prop-list mcpkit --kind SEP --release 2026-06-30
  gapt prop-list mcpkit --stage accepted --priority P0
  gapt prop-list mcpkit --theme Transport`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			repo := source
			if repo != "" {
				resolved := a.Cfg.SourceLabelToRepo(repo)
				if resolved == "" {
					return fmt.Errorf("unknown source label %q", repo)
				}
				repo = resolved
			}

			rows, err := a.Store.ListProposals(store.ProposalFilter{
				Kind:     kind,
				Stage:    stage,
				Release:  release,
				Priority: priority,
				Theme:    theme,
				Repo:     repo,
			})
			if err != nil {
				return err
			}

			list := buildProposalList(a.Cfg, kind, release, rows)
			return a.Renderer.RenderProposals(os.Stdout, list)
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind (SEP, PEP, JEP)")
	cmd.Flags().StringVar(&stage, "stage", "", "filter by stage (draft, in-review, accepted, final, ...)")
	cmd.Flags().StringVar(&release, "release", "", "filter by target release")
	cmd.Flags().StringVar(&priority, "priority", "", "filter by priority (P0, P1)")
	cmd.Flags().StringVar(&theme, "theme", "", "filter by theme (substring match)")
	cmd.Flags().StringVar(&source, "source", "", "filter to one source label")

	return cmd
}

// newPropSetCmd is `gapt prop-set <project> <source>:<number> --kind SEP --id SEP-2356 [...]`.
func newPropSetCmd() *cobra.Command {
	var (
		kind       string
		identifier string
		priority   string
		stage      string
		theme      string
		release    string
		sponsor    string
	)

	cmd := &cobra.Command{
		Use:   "set <project> <source>:<number>",
		Short: "Register or update a proposal",
		Long: `Register an issue as a tracked proposal or update its metadata.

Examples:
  gapt prop-set mcpkit spec:2356 --kind SEP --id SEP-2356 --priority P0 --stage in-review --theme "Agent Communication" --release 2026-06-30
  gapt prop-set mcpkit spec:2575 --stage accepted-with-changes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			ref := rest[0]
			label, numStr, ok := strings.Cut(ref, ":")
			if !ok {
				return fmt.Errorf("expected <source>:<number>, got %q", ref)
			}
			number, err := strconv.Atoi(numStr)
			if err != nil {
				return fmt.Errorf("invalid number in %q: %w", ref, err)
			}

			repo := a.Cfg.SourceLabelToRepo(label)
			if repo == "" {
				return fmt.Errorf("unknown source label %q", label)
			}

			// If updating, merge with existing values.
			existing, _ := a.Store.GetProposal(repo, number)
			if existing != nil {
				if kind == "" {
					kind = existing.Kind
				}
				if identifier == "" {
					identifier = existing.Identifier
				}
				if priority == "" {
					priority = existing.Priority
				}
				if stage == "" {
					stage = existing.Stage
				}
				if theme == "" {
					theme = existing.Theme
				}
				if release == "" {
					release = existing.Release
				}
				if sponsor == "" {
					sponsor = existing.Sponsor
				}
			}

			if kind == "" {
				return fmt.Errorf("--kind is required for new proposals")
			}
			if identifier == "" {
				return fmt.Errorf("--id is required for new proposals")
			}

			err = a.Store.UpsertProposal(store.UpsertProposalInput{
				Repo:       repo,
				Number:     number,
				Kind:       kind,
				Identifier: identifier,
				Priority:   priority,
				Stage:      stage,
				Theme:      theme,
				Release:    release,
				Sponsor:    sponsor,
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "✓ %s → %s (stage=%s, pri=%s)\n",
				identifier, ref, stage, priority)
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "proposal kind: SEP, PEP, JEP, etc.")
	cmd.Flags().StringVar(&identifier, "id", "", "proposal identifier (e.g. SEP-2356)")
	cmd.Flags().StringVar(&priority, "priority", "", "priority (P0, P1, ...)")
	cmd.Flags().StringVar(&stage, "stage", "", "lifecycle stage")
	cmd.Flags().StringVar(&theme, "theme", "", "roadmap theme")
	cmd.Flags().StringVar(&release, "release", "", "target release (e.g. 2026-06-30)")
	cmd.Flags().StringVar(&sponsor, "sponsor", "", "proposal sponsor/owner")

	return cmd
}

// newPropUnsetCmd removes a proposal registration.
func newPropUnsetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset <project> <source>:<number>",
		Short: "Remove a proposal registration",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			ref := rest[0]
			label, numStr, ok := strings.Cut(ref, ":")
			if !ok {
				return fmt.Errorf("expected <source>:<number>, got %q", ref)
			}
			number, err := strconv.Atoi(numStr)
			if err != nil {
				return fmt.Errorf("invalid number in %q: %w", ref, err)
			}

			repo := a.Cfg.SourceLabelToRepo(label)
			if repo == "" {
				return fmt.Errorf("unknown source label %q", label)
			}

			deleted, err := a.Store.DeleteProposal(repo, number)
			if err != nil {
				return err
			}
			if !deleted {
				fmt.Fprintf(os.Stderr, "no proposal found for %s\n", ref)
			} else {
				fmt.Fprintf(os.Stderr, "✓ removed proposal for %s\n", ref)
			}
			return nil
		},
	}
	return cmd
}

// newPropImportCmd bulk-imports proposals from a structured list.
// This enables repeatable loading from scraped data (e.g. plan.modelcontextprotocol.io).
func newPropImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <project> <file.yaml>",
		Short: "Bulk-import proposals from YAML",
		Long: `Import proposals from a YAML file. Format:

proposals:
  - repo: modelcontextprotocol/specification
    number: 2356
    kind: SEP
    identifier: SEP-2356
    priority: P0
    stage: in-review
    theme: Agent Communication
    release: "2026-06-30"
    sponsor: localden`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, rest, err := loadAppFromArgs(args)
			if err != nil {
				return err
			}
			defer a.Close()

			data, err := os.ReadFile(rest[0])
			if err != nil {
				return err
			}

			inputs, err := parseProposalYAML(data)
			if err != nil {
				return err
			}

			count, err := a.Store.UpsertProposals(inputs)
			if err != nil {
				return fmt.Errorf("imported %d before error: %w", count, err)
			}
			fmt.Fprintf(os.Stderr, "✓ imported %d proposals\n", count)
			return nil
		},
	}
	return cmd
}

// buildProposalList transforms store rows into the render view, grouped by stage.
func buildProposalList(cfg *config.Config, kind, release string, rows []store.ProposalRow) render.ProposalList {
	list := render.ProposalList{
		Project: cfg.Project,
		Kind:    kind,
		Release: release,
		Total:   len(rows),
	}

	// Group by stage preserving order of first appearance.
	stageOrder := []string{}
	grouped := map[string][]render.ProposalRow{}
	for _, r := range rows {
		stage := r.Stage
		if stage == "" {
			stage = "(unset)"
		}
		if _, seen := grouped[stage]; !seen {
			stageOrder = append(stageOrder, stage)
		}
		label := cfg.RepoToSourceLabel(r.Repo)
		if label == "" {
			label = shortLabel(r.Repo)
		}
		grouped[stage] = append(grouped[stage], render.ProposalRow{
			Identifier: r.Identifier,
			Priority:   r.Priority,
			Title:      r.Title,
			Stage:      r.Stage,
			Theme:      r.Theme,
			Sponsor:    r.Sponsor,
			Release:    r.Release,
			State:      r.State,
			Bucket:     r.Bucket,
			Ref:        fmt.Sprintf("%s:%d", label, r.Number),
		})
	}

	for _, stage := range stageOrder {
		list.Groups = append(list.Groups, render.ProposalGroup{
			Stage: stage,
			Count: len(grouped[stage]),
			Rows:  grouped[stage],
		})
	}
	return list
}

// shortLabel extracts the last path segment of a repo as a short label.
// e.g. "modelcontextprotocol/specification" -> "specification"
// This is a fallback when we don't have access to the config's label mapping.
func shortLabel(repo string) string {
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		return repo[i+1:]
	}
	return repo
}

// parseProposalYAML parses the YAML import format into upsert inputs.
func parseProposalYAML(data []byte) ([]store.UpsertProposalInput, error) {
	// Use gopkg.in/yaml.v3 to parse.
	// Import inline to avoid adding it to this file's imports if not needed.
	type yamlProposal struct {
		Repo       string `yaml:"repo"`
		Number     int    `yaml:"number"`
		Kind       string `yaml:"kind"`
		Identifier string `yaml:"identifier"`
		Priority   string `yaml:"priority"`
		Stage      string `yaml:"stage"`
		Theme      string `yaml:"theme"`
		Release    string `yaml:"release"`
		Sponsor    string `yaml:"sponsor"`
	}
	type yamlFile struct {
		Proposals []yamlProposal `yaml:"proposals"`
	}

	var f yamlFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse proposals YAML: %w", err)
	}

	inputs := make([]store.UpsertProposalInput, 0, len(f.Proposals))
	for _, p := range f.Proposals {
		if p.Repo == "" || p.Number == 0 || p.Kind == "" || p.Identifier == "" {
			return nil, fmt.Errorf("proposal entry missing required fields (repo, number, kind, identifier)")
		}
		inputs = append(inputs, store.UpsertProposalInput{
			Repo:       p.Repo,
			Number:     p.Number,
			Kind:       p.Kind,
			Identifier: p.Identifier,
			Priority:   p.Priority,
			Stage:      p.Stage,
			Theme:      p.Theme,
			Release:    p.Release,
			Sponsor:    p.Sponsor,
		})
	}
	return inputs, nil
}
