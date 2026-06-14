Refresh an interest area: pull deltas from Discord (Playwright path), spec PRs, gapt, and code-repo commits; surface stale state in our own docs; emit a refresh document.

## Configuration

First, resolve the tracking root and load `<tracking-root>/.discord-tracking.yaml`. See `~/.claude/discord-tracking-playbook.md` for the resolver and config schema. `<areas-dir>`, `<code.area_refresh_checkout>` (with `<code.default_worktree>` as fallback), `<code.name>`, `<discord.guild_id>`, `<discord.parse_snapshot_script>`, `<discord.setup_doc>`, `<gapt.project_id>`, `<gapt.config_path>` refer to values from that config.

If the resolver fails, bail with the playbook's error message.

This skill uses the Playwright path for Discord pulls. If `<discord.parse_snapshot_script>` is not set in config, the Discord per-channel pulls are skipped (the rest of the refresh still runs). Use `/channel-refresh` for the lighter `<sync_tool>`-based Discord path.

## Arguments

`$ARGUMENTS` — `<area-name> [--checkout <path>] [--baseline-window 4w]`

- `<area-name>` (required) — folder under `<areas-dir>/` containing an `area.yaml`.
- `--checkout <path>` (optional) — override code-repo checkout location. Default: `<code.area_refresh_checkout>` (falls back to `<code.default_worktree>`).
- `--baseline-window 4w` (optional) — on cold-start (cursor null), pull this much Discord history instead of stamp-only. Default: stamp-only.

## Output

