---
name: gaps
description: "Track and reconcile deltas between upstream (Discord channels, spec PRs, external docs) and our code repo, using the gap-tracker DB and per-area CURRENT_GAPS.md files. Three modes over one artifact: `channel` synthesizes a Discord channel's gap view, `proposal` synthesizes the non-Discord sources (spec PR activity, code-repo git log, external doc reviews), and `update` reconciles an area's gaps against the code repo and open issue list, then mutates both CURRENT_GAPS.md and gh state. Use when the user says \"refresh the channel\", \"refresh the proposals\", \"update gaps for <area>\", \"reconcile the gaps\", \"what changed upstream\", \"sync CURRENT_GAPS with the issues\", or names a Discord channel or spec PR to re-synthesize. Invoke as `/gaps channel`, `/gaps proposal`, or `/gaps update`."
---

# gaps

Deltas between upstream and our implementation, tracked per area in `CURRENT_GAPS.md`.

## Modes

Read the matching reference file, then follow it. Do not improvise a mode.

| Invocation | Reference | Writes | What it does |
|---|---|---|---|
| `/gaps channel <name> [url] [--window <w>]` | `references/channel.md` | `CURRENT_GAPS.md` | Synthesizes a Discord channel's gap view from the local DB. |
| `/gaps proposal <area\|pr-url> [url] [--window <w>]` | `references/proposal.md` | `CURRENT_GAPS.md` | Synthesizes spec PR activity, code-repo git log, and external doc reviews. |
| `/gaps update <area> [flags]` | `references/update.md` | `CURRENT_GAPS.md` **and** `gh` | Reconciles gaps against code and issues, then executes approved actions. |

`channel` and `proposal` are counterparts writing the same file. Areas with both
Discord chatter and PR activity want both, and PR-primary areas with no channel
want only `proposal`. They coexist through stable gap IDs and the
don't-re-propose rule. `update` runs after either one, and takes its input at
face value, so refresh before reconciling if the file is stale.

Bare `/gaps` with no mode: ask which. The three do different things to different
artifacts, and only one of them mutates GitHub.

## Configuration

Resolve the tracking root and load `<tracking-root>/.discord-tracking.yaml`. See
`${CLAUDE_PLUGIN_ROOT}/reference/discord-tracking-playbook.md` for the resolver and the config schema.
Throughout all three modes, placeholders like `<channels-dir>`, `<areas-dir>`,
`<code.name>`, `<code.repo>`, `<code.default_worktree>` and `<gapt.project_id>`
refer to values from that config.

The `disctrack` Go binary (`~/newstack/gap-tracker/cmd/disctrack`, installed via
`make install-disctrack`) does resolution and lookups deterministically. Use it
rather than walking the symlink by hand:

- `disctrack config` prints the resolved tracking root and key fields (sanity check)
- `disctrack channel <name>` prints the channel_id from `<channels-dir>/<name>.yaml`
- `disctrack channels-dir` / `disctrack areas-dir` print the absolute paths
- `disctrack area <name>` prints the absolute area folder path (must exist)
- `disctrack url <discord-url>` parses `https://discord.com/channels/<g>/<c>` and prints `"<g> <c>"`

**No fallback to hardcoded paths.** If `disctrack config` fails, meaning no
`.discord-tracking.yaml` is reachable from cwd, bail with its error message and
refuse to run.

## The `--window` lens

Optional, default `1d`. It controls **only** the "Activity in window" section.
Cumulative state (channel position, code position, the full gap registry) is
synthesized from all available data plus the prior `CURRENT_GAPS.md`, so a narrow
window never truncates the registry.

Resolve `--window` into three forms before gathering anything:

- `WINDOW_ISO`, an ISO 8601 timestamp computed relative to now in UTC, for `disctrack messages --since` and for `gh api` filtering where supported
- `WINDOW_GIT`, the original expression passed straight to `git log --since="..."`, since git parses natural language directly
- `WINDOW_GH`, an ISO timestamp for `gh api` filtering

