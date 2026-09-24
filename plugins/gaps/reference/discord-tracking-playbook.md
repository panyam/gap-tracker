# Discord-tracking playbook

Shared infrastructure for the global discord-tracking skills (`/gaps channel`, `/gaps proposal`, `/gaps update`). Each of those skills starts by resolving a **tracking root** and loading a **config**. This file specifies both, plus the channel manifest that every skill keys on.

**Discord channels are the only entry point.** Each tracked non-thread channel owns exactly one docs folder, `<gaps-dir>/<channel-name>/`. There is no indirection between a channel and its folder, no per-folder manifest, and no grouping layer. Everything a skill needs to know about a channel lives in its manifest at `<channels-dir>/<channel-name>.yaml`.

Skills are project-scoped: they only work when invoked inside a tracked project's directory tree. If no tracking root resolves from cwd, the skill refuses to run.

## Helper binary: `disctrack`

The `disctrack` Go binary lives at `~/newstack/gap-tracker/cmd/disctrack` (install via `cd ~/newstack/gap-tracker && make install-disctrack`). It encapsulates the resolver + lookups so skills don't have to walk paths manually:

```
disctrack config                     # resolved tracking root + key config fields
disctrack channel <name>             # channel_id from <channels-dir>/<name>.yaml, did-you-mean on miss
disctrack channels-dir               # absolute channels directory path
disctrack gaps-dir                   # absolute gaps directory path
disctrack dir <channel>              # absolute <gaps-dir>/<channel> (must exist, errors for threads and untracked channels)
disctrack url <discord-url>          # parses "https://discord.com/channels/<g>/<c>" → "<g> <c>" (thread URL: <c> is the thread id)
disctrack onboard <name> <url>       # writes <channels-dir>/<name>.yaml (+ <gaps-dir>/<name>/README.md skeleton), refuses duplicates
disctrack sync <name>                # scrapes the channel, then auto-tracks and syncs its threads (--no-threads to skip)
disctrack threads list <parent>      # tracked threads of a channel
disctrack threads discover <parent>  # thread-start markers in the synced parent, tracked or not
disctrack version                    # build metadata
```

Skills should prefer `disctrack` over hand-rolled path walking. The resolver below describes what `disctrack` does internally. It helps with reading errors and with tooling that needs to reproduce the logic outside the binary.

## Resolver

Walk up from cwd applying these rules in order. Return the first match.

1. **Direct**. `<ancestor>/.discord-tracking.yaml` exists. The tracking root is `<ancestor>`. (Used when cwd is inside the tracking repo itself, e.g. `cd ~/projects/mcpcontrib`.)
2. **Symlink/dir**. `<ancestor>/discord_tracking` exists (file, directory, or symlink). Resolve it via `realpath`. If `<resolved>/.discord-tracking.yaml` exists, the tracking root is `<resolved>`. (Used when cwd is inside a code repo with an attached tracking repo, e.g. `cd ~/projects/lilbattle` with `discord_tracking → ../lilbattle-tracking`.)

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

`<tracking-root>/.discord-tracking.yaml`. All path-typed fields support `~` (expand to `$HOME`). Relative paths resolve against `<tracking-root>`.

```yaml
# Layout: where manifests and channel docs folders live, relative to <tracking-root>
channels_dir: channels                # default "channels". mcpcontrib uses "discord/channels"
gaps_dir: proposals                   # default "proposals". One folder per channel: <gaps_dir>/<channel-name>/

# Discord
discord:
  guild_id: "<numeric Discord server ID>"             # required by disctrack sync and onboard's guild check
  snapshot_script: null                               # optional, defaults to the build-time python/snapshot.py
  profile_dir: ~/path/to/profile                      # optional Chromium user-data-dir, defaults to <tracking-root>/profiles/default
  setup_doc: discord/setup.md                         # optional, relative to <tracking-root>, pointed at when login expires

# The code project gaps are tracked against
code:
  name: mcpkit                                        # human-readable, used in prose ("mcpkit side", citations)
  repo: panyam/mcpkit                                 # GitHub org/repo for issue mutations
  default_worktree: ~/newstack/mcpkit/main            # used by /gaps update, /gaps channel, /gaps proposal

# gapt integration. Optional, omit the whole block to disable gapt steps
gapt:
  enabled: true
  project_id: mcpkit                                  # used in `gapt status <id>`, `gapt show <id>:...`
  config_path: ~/newstack/gap-tracker/mcpkit/config.json

# Discord-sync tool. Optional, default `mcpcontribd`
sync_tool:
  command: mcpcontribd                                # invoked as `<command> sync <channel>`, `<command> messages-since <channel> --ts <iso>`
```

**Legacy keys.** `areas_dir` is still read when `gaps_dir` is unset, and disctrack prints a one-line deprecation warning on stderr each time it loads such a config. Rename it to `gaps_dir`. `groups_dir`, `discord.parse_snapshot_script` and `code.area_refresh_checkout` belonged to the removed `/area-refresh` and `/group-refresh` commands and are ignored.

### Field reference for skills

When a skill below refers to a placeholder, the value comes from config:

