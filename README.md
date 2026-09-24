# gap-tracker

Tracks feature gaps between public reference repos (upstream specs, SDKs,
peer libraries) and a local project. Supports many-to-one: multiple source
repos feeding into a single target repo. The workflow is centered around
classifying every upstream issue as either a real gap to track, an
already-covered capability, something to defer, or not applicable.

The daily-driver CLI is `gapt` (Go). The original Python implementation in
`main.py` is kept for reference until the Go port is retired.

A sibling binary `disctrack` lives here too. It does deterministic lookups
against a project's `.discord-tracking.yaml` (resolving names to channel
ids, parsing Discord URLs, printing config) and is paired with the global
discord-tracking Claude Code skills under `.claude/commands/`. See the
[Discord-tracking skills](#discord-tracking-skills-claude-code) section.

See [docs/STATE_MACHINE.md](docs/STATE_MACHINE.md) for the full state
model and developer entry points — it's the design anchor for how
classification flows through sync → triage → close.

## Install

```bash
cd ~/newstack/gap-tracker
make install        # builds both gapt and disctrack into $GOBIN
gapt version
disctrack version
```

`make install-gapt` and `make install-disctrack` install one at a time.
Both are pure Go (cgo-free, uses `glebarez/sqlite`), producing static
executables in `$GOBIN` (typically `~/go/bin`).

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
├── cmd/
│   ├── gapt/              # gapt CLI entrypoint
│   └── disctrack/         # disctrack CLI entrypoint
├── internal/
│   ├── cli/               # gapt cobra tree + helpers
│   ├── store/render/github/triage/  # gapt internals
│   ├── disctrack/         # resolver + manifest types
│   └── disctrackcli/      # disctrack cobra tree
├── .claude-plugin/
│   └── marketplace.json   # makes this repo a Claude Code marketplace
├── plugins/gaps/          # the `gaps` plugin, served straight from git
│   ├── skills/gaps/       # the /gaps skill (channel | proposal | update)
│   ├── commands/          # /gap-track
│   └── reference/discord-tracking-playbook.md  # resolver + config schema
├── docs/STATE_MACHINE.md  # design anchor: states, verbs, entry points
├── main.py                # legacy Python CLI (parity port source)
├── mcpkit/                # per-project data:
│   ├── config.json        # sources, target repo, target_path
│   ├── gaps.db            # SQLite source of truth (gitignored)
│   └── triage.snapshot.yaml  # committed snapshot (generated via `gapt triage export`)
└── Makefile               # build / install / test / tidy
```

## Discord-tracking skills (Claude Code)

This repo is a Claude Code **marketplace**. `plugins/gaps/` carries the
`/gaps` skill (`channel`, `proposal`, `update`), the `/gap-track`
command, and the resolver/config spec they share. They
work against any project that has a `.discord-tracking.yaml` (directly, or
via a `discord_tracking` symlink to a sibling tracking repo). The
`disctrack` binary backs them.

Nothing is published anywhere: a marketplace is just a git repo with a
`.claude-plugin/marketplace.json`. Declare it once, in the tracked
`.claude/settings.json` of each repo that has channels to track:

```json
{
  "extraKnownMarketplaces": {
    "gap-tracker": { "source": { "source": "github", "repo": "panyam/gap-tracker" } }
  },
  "enabledPlugins": { "gaps@gap-tracker": true }
}
```

That declaration IS the install — a fresh clone of the tracking repo has
the skill, with no per-machine command to remember and no symlink into
`$HOME`. Add it ad hoc on a machine instead with:

```bash
claude plugin marketplace add panyam/gap-tracker --sparse .claude-plugin plugins
claude plugin install gaps@gap-tracker
```

The binaries are still a normal Go install:

```bash
git clone <gap-tracker-remote> ~/newstack/gap-tracker
cd ~/newstack/gap-tracker && make install
```

**Paths inside the plugin are plugin-relative (`${CLAUDE_PLUGIN_ROOT}`) or
`~`-relative, never absolute.** An absolute path here is only ever right on
the machine it was written on; see megh's `CONSTRAINTS.md` C6.

Each tracked project then needs either a `.discord-tracking.yaml` at its
repo root (when the project IS the tracking repo, like `mcpcontrib`) or a
`discord_tracking -> ../<project>-tracking` symlink to a sibling tracking
repo (like `lilbattle`). See the playbook for layout details.

Start tracking a channel with `disctrack onboard <name> <discord-url>`,
which writes the channel manifest and its docs folder
(`<gaps_dir>/<name>/`, one per channel), then pull it with
`disctrack sync <name>`. Sync also tracks and syncs the channel's threads.
The playbook covers the manifest shape, thread handling, and why the
scraper runs from its own directory (a foreign-arch `.venv` in your cwd
used to break it with "Exec format error").

## Config

`<project>/config.json` declares the sources, target repo, and local
target checkout path used by `gapt triage verify`:

```json
{
  "target_repo": "panyam/mcpkit",
  "target_path": "~/newstack/mcpkit/gaptrack",
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
