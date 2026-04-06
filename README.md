# gap-tracker

Generic tool for tracking feature gaps between public reference repos and a local project. Supports many-to-one: multiple source repos feeding into a single target.

## Structure

```
gap-tracker/
  main.py              # all commands
  pyproject.toml       # uv project (ipython, ipdb)
  mcpkit/              # project: mcpkit gaps
    config.json        # sources=[go-sdk, spec], target=panyam/mcpkit
    gaps.db            # SQLite DB (gitignored)
```

## Setup

```bash
cd ~/newstack/gap-tracker
uv venv && uv sync
```

## Usage

```bash
# Sync issues (offset is required, count defaults to 100)
uv run python main.py sync mcpkit 0           # first 100 issues
uv run python main.py sync mcpkit 100         # next 100
uv run python main.py sync mcpkit 200 --count 50

# Report: source-centric view of tracked vs untracked
uv run python main.py report mcpkit

# Cross-reference: auto-match source→target by keyword similarity
uv run python main.py crossref mcpkit

# Show issue details (body included)
uv run python main.py show mcpkit 56          # target issue
uv run python main.py show mcpkit go-sdk:666  # source issue
uv run python main.py show mcpkit spec:282    # spec issue

# Link a gap: feature name → our issue# + source evidence
uv run python main.py link mcpkit "session-timeout" 60 "go-sdk:148,spec:282"

# Mark gap status
uv run python main.py mark mcpkit "session-timeout" in-progress
```

## Config format (config.json)

```json
{
  "target_repo": "panyam/mcpkit",
  "sources": [
    {"repo": "modelcontextprotocol/go-sdk", "label": "go-sdk"},
    {"repo": "modelcontextprotocol/specification", "label": "spec"}
  ]
}
```

## Schema

- `issues`: all issues from all repos (source + target), synced incrementally
- `gaps`: maps a feature/capability → target issue# + source evidence + status + priority
- `sync_log`: when each repo was last synced, with offset used

## Weekly workflow

```bash
# 1. Sync latest issues
GH_TOKEN="$GH_PERSONAL_TOKEN" uv run python main.py sync mcpkit 0

# 2. Cross-reference to find new matches
uv run python main.py crossref mcpkit

# 3. Review report
uv run python main.py report mcpkit

# 4. Link new gaps and update status
uv run python main.py link mcpkit "new-feature" 99 "go-sdk:123"
uv run python main.py mark mcpkit "old-feature" closed
```
