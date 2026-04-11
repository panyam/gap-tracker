// Package app wires together the three pieces every command needs:
// a loaded config, an open store, and a renderer selected by --output.
//
// Commands receive an *App from cli.loadApp and use its fields directly.
// Tests can inject a fake App to avoid touching the filesystem.
package app

import (
	"fmt"
	"os"

	"github.com/panyam/gap-tracker/internal/config"
	"github.com/panyam/gap-tracker/internal/github"
	"github.com/panyam/gap-tracker/internal/render"
	"github.com/panyam/gap-tracker/internal/store"
)

// App is the per-invocation context. Not long-lived — one App per command
// execution. Store is closed by Command.Run's defer.
type App struct {
	Cfg      *config.Config
	Store    *store.Store
	Renderer render.Renderer
	GitHub   github.Client
}

// Load resolves the root + project from CLI flags, loads config.json,
// opens the store, and picks a renderer based on the output mode.
func Load(rootDir, project, outputMode string) (*App, error) {
	if project == "" {
		return nil, fmt.Errorf("--project is required (use -p <name>)")
	}

	cfg, err := config.Load(rootDir, project)
	if err != nil {
		return nil, err
	}

	s, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, err
	}

	mode, err := render.ParseMode(outputMode)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	r := render.ForMode(mode, render.IsTerminal(os.Stdout))

	return &App{
		Cfg:      cfg,
		Store:    s,
		Renderer: r,
		GitHub:   github.NewCLIClient(),
	}, nil
}

// Close releases the underlying store connection.
func (a *App) Close() error {
	if a.Store != nil {
		return a.Store.Close()
	}
	return nil
}
