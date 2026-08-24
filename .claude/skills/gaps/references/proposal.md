# /gaps proposal

Refresh an area's non-Discord sources: spec PR activity (via gapt and gh),
code-repo activity (git log), and external doc reviews (Google Docs, IETF
drafts). Synthesize updates into `CURRENT_GAPS.md`.

Counterpart to `/gaps channel`. Use both for areas that have both Discord
chatter and PR or doc activity, and only this one for PR-primary areas with no
channel.

Shared conventions live in `SKILL.md` and this mode depends on them. Read it first
for the **config resolver**, the **`--window` lens** and its three resolved forms,
the **gap frame and lifecycle**, the **per-gap shape rules**, and the **shared
style notes and gotchas**. Then read `references/format.md` for the generation
template and the re-run responsibilities.

## Arguments

`<area-name-or-url> [<github-url-for-bootstrap>] [--window <duration>]`

Three invocation forms:

1. **Subsequent by name**: `/gaps proposal <area-name>`. Reads `<areas-dir>/<name>/area.yaml` and runs the normal flow.
2. **Subsequent by PR URL**: `/gaps proposal https://github.com/<repo>/pull/<n>`. Searches all `<areas-dir>/*/area.yaml` for a `spec_prs[]` entry matching `<repo>` plus `<n>`. One match uses that area. Multiple matches get listed for the user to pick. No match fails with "no area tracks this PR yet, to start: `/gaps proposal <name> <url>`".
3. **First time (bootstrap)**: `/gaps proposal <area-name> <github-url>`. The first positional is the new area name, and the second is the SEP PR URL. Creates `<areas-dir>/<name>/` with a stub `area.yaml` (PR pre-filled in `spec_prs[]`) and a `README.md` skeleton, then runs the normal flow.

## Output

`<areas-dir>/<area-name>/CURRENT_GAPS.md`, the same file `/gaps channel` writes
to. The two coexist via stable gap IDs and the don't-re-propose rule.

