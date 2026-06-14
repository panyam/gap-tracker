Refresh an area's non-Discord sources: spec PR activity (via gapt + gh), code-repo activity (git log), external doc reviews (Google Docs, IETF drafts). Synthesize updates to `CURRENT_GAPS.md` following the same conventions as `/channel-refresh`. Counterpart skill — use both for areas that have both Discord chatter and PR/doc activity; use only this one for PR-primary areas with no channel.

## Configuration

First, resolve the tracking root and load `<tracking-root>/.discord-tracking.yaml`. See `~/.claude/discord-tracking-playbook.md` for the resolver and config schema. Throughout this skill, `<areas-dir>`, `<code.name>`, `<code.default_worktree>`, `<code.repo>`, `<gapt.project_id>` refer to values from that config.

If the resolver fails, bail with the playbook's error message. Do NOT fall back to hardcoded paths.

## Arguments

`$ARGUMENTS` — `<area-name-or-url> [<github-url-for-bootstrap>] [--window <duration>]`

Three invocation forms:

1. **Subsequent by name** — `/proposal-refresh <area-name>`. Reads `<areas-dir>/<name>/area.yaml` and runs the normal flow.
2. **Subsequent by PR URL** — `/proposal-refresh https://github.com/<repo>/pull/<n>`. The skill searches all `<areas-dir>/*/area.yaml` files for a `spec_prs[]` entry matching `<repo>` + `<n>`. If exactly one match: use that area. If multiple: list them and ask which. If none: fail with "no area tracks this PR yet — to start: `/proposal-refresh <name> <url>`".
3. **First time (bootstrap)** — `/proposal-refresh <area-name> <github-url>`. The first positional is the new area name; the second is the SEP PR URL. The skill creates `<areas-dir>/<name>/` with a stub `area.yaml` (PR pre-filled in `spec_prs[]`) and a `README.md` skeleton, then runs the normal flow.

Other args:
- `--window <duration>` (optional, default `1d`) — same semantics as `/channel-refresh`. Drives the "Activity in window" section for PR comments, commits, and git log. Accepts shorthand (`1d`, `3d`, `1w`) or natural language (`"since monday"`, `"yesterday"`, `"last 3 days"`).

## Output

`<areas-dir>/<area-name>/CURRENT_GAPS.md` — same file `/channel-refresh` writes to. The two skills coexist via stable gap IDs and the don't-re-propose rule.

The skill does NOT:
- Touch Discord (that's `/channel-refresh`'s job).
- File GitHub issues. Filing happens separately (manual `gh` or `/gap-update`); once an issue exists it carries the work into a fresh session, no paste-prompt needed.
- Modify the area's `README.md`. Stale findings get surfaced in `CURRENT_GAPS.md`.
- Promote gaps between Discussing/Finalized/Ticketed/Closed automatically.

## Steps

### 1. Validate inputs (fail fast, lookup-by-url, or bootstrap)

Resolve the first positional argument:

**A. URL form** (matches `https://github.com/<owner>/<repo>/pull/<n>`):
- Parse out `<owner>/<repo>` and `<n>`.
- Search every `<areas-dir>/*/area.yaml` for a `spec_prs[]` entry where `repo == "<owner>/<repo>"` (or `<owner>/<repo>` after canonical-name redirect normalization) and `pr == <n>`.
- If exactly one match → use that area, proceed.
- If multiple matches → list them and ask the user which one.
- If no match AND no second positional → bail with:
  ```
  error: no area tracks <owner>/<repo>#<n>

  to start tracking: /proposal-refresh <area-name> <github-url>
  ```

