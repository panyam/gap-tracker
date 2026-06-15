Refresh a single Discord channel: read messages from the local DB, gather context from gapt + code repo + area docs, synthesize a current-gaps view, write `CURRENT_GAPS.md` in the area folder.

**This skill is analysis-only — it never drives the browser.** Run `disctrack sync <name>` separately when you want fresh messages; this verb just reads what's already in the DB and synthesizes from it. The staleness warning that `disctrack messages` prints to stderr will surface if the DB hasn't caught up to your requested window.

Does NOT file issues. Does NOT modify README. Does NOT scrape Discord.

## Configuration

First, resolve the tracking root and load `<tracking-root>/.discord-tracking.yaml`. See `~/.claude/discord-tracking-playbook.md` for the resolver and config schema. Throughout this skill, placeholders like `<channels-dir>`, `<areas-dir>`, `<code.name>`, `<code.default_worktree>`, `<gapt.project_id>` refer to values from that config.

The `disctrack` Go binary (`~/newstack/gap-tracker/cmd/disctrack`, installed via `make install-disctrack`) does the resolution and lookups deterministically. Use it instead of manually walking the symlink:

- `disctrack config` — print the resolved tracking root and key fields (sanity check)
- `disctrack channel <name>` — print the channel_id from `<channels-dir>/<name>.yaml`
- `disctrack channels-dir` / `disctrack areas-dir` — print the absolute paths
- `disctrack area <name>` — print the absolute area folder path (must exist)
- `disctrack url <discord-url>` — parse `https://discord.com/channels/<g>/<c>`, print `"<g> <c>"`

If `disctrack config` fails (no `.discord-tracking.yaml` reachable from cwd), bail with its error message. Do NOT fall back to hardcoded paths.

## Arguments

`$ARGUMENTS` — `<channel-name> [<discord-url>] [--window <duration>]`

Two invocation forms:

1. **Subsequent (manifest exists)** — `/channel-refresh <channel-name>`. The skill resolves the name to a channel_id via `disctrack channel <name>`.
2. **First time (bootstrap)** — `/channel-refresh <channel-name> <discord-url>`. The first positional is the name to register; the second is a Discord channel URL (e.g. `https://discord.com/channels/931580580255125514/1116447544269082676`). The skill parses the URL via `disctrack url`, writes a minimum `<channels-dir>/<channel-name>.yaml` stub and the area folder, then bails with an instruction to run `disctrack sync <name>` first. A raw channel_id (all digits) is accepted as a second arg for back-compat but URL is preferred — it carries both guild + channel and is copy-pasted directly from Discord.

Other args:
- `--window <duration>` (optional, default `1d`) — freshness lens for the "what's new" section. Accepts:
  - Shorthand: `1d`, `3d`, `1w`, `2w`, `12h`
  - Natural language: `"since monday"`, `"yesterday"`, `"last 3 days"`, `"since 2026-05-08"`
  - Claude resolves the expression to an ISO 8601 timestamp before invoking `disctrack messages <channel> --since <iso>`. Git accepts the original expression directly via `--since`.
  - The window controls *only* the "Activity in window" highlight section. Cumulative state (channel position, code position, full gap registry) is synthesized from all available data plus the prior `CURRENT_GAPS.md`.

If the user wanted fresh data, they should have run `disctrack sync <channel-name>` before invoking this skill. This skill never spawns the Playwright scraper.

## Output

`<areas-dir>/<area-folder>/CURRENT_GAPS.md`. Where `<area-folder>` is the channel.yaml's `area_folder` field (legacy: `proposal_folder`) if set, otherwise the channel name.

The skill does NOT:
- File GitHub issues. That's a separate, explicit user step.
- Modify the area's `README.md`. Stale findings about README are surfaced in `CURRENT_GAPS.md` for the human to act on.
- Promote gaps between buckets (Discussing → Finalized → Ticketed). The user does that manually based on review.

## Steps

### 1. Validate inputs (fail fast, or bootstrap)

Run `disctrack channel <name>` to look up the channel_id from the manifest.

- **Exit 0** — manifest exists. Stdout is the channel_id. Read the manifest yaml directly to extract `spec_prs`, code-side `paths` (under either the `code:` block or the legacy `mcpkit:` block), `references`, optional `area_folder` (legacy: `proposal_folder`). Proceed.
- **Exit 1** — manifest doesn't exist. stderr lists available channels for did-you-mean. Two paths:
  - **Second positional arg passed** (bootstrap form) — proceed to bootstrap below.
  - **No second arg** — surface disctrack's error to the user verbatim (it already includes available channels). Append: `to bootstrap a new channel: /channel-refresh <name> <discord-url>`. Do not attempt sync, do not write CURRENT_GAPS.md, do not create stubs.

