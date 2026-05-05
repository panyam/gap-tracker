# gap-tracker

Tracks feature gaps between public reference repos (upstream specs, SDKs,
peer libraries) and a local project. Supports many-to-one: multiple source
repos feeding into a single target repo. The workflow is centered around
classifying every upstream issue as either a real gap to track, an
already-covered capability, something to defer, or not applicable.

The daily-driver CLI is `gapt` (Go). The original Python implementation in
`main.py` is kept for reference until the Go port is retired.

See [docs/STATE_MACHINE.md](docs/STATE_MACHINE.md) for the full state
model and developer entry points — it's the design anchor for how
classification flows through sync → triage → close.

## Install

```bash
cd ~/newstack/gap-tracker
make install        # go install ./cmd/gapt into $GOBIN
gapt version
```

The binary is pure Go (cgo-free, uses `glebarez/sqlite`), so installing
produces a single static `gapt` executable in `$GOBIN` (typically
`~/go/bin`).

**After pulling source changes**, rerun `make install` — `gapt version`
prints `v0.0.X-N-g<sha>` (the `-N` suffix is commits-since-tag) and
`gapt sync` output formatting can change between revisions. A stale
binary against newer config behavior produces silent miscompares (e.g.,
`include_prs: true` ignored because the binary predates the flag's
plumbing). No automatic check today — `make install` is the only
guard. If sync output looks wrong (e.g., `0 issues` for a repo you
know has a PR), check whether `stat $(which gapt)` is older than the
source files you just pulled.

## Structure

```
gap-tracker/
├── cmd/gapt/              # Go CLI entrypoint
├── internal/              # Go packages: cli, store, render, github, triage, ...
├── docs/STATE_MACHINE.md  # design anchor: states, verbs, entry points
├── main.py                # legacy Python CLI (parity port source)
├── mcpkit/                # per-project data:
│   ├── config.json        # sources, target repo, target_path
│   ├── gaps.db            # SQLite source of truth (gitignored)
│   └── triage.snapshot.yaml  # committed snapshot (generated via `gapt triage export`)
└── Makefile               # build / install / test / tidy
```

## Config

`<project>/config.json` declares the sources, target repo, and local
target checkout path used by `gapt triage verify`:

```json
{
  "target_repo": "panyam/mcpkit",
  "target_path": "/Users/dzshrh/newstack/mcpkit/gaptrack",
  "sources": [
    {"repo": "modelcontextprotocol/go-sdk", "label": "go-sdk"},
    {"repo": "modelcontextprotocol/specification", "label": "spec"},
    {"repo": "modelcontextprotocol/ext-apps", "label": "ext-apps"}
  ]
}
```

## Daily flow

```bash
# 1. Pull new upstream issues + refresh target state
GH_TOKEN="$GH_PERSONAL_TOKEN" gapt -p mcpkit sync-all

# 2. See what needs attention — any unclassified items?
gapt -p mcpkit triage status

# 3. Triage new items (interactive flow via the /gap-track slash command)
#    or one at a time via the CLI:
gapt -p mcpkit triage set ext-apps:612 real-gap mcpkit#200 "Rationale here" \
    --test ext/ui/app_helpers_test.go::TestFoo

# 4. When a gap lands: sync target, then flip to done
gapt -p mcpkit sync mcpkit target:0
gapt -p mcpkit triage close ext-apps:612

# 5. Drift check — run pinned tests and flag mismatches
gapt -p mcpkit triage verify

# 6. Commit a snapshot periodically
gapt -p mcpkit triage export
git add mcpkit/triage.snapshot.yaml
```

## Output modes

Every read verb supports `-o / --output`:

| mode | when | notes |
|---|---|---|
| `auto` | default | pretty on a TTY, text in pipes |
| `pretty` | interactive use | lipgloss tables + color |
| `text` | pipes, LLM tool context | plain ASCII, no ANSI |
| `yaml` | scripting, `yq`, LLM consumption | structured per view type |

Example — pipe triage status to an LLM tool cleanly:

```bash
gapt -p mcpkit triage list --bucket real-gap -o yaml
```

## Shell completion

`gapt` emits shell completion scripts via cobra. Pick one:

```bash
# zsh — source on demand
gapt completion zsh > ~/.gapt-completion.zsh
echo 'source ~/.gapt-completion.zsh' >> ~/.zshrc

# bash
gapt completion bash > /usr/local/etc/bash_completion.d/gapt

# fish
gapt completion fish > ~/.config/fish/completions/gapt.fish
```

After sourcing, tab-complete on `gapt <TAB>`, `gapt triage <TAB>`,
flag names, bucket names on `triage set`, etc.

## Command surface

`gapt --help` groups commands into **Primary** (daily driver) and
**Legacy** (gaps-table verbs kept for historical inspection — prefer
`triage` equivalents):

```
Primary:
  crossref    suggest matches from source issues to target issues
  note        append (or --set overwrite) a note on an issue
  notes       list issues with notes, optionally filtered by substring
  show        show details for an issue (triage block, notes, linked gap)
  sync        sync one source or target from GitHub
  sync-all    sync every source + target in one shot
  triage      classification — the main verb group (see below)
  version     print version / commit / build date

Triage subcommands:
  triage status     per-source bucket counts + unclassified tail
  triage list       grouped view, --bucket X / --source Y filters
  triage set        upsert a classification with optional --test path
  triage unset      delete a classification
  triage close      flip real-gap → done (refuses if target still OPEN)
  triage export     write YAML snapshot (default: triage.snapshot.yaml)
  triage import     bulk-load a YAML snapshot into the DB
  triage verify     run pinned tests against target_path + flag drift

Legacy (prefer triage.*):
  link        link a feature to a target issue
  mark        update a gap's status
  report      per-source tracked/untracked + all-gaps summary
  reconcile   sync target + auto-close resolved gaps
```

## Schema

`gaps.db` has four tables:

- **`issues`** — every GitHub issue we've synced from any repo (source or
  target). Keyed on `(repo, number)`.
- **`triage`** — one classification per source issue. Primary source of
  truth for the daily workflow. FK to `issues`.
- **`gaps`** — legacy feature-level mapping (feature → target issue +
  source evidence). Superseded by `triage`; kept for historical rows.
- **`sync_log`** — append-only record of sync calls for observability.

Schema is defined as raw DDL in `internal/store/schema.go` and applied
via `CREATE TABLE IF NOT EXISTS` on every open. GORM is used only for
queries — see `feedback_gorm_automigrate.md` for the history.