| Placeholder | Source | Example |
|---|---|---|
| `<tracking-root>` | resolver output | `<home>/projects/mcpcontrib` |
| `<channels-dir>` | `channels_dir`, joined to tracking root | `<home>/projects/mcpcontrib/discord/channels` |
| `<gaps-dir>` | `gaps_dir`, joined to tracking root | `<home>/projects/mcpcontrib/proposals` |
| `<gaps-dir>/<channel>/` | the channel's docs folder, printed by `disctrack dir <channel>` | `.../proposals/gateways-ig/` |
| `<code.name>` | `code.name` | `mcpkit` |
| `<code.repo>` | `code.repo` | `panyam/mcpkit` |
| `<code.default_worktree>` | `code.default_worktree`, ~-expanded | `<home>/newstack/mcpkit/main` |
| `<discord.guild_id>` | `discord.guild_id` | `1358869848138059966` |
| `<discord.setup_doc>` | `discord.setup_doc` (relative to tracking root) | `<home>/projects/mcpcontrib/discord/setup.md` |
| `<gapt.project_id>` | `gapt.project_id` if `gapt.enabled` | `mcpkit` |
| `<sync_tool>` | `sync_tool.command` or `mcpcontribd` | `mcpcontribd` |

## Layout produced by tracked projects

Each tracked project (regardless of whether tracking is in-repo or a sibling repo) ends up with this structure under `<tracking-root>`:

```
<tracking-root>/
├── .discord-tracking.yaml
├── <channels-dir>/
│   ├── <channel-name>.yaml          # per-channel manifest, the only per-channel config
│   └── <parent>-<slug>-thread.yaml  # thread manifests, no folder of their own
└── <gaps-dir>/
    └── <channel-name>/              # one per tracked non-thread channel, same name
        ├── README.md                # human-written strategy doc, skeleton from onboard
        ├── CURRENT_GAPS.md          # written by /gaps channel and /gaps proposal
        ├── gap-update-plan.md       # ephemeral, written by /gaps update, gitignored
        └── spec-snapshots/          # optional snapshots for inline-comment cursors
```

The folder name is always the channel's manifest name. A thread has no folder: its chatter is read by `/gaps channel <parent>` and lands in the parent's folder. Work that has no Discord channel of its own (a spec PR discussed only on GitHub, say) belongs to whichever channel owns it. Add the PR to that channel's `spec_prs`.

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
baseline_window: 4w
tags: []
notes: |
  Onboarded on YYYY-MM-DD via disctrack onboard.
```

Onboard refuses a URL from another guild, an existing `<name>.yaml`, and a channel id that another manifest already tracks. A non-thread channel also gets `<gaps-dir>/<name>/README.md` with a heading-only skeleton when that folder is missing. A thread (a `/threads/` URL, or `--parent <name>`) gets `kind: thread` and `parent_channel`, and no folder. Its parent must be tracked first.

### Fields the skills read

disctrack reads only the fields above. The `/gaps` skill reads the manifest yaml directly for everything else, and disctrack tolerates all of these. Every one is optional.

| Field | Read by | Meaning |
|---|---|---|
| `spec_prs` | channel, proposal | List of `{repo, pr, label}` spec PRs this channel owns. `/gaps proposal <pr-url>` resolves a PR to the channel that lists it here. |
| `code:` or `mcpkit:` | channel, proposal | Code-side `paths` (each `{path, topic}`) to git-log, plus an optional `commit`. The block name may be `code` or the project's `code.name`. |
| `references` | channel, proposal | External docs (`google-doc`, `ietf-draft`, `external-spec`, `meeting-notes`, `discussion`, `external-pr`) and local refs (`gap-table`, `spec-snapshot`, `spec-notes`, `pr-comments`), each with `label`, `url` or `location`, `last_manually_checked` and an optional `note`. |
| `ext_repos` | proposal | Other repos to watch for this channel (an SDK's implementation PRs, say). Free-form list of `{repo, note}` or `{repo, pr, label}`. |
| `related_channels` | channel, proposal | Names of other tracked channels whose chatter is secondary for this one. Read their recent messages for context. Gaps are still written only to this channel's folder. |
| `mcpconformance` | channel, proposal | Free-form block describing a conformance repo, branch, draft PR and paths that exercise this channel's topic. |
| `gapt` | all | `sources` (gapt source labels), optional `pr_filter`, `last_status_at`. |
| `worktree` | update | Default code-repo checkout for `/gaps update`, instead of `code.default_worktree`. |
| `repo` | update | GitHub repo to file or edit issues against, instead of `code.repo`. |
| `default_label` | update | Default `--label` filter for the existing-issue query. |

### Removed fields

`area_folder` and its older alias `proposal_folder` pointed a channel at a differently-named folder. They are gone. A manifest whose value differs from its own `name` fails to load with an error naming the file, so move the folder to match the channel name (or rename the channel) and delete the line. A value equal to `name` is accepted and ignored so a half-migrated root keeps working, and so is a thread whose value names its `parent_channel`. `disctrack onboard` never writes either key.

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
- `/gaps proposal`: requires `code.default_worktree`, optionally `gapt`. Discord messages are not read, but the proposal is still keyed by a tracked channel.
- `disctrack sync`: requires `discord.guild_id` and a logged-in Chromium profile (`discord.profile_dir`).