Accepts shorthand (`1d`, `3d`, `1w`, `2w`, `12h`) or natural language
(`"since monday"`, `"yesterday"`, `"last 3 days"`, `"since 2026-05-08"`). Resolve
the expression to an ISO 8601 timestamp before invoking `disctrack messages
<channel> --since <iso>`. Git accepts the original expression directly via
`--since`.

## What a gap is

A gap is a **delta between upstream (channel or spec) and `<code.name>`**, meaning
a place where the two are out of sync. Things that don't fit that frame go
elsewhere:

- Pure channel or spec activity with no code hook belongs in "Activity in window", not promoted to a gap.
- Pure code-repo refactors with no spec hook belong in that repo's own backlog.
- "Things to research or discuss" that don't yet describe a delta are fine as a Pending checklist item under an existing gap, rather than a gap of their own.

## Lifecycle

**Discussing** (delta exists, direction unclear) → **Finalized** (direction
decided, work pending) → **Ticketed** (filed as an upstream issue, or a
downstream issue or PR) → **Closed** (delta gone, either by alignment or by
intentional divergence).

Gap IDs are stable across runs. **The human owns transitions between sections.**
No mode promotes a gap between buckets on its own. In `update`, a transition is
the *result* of executing an approved action, never a separate move.

## CURRENT_GAPS.md

`channel` and `proposal` regenerate this file, and `update` edits it in place.
The full section-by-section generation template, plus the four responsibilities
that apply when re-running over an existing file, live in
`references/format.md`. Read it before synthesizing. The per-gap shape rules
below apply to every mode, including `update`.

## Per-gap shape rules

- **Title**: 80 chars or fewer, naming the delta rather than the activity. Good: "G3: requestState removed from spec, still emitted by `<code.name>`." Bad: "G3: SEP-author replied to comment 123."
- **Where this stands**: one line. A gap you cannot describe in one line is too big and should be split.
- **Spec/channel side** and **`<code.name>` side**: always cite a specific anchor, meaning a comment ID, message ID, file:line, or commit SHA. If there is no implementation yet, say so explicitly.
- **Closes when**: a state-of-the-world criterion rather than a task. "`<code.name>` emits the new method name" is a criterion. "Rename the constant" is a Pending item.
- **Pending**: verb-led, one line each. Use `[ ]` open, `[x]` done with a verifier, `[~]` invalidated with a reason. When an item becomes ripe to file, the human files it via `gh issue create` (or `/gaps update`) and replaces the `[ ]` with `[→ <repo>#<N>]`. The gap moves to `Ticketed` when its primary work has an issue.
- **No paste-prompt blocks.** Don't include `**Evaluate prompt**` blocks. The slim shape is meant to be read rather than pasted. When a gap is ripe enough to need a fresh session for grunt-work, the human files an issue and the issue body carries the work into that session.

## Style notes

- **No em dashes, no semicolons in committed prose.** SQL and JSON are exempt. This covers `CURRENT_GAPS.md`, plan files, and issue bodies.
- Cite PR comments by comment ID plus author plus date when surfacing them in synthesis.
- Cite gapt rationale plus `classified_at` when leaning on a classification.
- Cite worktree state by short SHA plus file:line. "verified at `9b913b62`, `events.go:824`" reads better than "verified in the codebase."
- For external doc reminders, include the URL inline so the user can click through to re-read.

## Shared gotchas

- **`gh` calls require `GH_TOKEN="$GH_PERSONAL_TOKEN"`.** Personal repos are not accessible via the EMU account.
- **Auth-gated docs cannot be fetched.** Google Docs, Notion and the like get surfaced as manual-review reminders. Never attempt to fetch them.
- **Never modify the area's `README.md`.** Stale findings about README stay surfaced in `CURRENT_GAPS.md` for the human to act on.
- **gapt source mismatch.** An `area.yaml` `spec_prs[].repo` may not be in gapt's `sources[]`. Surface that as a finding rather than failing.
- **gapt re-classification is out of scope for every mode.** Surface the current classification, rationale and classified-at date as evidence. Reclassifying is a separate `gapt classify` workflow.
- **Backlinks already created** by past commits or PRs cannot be retroactively removed from GitHub's cross-reference system. Editing a plan or body before execution prevents new backlinks, and old ones stay.