**Bootstrap path** — second arg present:

- If second arg matches `https?://(discord|discordapp)\.com/channels/...`, run `disctrack url <arg>` → stdout is `"<guild-id> <channel-id>"`. Use the channel-id.
- If second arg is all digits, accept it as the channel-id (back-compat).
- Otherwise: error out, instruct the user to pass a Discord URL.

Write the manifest:

```yaml
# <channels-dir>/<name>.yaml
name: <name>
channel_id: "<channel-id>"
display_name: "<name>"          # human can refine; sync may auto-update later
status: active
kind: text                       # text | thread | forum; default text
# parent_channel: <name>         # set when kind: thread; references another channel by name or id
baseline_window: 4w
tags: []
notes: |
  Bootstrapped on YYYY-MM-DD via /channel-refresh.
```

For Discord threads: write the same manifest by hand with `kind: thread` and `parent_channel: <parent-name>`. Once present, `disctrack threads list <parent>` will surface it, and ingest writes the parent's channel_id into the `parent_channel_id` column on each message row.

Then determine the area folder. If `<areas-dir>/<name>/` doesn't exist, create it with a minimal `README.md` skeleton (headings only — Summary / Status / Strategy / Next steps / When you come back). The skill is bootstrapping the entire tracked-channel layout in one shot.

