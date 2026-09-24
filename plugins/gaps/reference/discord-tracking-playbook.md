# Discord-tracking playbook

Shared infrastructure for the global discord-tracking skills (`/gaps channel`, `/gaps proposal`, `/area-refresh`, `/group-refresh`, `/gaps update`). Each of those skills starts by resolving a **tracking root** and loading a **config**. This file specifies both.

Skills are project-scoped: they only work when invoked inside a tracked project's directory tree. If no tracking root resolves from cwd, the skill refuses to run.

## Helper binary: `disctrack`

The `disctrack` Go binary lives at `~/newstack/gap-tracker/cmd/disctrack` (install via `cd ~/newstack/gap-tracker && make install-disctrack`). It encapsulates the resolver + lookups so skills don't have to walk paths manually:

```
disctrack config                     # resolved tracking root + key config fields
disctrack channel <name>             # channel_id from <channels-dir>/<name>.yaml; did-you-mean on miss
disctrack channels-dir               # absolute channels directory path
disctrack area <name>                # absolute area folder path (must exist); did-you-mean on miss
disctrack areas-dir                  # absolute areas directory path
disctrack url <discord-url>          # parses "https://discord.com/channels/<g>/<c>" → "<g> <c>" (thread URL: <c> is the thread id)
disctrack onboard <name> <url>       # writes <channels-dir>/<name>.yaml (+ area README skeleton), refuses duplicates
disctrack sync <name>                # scrapes the channel, then auto-tracks and syncs its threads (--no-threads to skip)
disctrack threads list <parent>      # tracked threads of a channel
disctrack threads discover <parent>  # thread-start markers in the synced parent, tracked or not
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
  - if this is the tracking repo: add .discord-tracking.yaml (see the discord-tracking playbook for the schema)
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
  default_worktree: ~/newstack/mcpkit/main            # used by /gaps update, /gaps channel, /gaps proposal
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
| `<tracking-root>` | resolver output | `<home>/projects/mcpcontrib` |
| `<channels-dir>` | `channels_dir`, joined to tracking root | `<home>/projects/mcpcontrib/discord/channels` |
| `<areas-dir>` | `areas_dir`, joined to tracking root | `<home>/projects/mcpcontrib/proposals` |
| `<groups-dir>` | `areas_dir/groups_dir` | `.../proposals/_groups` |
| `<code.name>` | `code.name` | `mcpkit` |
| `<code.repo>` | `code.repo` | `panyam/mcpkit` |
| `<code.default_worktree>` | `code.default_worktree`, ~-expanded | `<home>/newstack/mcpkit/main` |
| `<code.area_refresh_checkout>` | `code.area_refresh_checkout` or fallback | as above |
| `<discord.guild_id>` | `discord.guild_id` | `1358869848138059966` |
| `<discord.parse_snapshot_script>` | `discord.parse_snapshot_script` (may be null) | `<home>/projects/mcpcontrib/discord/parse_snapshot.py` |
| `<discord.setup_doc>` | `discord.setup_doc` (relative to tracking root) | `<home>/projects/mcpcontrib/discord/setup.md` |
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
│   │   ├── CURRENT_GAPS.md          # written by /gaps channel + /gaps proposal
│   │   ├── gap-update-plan.md       # ephemeral; written by /gaps update; gitignored
│   │   ├── refresh-YYYY-MM-DD.md    # per-run output of /area-refresh
│   │   └── spec-snapshots/          # snapshots for inline-comment cursors
│   └── <groups-dir>/
│       └── <group-name>/
│           └── refresh-YYYY-MM-DD.md  # /group-refresh coordinator output
```

The `channel-name` and `area-name` may differ (one channel may map to a differently-named area folder via `area_folder:` in channel.yaml — legacy: `proposal_folder:`).

## Channel manifests

`disctrack onboard <name> <discord-url>` writes a new manifest. Hand-writing one still works, and this is the shape onboard produces:

```yaml
# <channels-dir>/<name>.yaml
name: <name>
channel_id: "<channel-id>"
display_name: "<name>"
status: active                   # active | dormant | parked | resolved
kind: text                       # text | thread | forum. Default text.
# parent_channel: <parent-name>  # threads only. A manifest name or channel id.
# area_folder: <folder>          # when the area folder is not <name>
baseline_window: 4w
tags: []
notes: |
  Onboarded on YYYY-MM-DD via disctrack onboard.
```

Onboard refuses a URL from another guild, an existing `<name>.yaml`, and a channel id that another manifest already tracks. A text channel also gets `<areas-dir>/<name>/README.md` with a heading-only skeleton when that folder is missing. A thread (a `/threads/` URL, or `--parent <name>`) gets `kind: thread` and `parent_channel`, and no area folder. Its parent must be tracked first.

### Threads

A Discord thread is its own channel with its own id, cursor and manifest. You rarely write thread manifests by hand. After `disctrack sync <parent>` ingests the parent, it:

1. Finds the parent's "started a thread" markers. snapshot.py captures each marker's thread id during the parent scrape.
2. Writes a manifest for every thread that no manifest tracks yet. The name is `<parent>-<title-slug>-thread`, with the last four digits of the thread id added on a name collision. `notes` records the title, who started it, when, and that it was auto-tracked.
3. Syncs every `status: active` thread of the parent, new and old, in one browser session. A new thread starts five minutes before its marker, or at the parent's `--since` if that is later. An old thread resumes from its own cursor.

A thread that fails to sync is reported in its own JSON line and in the closing `summary` line. It does not fail the parent or the other threads. `disctrack sync <thread-name>` retries one thread on its own. Markers whose thread id could not be captured count as `unresolved_markers` and usually resolve on the next parent sync.

Older thread manifests use a `parent:` block (`channel_id`, `channel_name`) and no `kind:`. disctrack treats those as threads of that parent, so `threads list` and the thread sync include them.

## Running the scraper

`disctrack sync` runs `python/snapshot.py` from this repo through `uv run`, with the working directory set to the script's own folder and `VIRTUAL_ENV` removed from its environment. uv picks its interpreter from `VIRTUAL_ENV` and from any `.venv` in the working directory or above it, ahead of every other Python. A `.venv` built on another machine (a Mac venv in a repo mounted into a Linux container) used to fail the sync with `Exec format error` whenever disctrack ran from inside that repo. If the error comes back, look for an active or ancestor `.venv` of the wrong architecture next to snapshot.py itself.

## Style notes (apply to every committed file these skills write)

- No em dashes in prose. SQL/JSON exempt.
- No semicolons in prose. SQL/JSON exempt.
- Cite by anchor (commit SHA, file:line, comment ID, message ID, issue URL), not by quoted prose.
- `gh` commands require `GH_TOKEN="$GH_PERSONAL_TOKEN"`.
- Backlink hygiene per `~/.claude/CLAUDE.md`: default to plain text or backtick-wrapped `<repo>#N` form for upstream cross-refs.

## Layout vs. skills compatibility

- `/gaps channel`, `/gaps update`: require `code.default_worktree`, optionally `gapt`, `sync_tool`.
- `/gaps proposal`: requires `code.default_worktree`, optionally `gapt`. Discord not needed (PR-only).
- `/area-refresh`: requires `code.area_refresh_checkout`, `discord.guild_id`, `discord.parse_snapshot_script`. Heaviest config dependency.
- `/group-refresh`: pure delegation to `/area-refresh`; no extra config.

A tracked project that doesn't use Playwright snapshots (e.g. lilbattle, if mcpcontribd-only) can leave `discord.parse_snapshot_script` and `discord.guild_id` unset and skip `/area-refresh`; the lighter `/gaps channel` works without them.