This mode does NOT:
- Touch Discord. That is `/gaps channel`'s job.
- File GitHub issues. Filing happens separately, via manual `gh` or `/gaps update`. Once an issue exists it carries the work into a fresh session, so no paste-prompt is needed.
- Modify the area's `README.md`. Stale findings get surfaced in `CURRENT_GAPS.md`.
- Promote gaps between Discussing, Finalized, Ticketed and Closed automatically.

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

  to start tracking: /gaps proposal <area-name> <github-url>
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

  to bootstrap a new area: /gaps proposal <name> <github-url>
  ```
  Show "did you mean" only when there are plausible matches, and always show the full list as fallback. If `<channels-dir>/<name>.yaml` exists, also suggest `/gaps channel <name>` as a possibly-correct alternative skill.

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
- Write `<areas-dir>/<name>/README.md` skeleton, headings only:
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
- `spec_prs[]` holds `{repo, pr, label}` entries
- code-side `paths[]` and topics (under `code:` or legacy `mcpkit:` block)
- `references[]` holds external docs (google-doc, ietf-draft, external-spec, meeting-notes, discussion) plus local refs (gap-table, spec-snapshot, spec-notes, pr-comments)
- `gapt.sources[]` is for cross-checking gapt has the source configured

### 2. Gather deterministic context

Resolve `--window` into `WINDOW_ISO` / `WINDOW_GIT` / `WINDOW_GH` first (see `SKILL.md`), then gather, in parallel where possible:


- **Spec PR window activity.** For each `spec_prs[]` entry:
  - `GH_TOKEN="$GH_PERSONAL_TOKEN" gh api repos/<repo>/issues/<pr>/comments --jq '.[] | select(.created_at > "<WINDOW_ISO>")'`
  - `GH_TOKEN="$GH_PERSONAL_TOKEN" gh api repos/<repo>/pulls/<pr>/commits --jq '.[] | select(.commit.author.date > "<WINDOW_ISO>")'`
  - `GH_TOKEN="$GH_PERSONAL_TOKEN" gh pr view <pr> --repo <repo> --json state,title,headRefOid,updatedAt`
- **gapt classification** (only if `gapt.enabled` in config). For each `spec_prs[]` entry: `gapt show <gapt.project_id> <source>:<pr>`. If gapt has no source for the repo, surface as a finding ("gapt config gap") under Notes for next refresh. Don't fail.
- **gapt status** (only if `gapt.enabled`). `gapt status <gapt.project_id> -o yaml | grep -A2 <relevant-source>` for each declared source.
- **Code-repo git activity (window).** `git -C <code.default_worktree> log --oneline --since="<WINDOW_GIT>" -- <code-paths>`.
- **Code-repo git activity (cumulative).** `git -C <code.default_worktree> log --oneline --since="6 weeks ago" -- <code-paths>` for the cumulative code-position synthesis.
- **External doc review status.** For each `references[]` entry of type `google-doc`, `ietf-draft`, `external-spec`, `meeting-notes`, `discussion`: note `last_manually_checked`. If null → "never reviewed" reminder. If older than 30 days → "stale, recheck" reminder. Auth-gated content (Google Docs, Notion) is human-reviewable only, so surface the reminder and don't try to fetch.
- **Local reference freshness.** For each `references[]` entry pointing at a local file (`gap-table`, `spec-snapshot`, `pr-comments`, `spec-notes`): compare file mtime vs `last_manually_checked`. Suggest a stamp bump if file is newer.
- **Area context.** Read `<areas-dir>/<name>/README.md` and the most recent `refresh-*.md` (sorted by filename date).
- **Existing CURRENT_GAPS.md** if present:
  - **Preserve `Ticketed` and `Closed` sections verbatim.**
  - **Walk existing Pending checklists.** For each gap, scan the `**Pending**` list. Items the skill can verify as done (commit landed, comment posted, file mtime moved past the cited date, issue closed) get flipped from `[ ]` to `[x]` with a verifier reference (commit SHA, comment ID, issue URL). Items that became invalid get `[~]` with a one-line reason. New items get appended.
  - **Don't re-propose what's already tracked.** Scan all sections for subject overlap. Skip candidate gaps that look substantially the same as existing entries. Surface ambiguity under "Notes for next refresh" rather than re-proposing.
  - **Stable IDs.** Read existing gap IDs. New gaps get `G<max-existing + 1>`. Existing gaps keep their IDs even when the user has moved them between sections.
- **Existing CURRENT_GAPS.md** if present. Apply the four re-run responsibilities from `references/format.md`.

### 3. Synthesize CURRENT_GAPS.md

Follow the gap frame and per-gap shape rules in `SKILL.md`, and the generation
template in `references/format.md`. PR-derived differences from that template:

- The header line reads `Last refreshed: YYYY-MM-DD by /gaps proposal [--window <w>]`.
- The **Spec/channel side** field cites PR comment IDs plus author plus date rather than Discord message IDs. For doc-derived gaps, meaning something raised in a Google Doc or IETF draft, cite the doc URL plus section plus the `last_manually_checked` date from `area.yaml`.
- The "Activity in window" section is sourced from PR, code repo and doc-review reminders rather than Discord:

  ```markdown
  ## Activity in window

  - Spec PR <repo>#<pr>: <N new comments, M new commits since WINDOW_ISO>
    - <one-line summary of each substantive comment with author + date>
    - <one-line summary of each commit>
  - <code.name>: <commits in declared paths within window, or "no commits">
  - gapt classification: <changes within window, or "no classification changes">
  - External docs flagged for review: <list with last_manually_checked dates>
  ```

- For doc-derived gaps, **Pending** items can include reminders like `[ ] re-read <doc URL>, then bump references[<label>].last_manually_checked in area.yaml when done`. The human applies the YAML bump by hand, and this mode never modifies `area.yaml` automatically.

### 4. Write the file

Write to `<areas-dir>/<area-name>/CURRENT_GAPS.md`. Preserve `Ticketed` and `Closed` sections from the prior file. If `/gaps channel` has also written to this file, treat its entries as existing gaps, so augment them rather than overwriting Discord-derived sections.

### 5. Print a summary

To stdout (not to the file):

```
gaps proposal: <area-name>
  PR activity:    <N comments, M commits across K PRs>
  <code.name>:    <X commits in declared paths>
  docs flagged:   <Y reviews needed>
  gaps:           <D discussing, F finalized, T ticketed, C closed>
  README stale:   <S findings>
  written:        <areas-dir>/<area-name>/CURRENT_GAPS.md
```

## Parallel sweep pattern

This mode is parallel-safe, with no Playwright profile contention and no DB
writes, only reads from gapt, git and gh. Fan out N subagents, each running
`/gaps proposal <name>`. The gh api may rate-limit, so serialize if you hit
5000 req/h.

## Gotchas

- **Cursor state for PRs lives in gapt.** The Discord-sync DB has only Discord cursors. If a PR's comment-since state is needed and gapt has not cached it, use `gh api` with the gap's `last_manually_checked` as the lower bound, or fall back to the window.
- **CURRENT_GAPS.md is the only artifact this mode writes.** Don't auto-update `area.yaml`'s `last_manually_checked` stamps, which the human does during gap evaluation, and don't create issues.
- **PR-only areas don't need a channel.yaml.** An `area.yaml` may have `channels: []` or omit the field entirely. Handle missing channel info gracefully.