After bootstrap (or with an existing manifest), determine area folder: explicit `area_folder` (legacy: `proposal_folder`) field in channel.yaml, or `<areas-dir>/<channel-name>/`. Use `disctrack area <slug>` to verify the folder exists (exit 1 with did-you-mean if it doesn't). **If the folder doesn't exist and we didn't just bootstrap it**, bail with disctrack's error.

**If we just bootstrapped (newly-written channel.yaml and/or fresh area folder)**: do NOT proceed to synthesis. The local DB has no data for this channel yet. Print a clear message:

```
bootstrap complete:
  channel manifest:  <channels-dir>/<name>.yaml
  area folder:       <areas-dir>/<name>/
next step: run `disctrack sync <name>` to populate the DB, then re-run /channel-refresh <name>.
```

Then exit. The synthesis steps below only make sense once `disctrack sync` has been run at least once for this channel.

### 2. Gather deterministic context

Resolve `--window` to two forms before this step:
- `WINDOW_ISO`: an ISO 8601 timestamp (for `disctrack messages <channel> --since <iso>`). Compute relative to "now in UTC."
- `WINDOW_GIT`: the original expression passed straight to `git log --since="..."` (git parses natural language directly).

Then gather, in parallel where possible:

- **Window Discord messages.** `disctrack messages <channel-name> --since <WINDOW_ISO>` — drives the "Activity in window" section. disctrack handles name → channel_id resolution internally via the same `.discord-tracking.yaml` walk-up the resolver does. `--since` accepts either an ISO 8601 timestamp or a number-of-minutes shorthand (e.g. `--since 60` = last hour).
- **Cumulative Discord context.** `disctrack messages <channel-name> --since <BASELINE_ISO>` where `BASELINE_ISO` is ~6 weeks ago, or older if the area folder was last refreshed beyond that. Drives the cumulative channel-position synthesis.
- **gapt classification** (only if `gapt.enabled` in config). For each `spec_prs[]` entry in channel.yaml: `gapt show <gapt.project_id> <source>:<pr>`. If gapt has no source for the repo, surface as a finding ("gapt config gap").
- **gapt status** (only if `gapt.enabled`). `gapt status <gapt.project_id> -o yaml | grep -A2 <relevant-source>`.
- **Code-repo git activity (window).** `git -C <code.default_worktree> log --oneline --since="<WINDOW_GIT>" -- <paths-from-channel.yaml>` — drives the "Activity in window" section's code-side bullet.
- **Code-repo git activity (cumulative).** `git -C <code.default_worktree> log --oneline --since="6 weeks ago" -- <paths-from-channel.yaml>` — drives the cumulative code-position synthesis. Override per channel.yaml if it specifies a different `checkout:` field.
- **Area context.** Read `<areas-dir>/<folder>/README.md` and the most recent `refresh-*.md` (sorted by filename date).
- **Existing CURRENT_GAPS.md** if present. Several responsibilities when re-running:
  - **Preserve `Ticketed` and `Closed` sections verbatim.** Those carry history the synthesis shouldn't overwrite.
  - **Walk existing Pending checklists.** For each gap, scan the `**Pending**` list. Items the skill can verify as done (commit landed, comment posted, file mtime moved past the cited date, issue closed) get flipped from `[ ]` to `[x]` with a verifier reference (commit SHA, comment ID, issue URL). Items that became invalid get `[~]` with a one-line reason. New items get appended.
  - **Don't re-propose what's already tracked.** Before drafting any new Discussing or Finalized entries, scan all sections for subject overlap. If a candidate gap looks substantially the same as an existing tracked one (same SEP/feature/decision), skip it. If ambiguous, surface in "Notes for next refresh" rather than re-proposing.
  - **Stable IDs.** Read existing gap IDs in the file. When moving an existing gap between sections (e.g., the user promoted G3 from Discussing → Finalized), preserve `G3`. New gaps get `G<max-existing + 1>`. IDs never decrement and never collide.

### 3. Synthesize CURRENT_GAPS.md

**Frame**: a "gap" is a **delta between (channel/spec) and (`<code.name>`)** — places where upstream and our implementation are out of sync. Things that don't fit that frame go elsewhere:

- Pure channel/spec activity with no code hook → covered in "Activity in window," not promoted to a gap.
- Pure code-repo refactors with no spec hook → that repo's own backlog, not here.
- "Things to research/discuss" that don't yet describe a delta → fine as a Pending checklist item under an existing gap, not their own gap.

Compare:
- What the channel says happened (messages, recent threads)
- Where `<code.name>` stands (gapt classification + git log)
- What README claims (strategic position)
- What the existing CURRENT_GAPS.md already tracked

Produce a markdown file with these sections in order:

```markdown
# Current gaps: <channel-name>

Last refreshed: YYYY-MM-DD by /channel-refresh [--window <w>]
Window: <ISO_START> → <ISO_END>

This file tracks **deltas between (channel/spec) and (<code.name>)** — places where upstream and our implementation are out of sync. Each gap names the spec side, the code side, and what would close the delta. Sections track lifecycle: **Discussing** (delta exists, direction unclear) → **Finalized** (direction decided, work pending) → **Ticketed** (filed as upstream issue or downstream issue/PR) → **Closed** (delta gone, by alignment OR intentional divergence). Gap IDs are stable across runs. The human owns transitions between sections.

## Activity in window

Bulleted. What happened since `<ISO_START>`:

- Discord: <N messages from M authors; one-line characterization of each substantive thread>
- <code.name>: <commits in declared paths within window, or "no commits">
- gapt: <classification changes within window, or "no classification changes">
- Spec PR: <new comments/commits if discoverable, otherwise note "not synced via gapt yet">

If the window is empty (nothing happened), say so in one line. Don't pad.

## Summary

One paragraph cumulative view: what's the channel converging toward, where does <code.name> stand relative to that, what's the strategic situation right now. Window-independent.

## Discussing

For each gap where the delta exists and the direction is still unclear:

### G<n>: <short title>

**Where this stands**: <one-line tl;dr the human can read in 5 seconds>

**Spec/channel side**: <citation — comment ID + author + date, or message ID + author + date — what upstream is saying>
**<code.name> side**: <file:line citation, commit SHA, or "no impl yet" — where we actually are>
**Closes when**: <criterion — what state of the world makes this gap gone>

**Pending**:
- [ ] <action item — research, decision, draft a reply, run a grep, etc.>
- [ ] <action item>
- [x] <done item> — <verifier reference: commit SHA, comment ID, file path:line, issue URL>
- [~] <invalidated item> — <one-line reason>

## Finalized

Same shape as Discussing. Difference: the direction is decided. "Where this stands" reads like a conclusion ("ready to file as `<code.name>` issue: X"). Pending items are concrete enough to file as tickets.

## Ticketed

Carry over from existing CURRENT_GAPS.md. Format:

### G<n>: <title> → <repo>#<N> (filed YYYY-MM-DD)

**Where this stands**: <one-line — issue state + what's blocking close>

**Spec/channel side**: <unchanged from when filed, or noted if upstream has shifted>
**<code.name> side**: <pointer to PR/branch if work is in flight>
**Closes when**: <unchanged from when filed>

**Pending** (post-filing follow-ups):
- [ ] <e.g., shepherd review, update on upstream movement>

## Closed

Carry over from existing CURRENT_GAPS.md. Format:

### G<n>: <title> — closed YYYY-MM-DD

<one-line resolution: how the delta went away (alignment, intentional divergence, upstream withdrew, etc.)>

Closed gaps are history. No Pending list.

## Where README looks stale

Bullets. Quote the stale claim and contrast with current reality. Do NOT auto-fix README; surface for the user to review.

- README says "X" — DB / gapt / `<code.name>` shows "Y" (as of <ts>).

## Notes for next refresh

Anything the user should think about by next invocation. Stale Pending items (open `[ ]` >2 weeks since last update), auth-gated docs that need manual review (Google Docs, IETF drafts), unsynced sources (Discord threads, gapt config gaps).
```

**Per-gap shape rules**:

- **Title**: ≤80 chars, names the delta (not the activity). Good: "G3: requestState removed from spec, still emitted by `<code.name>`." Bad: "G3: SEP-author replied to comment 123."
- **Where this stands**: one line. If you can't say it in one line, the gap is too big and should be split.
- **Spec/channel side** + **`<code.name>` side**: always cite a specific anchor (comment ID, message ID, file:line, commit SHA). If "no impl yet," say so explicitly.
- **Closes when**: a state-of-the-world criterion, not a task. "`<code.name>` emits the new method name" is a criterion. "Rename the constant" is a Pending item.
- **Pending**: verb-led, one line each. Use `[ ]` open, `[x]` done with verifier, `[~]` invalidated with reason. When an item becomes ripe to file as an issue, the human files it via `gh issue create` (or `/gap-update`) and replaces the `[ ]` with `[→ <repo>#<N>]`. The gap moves to `Ticketed` when its primary work has an issue.

**No paste-prompt blocks.** Don't include `**Evaluate prompt**` blocks. The slim shape is meant to be read, not pasted. When a gap is ripe enough to need a fresh session for grunt-work, the human files an issue and the issue body carries the work into that session.

### 4. Write the file

Write to `<areas-dir>/<folder>/CURRENT_GAPS.md`. Overwrite any existing file. Preserve `Ticketed` and `Closed` sections by reading them from the prior file first.

### 5. Print a summary

To stdout (not to the file):

```
channel-refresh: <channel-name>
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
# Skip this if the local DB is already current.
for ch in <ch1> <ch2> <ch3>; do
  disctrack sync "$ch"
done

# Step 2: fan out the synthesis (parallel-safe — no shared state, no
# browser, just DB reads + synthesis).
# In Claude: spawn N subagents, each running /channel-refresh <ch>
```

The skill itself is single-channel and stateless across invocations — the parent loop or subagent fan-out provides the orchestration. `/channel-refresh` never touches the browser, so it's safe to fan out without worrying about Playwright profile contention.

## Style notes

- Cite channel messages by author + date, not by full content quote. The DB is the audit trail.
- Cite gapt rationale + classified_at date when leaning on its classification.
- Be specific about code-repo state. "`<code.name>` ships X at commit Y" beats "`<code.name>` has X."
- For the README-stale section: quote the stale claim verbatim (in backticks) so the user can find and edit it.
- No em dashes, no semicolons in committed prose (per user style).

## Gotchas

- **Sync is a separate user step.** This skill never invokes `disctrack sync` and never opens the browser. If the user wanted fresh data they should have synced before calling /channel-refresh. The staleness warning in step 5's summary tells them when to re-sync.
- **Area folder may not match channel name 1:1.** Example: channel `auth-wg-fine-grained-authz` → area folder `fine-grained-auth`. Use the `area_folder:` (legacy: `proposal_folder:`) field in channel.yaml when they differ.
- **gapt config gap.** If `gapt.enabled` is true but `gapt show <gapt.project_id> <repo>:<pr>` errors because gapt has no source for `<repo>`, surface as a finding under "Notes for next refresh" rather than failing. Tell the user how to add the source.
- **CURRENT_GAPS.md is the only artifact this skill writes.** Don't touch README, don't write state.md, don't create issues. Those are separate, deliberate user actions.
- **Threads aren't synced.** Discord threads have their own channel IDs but channel.yaml doesn't enumerate them. Surface thread starts as "unsynced thread" findings in "Notes for next refresh."
- **No fallback to hardcoded paths.** If config resolution fails, refuse to run with the playbook's error message. Do not guess paths.
