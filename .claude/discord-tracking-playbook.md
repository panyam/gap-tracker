# Discord-tracking playbook

Shared infrastructure for the global discord-tracking skills (`/channel-refresh`, `/proposal-refresh`, `/area-refresh`, `/group-refresh`, `/gap-update`). Each of those skills starts by resolving a **tracking root** and loading a **config**. This file specifies both.

Skills are project-scoped: they only work when invoked inside a tracked project's directory tree. If no tracking root resolves from cwd, the skill refuses to run.

## Helper binary: `disctrack`

The `disctrack` Go binary lives at `~/newstack/gap-tracker/cmd/disctrack` (install via `cd ~/newstack/gap-tracker && make install-disctrack`). It encapsulates the resolver + lookups so skills don't have to walk paths manually:

```
disctrack config                     # resolved tracking root + key config fields
disctrack channel <name>             # channel_id from <channels-dir>/<name>.yaml; did-you-mean on miss
disctrack channels-dir               # absolute channels directory path
disctrack area <name>                # absolute area folder path (must exist); did-you-mean on miss
disctrack areas-dir                  # absolute areas directory path
disctrack url <discord-url>          # parses "https://discord.com/channels/<g>/<c>" → "<g> <c>"
disctrack version                    # build metadata
```

Skills should prefer `disctrack` over hand-rolled path walking. The resolver below describes what `disctrack` does internally — useful for understanding errors and for tooling that needs to reproduce the logic outside the binary.

## Resolver

Walk up from cwd applying these rules in order. Return the first match.

1. **Direct**. `<ancestor>/.discord-tracking.yaml` exists. The tracking root is `<ancestor>`. (Used when cwd is inside the tracking repo itself — e.g. `cd ~/projects/mcpcontrib`.)
2. **Symlink/dir**. `<ancestor>/discord_tracking` exists (file, directory, or symlink). Resolve it via `realpath`. If `<resolved>/.discord-tracking.yaml` exists, the tracking root is `<resolved>`. (Used when cwd is inside a code repo with an attached tracking repo — e.g. `cd ~/projects/lilbattle` with `discord_tracking → ../lilbattle-tracking`.)

If neither resolves after walking to `/`, bail:

```
error: no Discord-tracking root found.
cwd: <cwd>
walked: <list of ancestors checked>
to set up a tracked project:
  - cd into the project root
  - if this is the tracking repo: add .discord-tracking.yaml (see ~/.claude/discord-tracking-playbook.md for schema)
  - if this is a code repo: ln -s ../<project>-tracking discord_tracking (then add .discord-tracking.yaml in the tracking repo)
```

## Config schema

`<tracking-root>/.discord-tracking.yaml`. All path-typed fields support `~` (expand to `$HOME`); relative paths resolve against `<tracking-root>`.

```yaml
# Layout — where channels and areas live, relative to <tracking-root>
channels_dir: channels                # e.g. "channels" (default) or "discord/channels" (mcpcontrib)
areas_dir: areas                       # e.g. "areas" (default) or "proposals" (mcpcontrib)
groups_dir: _groups                    # subdir under areas_dir; default "_groups"

# Discord
discord:
  guild_id: "<numeric Discord server ID>"             # required for Playwright-based skills
  parse_snapshot_script: ~/path/to/parse_snapshot.py  # optional; used by /area-refresh's Playwright path
  setup_doc: discord/setup.md                         # optional; relative to <tracking-root>; pointed at when login expires

# The code project gaps are tracked against
code:
  name: mcpkit                                        # human-readable; used in prose ("mcpkit side", citations)
  repo: panyam/mcpkit                                 # GitHub org/repo for issue mutations
  default_worktree: ~/newstack/mcpkit/main            # used by /gap-update, /channel-refresh, /proposal-refresh
  area_refresh_checkout: ~/newstack/mcpkit/gaptrack   # /area-refresh default; falls back to default_worktree

# gapt integration — optional; omit the whole block to disable gapt steps
gapt:
  enabled: true
  project_id: mcpkit                                  # used in `gapt status <id>`, `gapt show <id>:...`
  config_path: ~/newstack/gap-tracker/mcpkit/config.json

# Discord-sync tool — optional; default `mcpcontribd`
sync_tool:
  command: mcpcontribd                                # invoked as `<command> sync <channel>`, `<command> messages-since <channel> --ts <iso>`
```

