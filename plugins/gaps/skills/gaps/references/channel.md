# /gaps channel

Refresh a single Discord channel: read messages from the local DB, gather context
from gapt plus the code repo plus area docs, synthesize a current-gaps view, and
write `CURRENT_GAPS.md` in the area folder.

**Analysis-only. This mode never drives the browser.** Run `disctrack sync <name>`
separately when you want fresh messages. This verb reads what is already in the DB
and synthesizes from it. The staleness warning that `disctrack messages` prints to
stderr surfaces if the DB has not caught up to your requested window.

Does NOT file issues. Does NOT modify README. Does NOT scrape Discord.

Shared conventions live in `SKILL.md` and this mode depends on them. Read it first
for the **config resolver and `disctrack` CLI**, the **`--window` lens** and its
three resolved forms, the **gap frame and lifecycle**, the **per-gap shape
rules**, and the **shared style notes and gotchas**. Then read
`references/format.md` for the generation template and the re-run
responsibilities.

## Arguments

`<channel-name> [<discord-url>] [--window <duration>]`

Two invocation forms:

1. **Subsequent (manifest exists)**, `/gaps channel <channel-name>`. Resolves the name to a channel_id via `disctrack channel <name>`.
2. **First time (bootstrap)**, `/gaps channel <channel-name> <discord-url>`. The first positional is the name to register, and the second is a Discord channel URL (e.g. `https://discord.com/channels/931580580255125514/1116447544269082676`). Bootstrap is one command, `disctrack onboard <channel-name> <discord-url>`, which writes the manifest and the area folder. Then bail with an instruction to run `disctrack sync <name>` first. A raw channel_id (all digits) is accepted as a second arg for back-compat, though a URL is preferred because it carries both guild and channel and is copy-pasted straight from Discord.

If the user wanted fresh data, they should have run `disctrack sync <channel-name>` before invoking this mode. It never spawns the Playwright scraper.

## Output

`<areas-dir>/<area-folder>/CURRENT_GAPS.md`, where `<area-folder>` is the
channel.yaml's `area_folder` field (legacy: `proposal_folder`) if set, otherwise
the channel name.

This mode does NOT:
- File GitHub issues. That is a separate, explicit user step.
- Modify the area's `README.md`. Stale findings about README get surfaced in `CURRENT_GAPS.md` for the human to act on.
- Promote gaps between buckets. The user does that manually based on review.

## Steps

### 1. Validate inputs (fail fast, or bootstrap)

Run `disctrack channel <name>` to look up the channel_id from the manifest.

- **Exit 0** means the manifest exists. Stdout is the channel_id. Read the manifest yaml directly to extract `spec_prs`, code-side `paths` (under either the `code:` block or the legacy `mcpkit:` block), `references`, optional `area_folder` (legacy: `proposal_folder`). Proceed.
- **Exit 1** means the manifest doesn't exist. stderr lists available channels for did-you-mean. Two paths:
  - **Second positional arg passed** (bootstrap form), so proceed to bootstrap below.
  - **No second arg**, so surface disctrack's error to the user verbatim (it already includes available channels). Append: `to bootstrap a new channel: /gaps channel <name> <discord-url>`. Do not attempt sync, do not write CURRENT_GAPS.md, do not create stubs.

**Bootstrap path**, second arg present. Run:

```bash
disctrack onboard <name> <discord-url-or-channel-id>
```

`disctrack onboard` does the whole bootstrap and refuses rather than overwrite:

- It parses channel URLs and thread URLs (`.../channels/<guild>/<parent>/threads/<thread>`), and accepts a bare channel id.
- It refuses a URL whose guild is not `discord.guild_id`, a name whose `<channels-dir>/<name>.yaml` already exists, and a channel id another manifest already tracks (the error names that manifest).
- It writes `<channels-dir>/<name>.yaml` in the current schema (`name`, `channel_id`, `display_name`, `status: active`, `kind`, `baseline_window: 4w`, `tags`, and a `notes` line recording the onboard date).
- For a text channel it creates `<areas-dir>/<name>/README.md` with the heading-only skeleton (Summary / Status / Strategy / Next steps / When you come back) when the folder is missing. `--area <folder>` sets `area_folder:` to a different folder instead.
- For a thread URL it writes `kind: thread` and `parent_channel: <parent-name>`, and creates no area folder. The parent must already be tracked. If it isn't, onboard prints the `disctrack onboard` command for the parent. `--parent <name>` covers a thread URL or id without the parent segment.

Surface any refusal to the user verbatim and stop. Pass `--dry-run` first if the user wants to see the manifest before it is written.

Threads rarely need onboarding by hand. `disctrack sync <parent>` finds every thread started in the parent, writes a manifest for each untracked one, and syncs them all (see Gotchas).