**B. Name form** (non-URL first arg):
- Read `<areas-dir>/<name>/area.yaml`. If found, proceed.
- If missing AND a second positional URL was passed → **bootstrap** (see below).
- If missing AND no second positional → bail with:
  ```
  error: no area manifest found at <areas-dir>/<area-name>/area.yaml

  did you mean one of:
    <list of 1-3 closest matches from `ls -d <areas-dir>/*/`, prioritized by:
      1. exact substring match,
      2. shared prefix of >= 3 chars,
      3. small edit distance>

  available areas:
    <full alphabetized list of folder names under <areas-dir>/>

  to bootstrap a new area: /proposal-refresh <name> <github-url>
  ```
  Show "did you mean" only when there are plausible matches; always show the full list as fallback. If `<channels-dir>/<name>.yaml` exists, also suggest `/channel-refresh <name>` as a possibly-correct alternative skill.

**Bootstrap** (Name form, second positional is a github PR URL):
- Parse `<owner>/<repo>` and `<n>` from the URL.
- `GH_TOKEN="$GH_PERSONAL_TOKEN" gh pr view <n> --repo <owner>/<repo> --json title,state,headRefOid` to fetch title/state.
- Create `<areas-dir>/<name>/` directory.
- Write `<areas-dir>/<name>/area.yaml`:
  ```yaml
  name: <name>
  status: active
  spec_prs:
    - repo: <owner>/<repo>
      pr: <n>
      label: SEP-<n>            # human can refine to SEP-<n>-<short-name>
  code:
    paths: []
  references: []
  gapt:
    sources: []                  # populate if gapt has this repo configured
  ```
- Write `<areas-dir>/<name>/README.md` skeleton — headings only:
  ```markdown
  # Area: <name>

  Tracking <owner>/<repo>#<n>: <PR title from gh>

  ## Summary
  <!-- one paragraph: what is this, who's authoring it, why we care -->

  ## Status
  <!-- current state, key dates -->

  ## Strategy
  <!-- how we position our contribution -->

  ## Key People
  <!-- table: name | affiliation | role -->

  ## Next steps
  <!-- what we plan to do next -->

  ## When you come back
  <!-- decision tree for returning sessions -->
  ```
- Proceed with the normal flow against the new manifest.

After resolution (any form), extract from `area.yaml`:
- `spec_prs[]` — `{repo, pr, label}` entries
- code-side `paths[]` and topics (under `code:` or legacy `mcpkit:` block)
- `references[]` — external docs (google-doc, ietf-draft, external-spec, meeting-notes, discussion) plus local refs (gap-table, spec-snapshot, spec-notes, pr-comments)
- `gapt.sources[]` — for cross-checking gapt has the source configured

### 2. Resolve window

Same as `/channel-refresh`:
- `WINDOW_ISO`: ISO 8601 timestamp computed from `--window`, for tools that need explicit timestamps
- `WINDOW_GIT`: original expression passed to `git log --since="..."`
- `WINDOW_GH`: ISO timestamp for `gh api` filtering (where supported)

### 3. Gather deterministic context

In parallel where possible:

- **Spec PR window activity.** For each `spec_prs[]` entry:
  - `GH_TOKEN="$GH_PERSONAL_TOKEN" gh api repos/<repo>/issues/<pr>/comments --jq '.[] | select(.created_at > "<WINDOW_ISO>")'`
  - `GH_TOKEN="$GH_PERSONAL_TOKEN" gh api repos/<repo>/pulls/<pr>/commits --jq '.[] | select(.commit.author.date > "<WINDOW_ISO>")'`
  - `GH_TOKEN="$GH_PERSONAL_TOKEN" gh pr view <pr> --repo <repo> --json state,title,headRefOid,updatedAt`
- **gapt classification** (only if `gapt.enabled` in config). For each `spec_prs[]` entry: `gapt show <gapt.project_id> <source>:<pr>`. If gapt has no source for the repo, surface as a finding ("gapt config gap") under Notes for next refresh. Don't fail.
- **gapt status** (only if `gapt.enabled`). `gapt status <gapt.project_id> -o yaml | grep -A2 <relevant-source>` for each declared source.
- **Code-repo git activity (window).** `git -C <code.default_worktree> log --oneline --since="<WINDOW_GIT>" -- <code-paths>`.
- **Code-repo git activity (cumulative).** `git -C <code.default_worktree> log --oneline --since="6 weeks ago" -- <code-paths>` for the cumulative code-position synthesis.
- **External doc review status.** For each `references[]` entry of type `google-doc`, `ietf-draft`, `external-spec`, `meeting-notes`, `discussion`: note `last_manually_checked`. If null → "never reviewed" reminder. If older than 30 days → "stale, recheck" reminder. Auth-gated content (Google Docs, Notion) is human-reviewable only; surface the reminder, don't try to fetch.
- **Local reference freshness.** For each `references[]` entry pointing at a local file (`gap-table`, `spec-snapshot`, `pr-comments`, `spec-notes`): compare file mtime vs `last_manually_checked`. Suggest a stamp bump if file is newer.
- **Area context.** Read `<areas-dir>/<name>/README.md` and the most recent `refresh-*.md` (sorted by filename date).
- **Existing CURRENT_GAPS.md** if present:
  - **Preserve `Ticketed` and `Closed` sections verbatim.**
  - **Walk existing Pending checklists.** For each gap, scan the `**Pending**` list. Items the skill can verify as done (commit landed, comment posted, file mtime moved past the cited date, issue closed) get flipped from `[ ]` to `[x]` with a verifier reference (commit SHA, comment ID, issue URL). Items that became invalid get `[~]` with a one-line reason. New items get appended.
  - **Don't re-propose what's already tracked.** Scan all sections for subject overlap. Skip candidate gaps that look substantially the same as existing entries. Surface ambiguity under "Notes for next refresh" rather than re-proposing.
  - **Stable IDs.** Read existing gap IDs. New gaps get `G<max-existing + 1>`. Existing gaps keep their IDs even when the user has moved them between sections.

### 4. Synthesize CURRENT_GAPS.md

Same format and conventions as `/channel-refresh` — see that skill's Step 4 for the full per-gap shape (gap-as-delta frame, `Where this stands` + `Spec/channel side` + `<code.name> side` + `Closes when` + `Pending` checklist, no paste-prompt blocks).

PR-derived differences from the channel-refresh template:

- The header line reads `Last refreshed: YYYY-MM-DD by /proposal-refresh [--window <w>]` (not `/channel-refresh`).
- The **Spec/channel side** field cites PR comment IDs + author + date, not Discord message IDs. For doc-derived gaps (something raised in a Google Doc or IETF draft), cite the doc URL + section + `last_manually_checked` date from `area.yaml`.
- The "Activity in window" section is sourced from PR + code repo + doc-review reminders, not Discord:

  ```markdown
  ## Activity in window

  - Spec PR <repo>#<pr>: <N new comments, M new commits since WINDOW_ISO>
    - <one-line summary of each substantive comment with author + date>
    - <one-line summary of each commit>
  - <code.name>: <commits in declared paths within window, or "no commits">
  - gapt classification: <changes within window, or "no classification changes">
  - External docs flagged for review: <list with last_manually_checked dates>
  ```

- For doc-derived gaps, **Pending** items can include reminders like `[ ] re-read <doc URL>; bump references[<label>].last_manually_checked in area.yaml when done`. The human applies the YAML bump by hand; the skill never modifies `area.yaml` automatically.

### 5. Write the file

Write to `<areas-dir>/<area-name>/CURRENT_GAPS.md`. Preserve `Ticketed` and `Closed` sections from the prior file. If `/channel-refresh` has also written to this file, treat its entries as existing gaps — don't overwrite Discord-derived sections, augment.

### 6. Print a summary

To stdout (not to the file):

```
proposal-refresh: <area-name>
  PR activity:    <N comments, M commits across K PRs>
  <code.name>:    <X commits in declared paths>
  docs flagged:   <Y reviews needed>
  gaps:           <D discussing, F finalized, T ticketed, C closed>
  README stale:   <S findings>
  written:        <areas-dir>/<area-name>/CURRENT_GAPS.md
```

## Pairing with `/channel-refresh`

Same area can be refreshed by both skills. They converge on `CURRENT_GAPS.md`:

```
/channel-refresh <name>     # Discord side
/proposal-refresh <name>    # PR + doc + code side
```

Order doesn't matter — stable IDs and don't-re-propose handle merging. For PR-primary areas, `/channel-refresh` is skipped entirely.

## Parallel sweep pattern

When refreshing multiple areas at once: this skill is parallel-safe (no Playwright profile contention, no DB writes — only reads from gapt + git + gh). Fan out N subagents, each running `/proposal-refresh <name>`. The gh api may rate-limit; serialize if you hit 5000 req/h.

## Style notes

- Cite PR comments by comment ID + author + date when surfacing in synthesis.
- Cite gapt rationale + `classified_at` when leaning on classification.
- For PR commit subjects that suggest wire-shape changes (mentions of "rename", "prefix", "encoding", "drop", "align"): flag in Notes for next refresh as worth a cross-check against `<code.name>`.
- No em dashes, no semicolons in committed prose. SQL/JSON exempt.
- For external doc reminders: include the URL inline so the user can click to re-read.

## Gotchas

- **`gh` calls require `GH_TOKEN="$GH_PERSONAL_TOKEN"`.** Personal repos aren't accessible via the EMU account.
- **gapt source mismatch.** `area.yaml`'s `spec_prs[].repo` may not be in gapt's `sources[]`. Surface as a finding, don't fail.
- **Auth-gated docs can't be fetched.** Google Docs, Notion, etc. — surface as manual-review reminders, never attempt to fetch.
- **Cursor state for PRs lives in gapt.** The Discord-sync DB has only Discord cursors. If a PR's comment-since-state is needed and gapt doesn't have it cached, use `gh api` with the gap's `last_manually_checked` as the lower bound, or fall back to the window.
- **CURRENT_GAPS.md is the only artifact this skill writes.** Don't touch README, don't auto-update `area.yaml`'s `last_manually_checked` stamps (the human does that during gap evaluation), don't create issues.
- **PR-only areas don't need a channel.yaml.** `area.yaml` may have `channels: []` (empty) or omit the field entirely. The skill should handle missing channel info gracefully.
- **No fallback to hardcoded paths.** If config resolution fails, refuse to run.