### Field reference for skills

When a skill below refers to a placeholder, the value comes from config:

| Placeholder | Source | Example |
|---|---|---|
| `<tracking-root>` | resolver output | `/Users/dzshrh/projects/mcpcontrib` |
| `<channels-dir>` | `channels_dir`, joined to tracking root | `/Users/dzshrh/projects/mcpcontrib/discord/channels` |
| `<areas-dir>` | `areas_dir`, joined to tracking root | `/Users/dzshrh/projects/mcpcontrib/proposals` |
| `<groups-dir>` | `areas_dir/groups_dir` | `.../proposals/_groups` |
| `<code.name>` | `code.name` | `mcpkit` |
| `<code.repo>` | `code.repo` | `panyam/mcpkit` |
| `<code.default_worktree>` | `code.default_worktree`, ~-expanded | `/Users/dzshrh/newstack/mcpkit/main` |
| `<code.area_refresh_checkout>` | `code.area_refresh_checkout` or fallback | as above |
| `<discord.guild_id>` | `discord.guild_id` | `1358869848138059966` |
| `<discord.parse_snapshot_script>` | `discord.parse_snapshot_script` (may be null) | `/Users/dzshrh/projects/mcpcontrib/discord/parse_snapshot.py` |
| `<discord.setup_doc>` | `discord.setup_doc` (relative to tracking root) | `/Users/dzshrh/projects/mcpcontrib/discord/setup.md` |
| `<gapt.project_id>` | `gapt.project_id` if `gapt.enabled` | `mcpkit` |
| `<sync_tool>` | `sync_tool.command` or `mcpcontribd` | `mcpcontribd` |

## Layout produced by tracked projects

Each tracked project (regardless of whether tracking is in-repo or a sibling repo) ends up with this structure under `<tracking-root>`:

```
<tracking-root>/
├── .discord-tracking.yaml
├── <channels-dir>/
│   └── <channel-name>.yaml          # per-channel manifest
├── <areas-dir>/
│   ├── <area-name>/
│   │   ├── area.yaml                # per-area manifest (PR/doc tracking config)
│   │   ├── README.md                # area's human-readable strategy doc
│   │   ├── CURRENT_GAPS.md          # written by /channel-refresh + /proposal-refresh
│   │   ├── gap-update-plan.md       # ephemeral; written by /gap-update; gitignored
│   │   ├── refresh-YYYY-MM-DD.md    # per-run output of /area-refresh
│   │   └── spec-snapshots/          # snapshots for inline-comment cursors
│   └── <groups-dir>/
│       └── <group-name>/
│           └── refresh-YYYY-MM-DD.md  # /group-refresh coordinator output
```

The `channel-name` and `area-name` may differ (one channel may map to a differently-named area folder via `area_folder:` in channel.yaml — legacy: `proposal_folder:`).

## Style notes (apply to every committed file these skills write)

- No em dashes in prose. SQL/JSON exempt.
- No semicolons in prose. SQL/JSON exempt.
- Cite by anchor (commit SHA, file:line, comment ID, message ID, issue URL), not by quoted prose.
- `gh` commands require `GH_TOKEN="$GH_PERSONAL_TOKEN"`.
- Backlink hygiene per `~/.claude/CLAUDE.md`: default to plain text or backtick-wrapped `<repo>#N` form for upstream cross-refs.

## Layout vs. skills compatibility

- `/channel-refresh`, `/gap-update`: require `code.default_worktree`, optionally `gapt`, `sync_tool`.
- `/proposal-refresh`: requires `code.default_worktree`, optionally `gapt`. Discord not needed (PR-only).
- `/area-refresh`: requires `code.area_refresh_checkout`, `discord.guild_id`, `discord.parse_snapshot_script`. Heaviest config dependency.
- `/group-refresh`: pure delegation to `/area-refresh`; no extra config.

A tracked project that doesn't use Playwright snapshots (e.g. lilbattle, if mcpcontribd-only) can leave `discord.parse_snapshot_script` and `discord.guild_id` unset and skip `/area-refresh`; the lighter `/channel-refresh` works without them.