After bootstrap (or with an existing manifest), determine area folder: explicit `area_folder` (legacy: `proposal_folder`) field in channel.yaml, or `<areas-dir>/<channel-name>/`. Use `disctrack area <slug>` to verify the folder exists (exit 1 with did-you-mean if it doesn't). **If the folder doesn't exist and we didn't just bootstrap it**, bail with disctrack's error.

**If we just bootstrapped (newly-written channel.yaml and/or fresh area folder)**: do NOT proceed to synthesis. The local DB has no data for this channel yet. Print a clear message:

```
bootstrap complete:
  channel manifest:  <channels-dir>/<name>.yaml
  area folder:       <areas-dir>/<name>/
next step: run `disctrack sync <name>` to populate the DB, then re-run /gaps channel <name>.
```

Then exit. The synthesis steps below only make sense once `disctrack sync` has been run at least once for this channel.

### 2. Gather deterministic context

Resolve `--window` into `WINDOW_ISO` / `WINDOW_GIT` / `WINDOW_GH` first (see `SKILL.md`), then gather, in parallel where possible:

- **Window Discord messages.** `disctrack messages <channel-name> --since <WINDOW_ISO>` drives the "Activity in window" section. disctrack handles name → channel_id resolution internally via the same `.discord-tracking.yaml` walk-up the resolver does. `--since` accepts either an ISO 8601 timestamp or a number-of-minutes shorthand (e.g. `--since 60` = last hour).
- **Cumulative Discord context.** `disctrack messages <channel-name> --since <BASELINE_ISO>` where `BASELINE_ISO` is ~6 weeks ago, or older if the area folder was last refreshed beyond that. Drives the cumulative channel-position synthesis.
- **gapt classification** (only if `gapt.enabled` in config). For each `spec_prs[]` entry in channel.yaml: `gapt show <gapt.project_id> <source>:<pr>`. If gapt has no source for the repo, surface as a finding ("gapt config gap").
- **gapt status** (only if `gapt.enabled`). `gapt status <gapt.project_id> -o yaml | grep -A2 <relevant-source>`.
- **Code-repo git activity (window).** `git -C <code.default_worktree> log --oneline --since="<WINDOW_GIT>" -- <paths-from-channel.yaml>` drives the "Activity in window" section's code-side bullet.
- **Code-repo git activity (cumulative).** `git -C <code.default_worktree> log --oneline --since="6 weeks ago" -- <paths-from-channel.yaml>` drives the cumulative code-position synthesis. Override per channel.yaml if it specifies a different `checkout:` field.
- **Area context.** Read `<areas-dir>/<folder>/README.md` and the most recent `refresh-*.md` (sorted by filename date).
- **Existing CURRENT_GAPS.md** if present. Apply the four re-run responsibilities from `references/format.md`, which cover preserving `Ticketed` and `Closed`, walking Pending checklists, not re-proposing tracked gaps, and keeping gap IDs stable.

### 3. Synthesize CURRENT_GAPS.md

Follow the gap frame and per-gap shape rules in `SKILL.md`, and the generation
template in `references/format.md`. The header line reads `Last refreshed: YYYY-MM-DD by /gaps channel
[--window <w>]`.

Compare:
- What the channel says happened (messages, recent threads)
- Where `<code.name>` stands (gapt classification plus git log)
- What README claims (strategic position)
- What the existing CURRENT_GAPS.md already tracked

The **Spec/channel side** field cites Discord message IDs plus author plus date.

### 4. Write the file

Write to `<areas-dir>/<folder>/CURRENT_GAPS.md`. Overwrite any existing file. Preserve `Ticketed` and `Closed` sections by reading them from the prior file first.

### 5. Print a summary

To stdout (not to the file):

```
gaps channel: <channel-name>
  messages read: <N> (since <window>) [+ <S> staleness warning if applicable]
  gaps: <D discussing, F finalized, T ticketed, C closed>
  README stale: <R findings>
  written: <areas-dir>/<folder>/CURRENT_GAPS.md
```

If `disctrack messages` emitted a staleness warning to stderr (cursor far older than the requested window), surface it inline so the user knows whether to run `disctrack sync` and re-invoke.

## Parallel sweep pattern

When the user wants to refresh multiple channels:

```bash
# Step 1: pre-sync sequentially (Playwright profile is single-tenant).
# Each sync also covers that channel's threads.
# Skip this if the local DB is already current.
for ch in <ch1> <ch2> <ch3>; do
  disctrack sync "$ch"
done

# Step 2: fan out the synthesis (parallel-safe, no shared state, no
# browser, just DB reads + synthesis).
# In Claude: spawn N subagents, each running /gaps channel <ch>
```

The skill itself is single-channel and stateless across invocations, and the parent loop or subagent fan-out provides the orchestration. `/gaps channel` never touches the browser, so it's safe to fan out without worrying about Playwright profile contention.

## Gotchas

- **Sync is a separate user step.** This mode never invokes `disctrack sync` and never opens the browser. The staleness warning in the step 5 summary tells the user when to re-sync.
- **Area folder may not match channel name 1:1.** Channel `auth-wg-fine-grained-authz` maps to area folder `fine-grained-auth`, for example. Use the `area_folder:` (legacy: `proposal_folder:`) field in channel.yaml when they differ.
- **Threads sync with their parent.** `disctrack sync <parent>` tracks every thread started in the parent (it writes `<parent>-<title-slug>-thread.yaml` with `kind: thread`) and syncs all of the parent's active threads in the same browser session. Read thread activity with `disctrack threads list <parent>` and then `disctrack messages <thread-name>`. A thread whose id the parent sync could not capture shows up in the sync summary as `unresolved_markers` and in `disctrack threads discover <parent> --untracked`. Surface those under "Notes for next refresh". `--no-threads` on sync skips all of this.
- **CURRENT_GAPS.md is the only artifact this mode writes.** Don't touch README, don't write state.md, don't create issues.