`<areas-dir>/<area-name>/refresh-YYYY-MM-DD.md` (today's date in the filename).

**Does NOT modify `area.yaml`.** Cursor stamps are surfaced in the output doc; user applies them manually after reading. This makes refresh idempotent and prevents losing "what was new last time" state before the user has read it.

## Inputs

- `<areas-dir>/<area-name>/area.yaml` — the area manifest (required)
- `<gapt.config_path>` — for cross-reference only (not used as the path source). Only consulted if `gapt.enabled`.

## Manifest schema (relevant top-level fields)

- `name:` — string, must match folder name
- `status:` — `active` | `parked` | `dormant`
- `groups:` — optional list of group tags. Areas tagged with the same group can be batch-refreshed via `/group-refresh <group-name>` (if that skill is available). Useful for cross-cutting topics.
- `discord:`, `spec_prs:`, `gapt:`, `code:` (or legacy `mcpkit:`), `references:` — see existing `<areas-dir>/<any>/area.yaml` for the full per-source shape.

### Cursor field notes (per-source)

- `discord[].last_message_timestamp` (ISO 8601 string) — **preferred Discord cursor.** Surfaced via accessibility-snapshot's `<time>` elements. Discord message IDs are *not* exposed in the snapshot YAML, so `last_message_id` cannot be reliably stamped from a Playwright pull. Timestamps work — they're monotonic per channel and the snapshot's `<time datetime="...">` attribute is stable.
- `discord[].last_message_id` — kept as a forward-compat field; populated only if extracted via a different mechanism (direct DOM query, etc.). Refresh's diff path should fall back to timestamp comparison.
- `references[].last_manually_checked` (date string `YYYY-MM-DD`) — when this reference was last reviewed by a human. For local files (`gap-table`, `spec-snapshot`, `pr-comments`, `channel-snapshot`), refresh auto-stamps via file mtime as a suggestion; user confirms. For external types (`google-doc`, `ietf-draft`, `external-spec`), the human stamps explicitly after reading.
- `references[].url` — **soft-required** when `type:` is `google-doc`, `ietf-draft`, `external-spec`, `discussion`, or `meeting-notes`. Refresh emits a schema warning if the field is null for these types (see Step 1).

## Steps

### 1. Parse and validate

Read `<areas-dir>/<area-name>/area.yaml`. Bail with a clear error if the file doesn't exist.

**Hard-stops** (refuse to run, fix needed before refresh works):
- Any `spec_prs[]` entry where `GH_TOKEN="$GH_PERSONAL_TOKEN" gh pr view <pr> --repo <repo> --json number` fails. Print the failing `<repo>#<pr>` pair and the gh error verbatim. This is the only hard-stop.

**Schema warnings** (don't fail — surface in output):
- Any `discord[].channel_id: null` → "channel ID missing, source skipped"
- Any `references[].url: null` where `type:` is `google-doc` / `ietf-draft` / `external-spec` / `discussion` / `meeting-notes` → "external reference missing url, manual review can't open the doc." (Other reference types — `gap-table`, `spec-snapshot`, `umbrella-issue`, etc. — don't need a url.)
- Any `references[].last_manually_checked: null` → "never reviewed" reminder (see References section in Step 3 for nag thresholds).
- **Undeclared `*-notes.md` / `*-analysis.md` files in the area folder** that aren't listed in `references[]` → "area folder has files not in manifest: <list>".
- For each `spec_prs[].repo`: if `gapt.enabled`, check it's in `<gapt.config_path>` `sources[]`. If not, emit "gapt has no source for `<repo>`; the gapt section will be empty for this area." Suggest adding it to gapt config as a manual follow-up.
- **Status-vs-activity mismatch**: if `area.yaml status` is `parked` or `dormant` AND committed activity in declared code-side `paths` exceeds 5 commits in the last 7 days (use `git log --since="7 days ago" --oneline -- <paths> | wc -l` on the synced checkout from Step 2), flag "status field may be stale (saw N commits in last 7d)."

### 2. Sync code-repo checkout

```
git -C <checkout> fetch && git -C <checkout> pull --ff-only
git -C <checkout> rev-parse HEAD
git -C <checkout> status --short
```

Idempotent and cheap. Warn (don't fail) if `--ff-only` rejects — means the local checkout has diverged and the user should know.

Record HEAD as the candidate `<code-block>.commit` cursor stamp.

If `git status --short` returns non-empty output, **surface this verbatim in the refresh doc under "Working-tree state"** so the user knows the inventory was taken with uncommitted edits in flight. Don't try to interpret — just show.

### 3. Per-source pulls

For each source in `area.yaml`, follow these rules. **Cursor null = baseline (record latest, don't summarize history). Cursor non-null = diff from cursor forward.**

#### Discord (per channel entry)

Only runs if `<discord.parse_snapshot_script>` is set in config. Otherwise: emit a one-line note in the refresh doc ("Discord pulls require `discord.parse_snapshot_script` in tracking config; use `/channel-refresh` for the lighter sync-tool path") and skip.

If `channel_id` is null: emit schema warning, skip.

Otherwise:
- Navigate via Playwright: `mcp__playwright__browser_navigate` to `https://discord.com/channels/<discord.guild_id>/<channel_id>`
- `mcp__playwright__browser_snapshot`
- Parse via `python3 <discord.parse_snapshot_script>`
- **Always re-snapshot** even if same-day on-disk snapshot exists (snapshots can be stale within a day).
- **Use `last_message_timestamp` as the cursor**, not `last_message_id`. The accessibility-snapshot exposes `<time datetime="...">` attributes per message but does not expose Discord message IDs. Comparison is timestamp-based: messages newer than `last_message_timestamp` are "new."
- Cold-start (cursor null): record latest message timestamp as baseline. With `--baseline-window 4w`, also list substantive messages from the past 4 weeks.
- Steady-state: list messages with `datetime > last_message_timestamp`, filter out reaction-only / echo-style messages (the snapshot pattern handles this).

For each substantive new message: capture timestamp, author, summary (1-2 lines), and any embeds.

If Playwright reports "not logged in," prompt the user — see `<discord.setup_doc>` if set in config.

#### Spec PRs (per entry)

```
GH_TOKEN="$GH_PERSONAL_TOKEN" gh pr view <pr> --repo <repo> --json title,state,headRefOid,updatedAt,labels
GH_TOKEN="$GH_PERSONAL_TOKEN" gh api repos/<repo>/issues/<pr>/comments       # issue-level
GH_TOKEN="$GH_PERSONAL_TOKEN" gh api repos/<repo>/pulls/<pr>/comments        # inline review
GH_TOKEN="$GH_PERSONAL_TOKEN" gh api repos/<repo>/pulls/<pr>/commits
```

Two cursor types per PR — different cursor mechanics because the comment streams behave differently:

| Cursor | Field | Mechanics | Why |
|---|---|---|---|
| Issue-level comments | `last_comment_id` (scalar) | Max-id. New = any id > stamp. | Append-only stream; ids only grow. |
| Inline review comments | `last_inline_ids_file` (path) | Set-diff against snapshot file. | Resolutions/deletions remove ids. By-max-id is unreliable. |
| Commits | `last_head_sha` (scalar) | Diff via `git log <sha>..HEAD`-equivalent through the API. | |

**Snapshot files for inline cursors**: refresh writes a fresh JSON file at `<areas-dir>/<area>/spec-snapshots/comments/inline-comments-YYYY-MM-DD.json` (one line per comment) on each run, then suggests updating `last_inline_ids_file` to that path. Set-diff is current-vs-snapshot ids: emit added / removed / unchanged counts.

**Cold-start**:
- `last_head_sha: null` → record current `headRefOid` as baseline.
- `last_comment_id: null` → if PR has zero issue comments, suggest `0`; otherwise record max id.
- `last_inline_ids_file: null` → write today's snapshot file, suggest its path.

**Steady-state**:
- New issue comments: any with id > `last_comment_id`. Summarize substantive ones (1-2 lines each, skip "+1" / timeline events).
- Inline comments: load `last_inline_ids_file`, fetch current set, compute diff. Report counts. Flag any *removed* ids as worth a sanity-check (resolved threads vs deletions).
- New commits: any since `last_head_sha`. List with date + first line of message. **Flag commits whose message suggests wire-shape changes** (heuristics: messages mentioning rename, prefix, encoding, schema, "drop X", "align on X") for cross-check follow-up.

#### gapt

Only runs if `gapt.enabled` in config. Otherwise skip with a one-line note.

Run filtered triage queries:

```
gapt show <gapt.project_id> <source>:<pr>     # per spec_prs[] entry
gapt status <gapt.project_id> -o yaml         # for the broader unclassified count
```

For each `spec_prs[]` entry that's classified:
- Note bucket + rationale + classified_at
- **Stale-rationale heuristic**: if classified as `real-gap` and any commit in declared code-side `paths` post-dates `classified_at`, flag the rationale for review.

For each `spec_prs[]` entry that's unclassified: surface as "needs triage" finding.

If `gapt.last_status_at` is non-null, also call out any newly-unclassified items in declared sources since that timestamp (deltas, not the full unclassified list).

#### Code repo

Use the synced checkout from Step 2.

Cold-start (code-side `.commit` cursor null):
- Don't diff. Record HEAD as baseline.
- Per-topic inventory: for each declared code-side `paths[]` entry, list current files, line counts, and (if recent) `git log --oneline --since=<area-creation-date> -- <path>`.

Steady-state:
- Per-topic diff: `git -C <checkout> diff <last_commit>..HEAD -- <paths>` grouped by `topic` field
- Per-topic commit log: `git log <last_commit>..HEAD --oneline -- <paths>`
- Summarize: file count, LoC delta, key commits

#### References

For each `references[]` entry:
- Auto-fetchable types (`umbrella-issue`, `audit-issue`, `poc-issue` with `<repo>#N`): check issue state via `gh`, surface if state changed since `last_manually_checked`.
- Manual types (`google-doc`, `ietf-draft`, `external-spec`, `meeting-notes`, `discussion`): if `last_manually_checked` null → "never reviewed" reminder. If older than 30 days → "stale, recheck" reminder. Otherwise skip. Also surface schema warning if `url:` is null (per Step 1).
- Local types (`gap-table`, `spec-snapshot`, `pr-comments`, `channel-snapshot`, `spec-notes`): check on-disk last-modified vs `last_manually_checked`. **Auto-stamp suggestion**: if `last_manually_checked < file_mtime`, refresh suggests bumping the cursor to today's date in the suggested-cursor table — the user can accept or reject.

After processing declared references, **scan the area folder for undeclared markdown files** (`*.md`, `*-notes.md`, `*-analysis.md`) that aren't listed in `references[]`. Surface as a stale-state finding in Step 4: "area folder contains files not declared in manifest: <list>." Don't auto-add — the user decides whether they belong in the manifest.

### 4. Stale-state detection (derived findings)

This is the highest-value section. Best-effort heuristics, not exhaustive:

- **README.md status assertions vs code state.** Grep `<areas-dir>/<area-name>/README.md` for phrases like `NOT shipped`, `not yet`, `pending`, `TBD`, `Phase \d`, `not implemented`. For each match, surface verbatim with "verify against current code state?" flag. Don't try to auto-resolve — just put the human's eyes on potentially-stale claims.
- **gapt rationale staleness.** Re-state the heuristic from Step 3 (real-gap + recent commits in paths post-dating classification).
- **Linked issue state changes.** If gapt links to `<repo>#N` and that issue's state/labels changed since `gapt.last_status_at`, flag it.
- **PR commits suggesting wire-shape changes.** From Step 3's spec PR pull — list these in this section under "cross-check needed."
- **Status-vs-activity mismatch.** Re-state Step 1's heuristic (`parked`/`dormant` + ≥5 commits in last 7d in declared paths).
- **Schema warnings from Step 1** — list here too so they're visible alongside other findings.

### 5. Synthesize the refresh document

Write `<areas-dir>/<area-name>/refresh-YYYY-MM-DD.md` with this structure:

```
# Refresh — <area-name> — YYYY-MM-DD

## Method
Sources hit live: ...
Sources read from on-disk snapshot: ...
Sources skipped: ...

## Suggested cursor stamps
| Field | Suggested value | Note |
|---|---|---|
...

## What's new since last review
### Discord — <channel>
### Spec PRs
### gapt
### <code.name>

## Stale state in our own docs (derived findings)
1. ...
2. ...

## Manual-attention reminders
- ...

## Suggested next moves
1. ...
```

### Tone notes

- Be specific. "Casey 4/27 substantive post on SEP-2631 scope" beats "channel had activity."
- Cite line numbers, commit SHAs, comment ids — these are the audit trail.
- Don't quote WG members verbatim in this output. Refresh docs are committed, so they count as "committed text."
- "Suggested next moves" should reference the area's existing decision tree (e.g., the "When you come back" section in the area README) rather than inventing fresh guidance, when one exists.

## Status field semantics

The `status:` field in `area.yaml` shapes what refresh emphasizes:

- `active` — full pull on every source. All sections populated.
- `parked` — full pull, but emphasize the "external trigger arrived?" question. Surface any new spec PR activity or Discord posts prominently.
- `dormant` — light pull. If Discord/PR activity is genuinely zero, refresh can be a one-line "still dormant" emit. If activity resumed, escalate to full treatment and flag "channel waking up."

## Cursor update (separate verb)

After the user reads the refresh and is happy with the suggested cursors, they apply them by hand-editing `area.yaml` (or a future `/area-refresh <name> --commit-cursors` verb that auto-applies the most recent refresh's table — not in v1).

## Gotchas

- **Never auto-modify `area.yaml`.** Refresh is read-only with respect to the manifest. Cursor stamps go in the output doc; user applies them by hand-editing.
- **Repo validation is the only hard-stop.** Everything else (channel_id null, status mismatch, gapt config gap, working-tree dirty) is a warning. Refresh proceeds and produces a doc, just with the warned section empty/flagged.
- **Working-tree dirty state is visible.** Don't silently take the inventory at HEAD; show the user what's modified.
- **Always re-snapshot Discord.** Same-day on-disk snapshots are not a substitute for a fresh pull.
- **`gh` calls require `GH_TOKEN="$GH_PERSONAL_TOKEN"`** per CLAUDE.md gotcha.
- **Don't attempt the cross-check between PR wire-shape changes and code paths.** That's a separate verb. Refresh flags candidates; doesn't run the diff.
- **Refresh is per-area, not all-areas.** Running over four areas in one shot would produce a wall of output. If user wants a multi-area sweep, they invoke per-area.
- **No fallback to hardcoded paths.** If config resolution fails, refuse to run.
