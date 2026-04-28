// Package config loads the per-project config.json file.
//
// Each project under ~/newstack/gap-tracker/<project>/ has a config.json
// declaring its target repo, source repos, and (optionally) a target_path
// for where the target repo is checked out locally.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Source is one upstream feeder repo.
type Source struct {
	Repo       string `json:"repo"`
	Label      string `json:"label"`
	IncludePRs bool   `json:"include_prs,omitempty"`
}

// Config is the structure of <project>/config.json.
type Config struct {
	TargetRepo string   `json:"target_repo"`
	TargetPath string   `json:"target_path,omitempty"`
	Sources    []Source `json:"sources"`

	// ProjectRoot is the directory holding this config (not serialized).
	ProjectRoot string `json:"-"`
	// Project is the project name (directory basename, not serialized).
	Project string `json:"-"`
}

// Load reads the config for a project. rootDir is the gap-tracker repo root
// (typically ~/newstack/gap-tracker); project is the subdirectory name.
func Load(rootDir, project string) (*Config, error) {
	projectDir := filepath.Join(rootDir, project)
	path := filepath.Join(projectDir, "config.json")

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.TargetRepo == "" {
		return nil, fmt.Errorf("%s: target_repo is required", path)
	}
	cfg.ProjectRoot = projectDir
	cfg.Project = project
	return &cfg, nil
}

// DBPath returns the path to this project's SQLite database.
func (c *Config) DBPath() string {
	return filepath.Join(c.ProjectRoot, "gaps.db")
}

// SourceLabelToRepo resolves a label ("go-sdk", "spec", "target") to its
// full repo name. Returns empty string if not found.
func (c *Config) SourceLabelToRepo(label string) string {
	if label == "target" {
		return c.TargetRepo
	}
	for _, s := range c.Sources {
		if s.Label == label {
			return s.Repo
		}
	}
	return ""
}

// SourceByLabel returns the Source struct for a label, or nil if not found.
func (c *Config) SourceByLabel(label string) *Source {
	for i := range c.Sources {
		if c.Sources[i].Label == label {
			return &c.Sources[i]
		}
	}
	return nil
}

// RepoToSourceLabel is the inverse lookup. Returns the label for a repo,
// or the full repo name if no label matches.
func (c *Config) RepoToSourceLabel(repo string) string {
	if repo == c.TargetRepo {
		return "target"
	}
	for _, s := range c.Sources {
		if s.Repo == repo {
			return s.Label
		}
	}
	return repo
}
