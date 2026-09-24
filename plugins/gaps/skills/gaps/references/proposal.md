# /gaps proposal

Refresh a channel's non-Discord sources: spec PR activity (via gapt and gh),
code-repo activity (git log), and external doc reviews (Google Docs, IETF
drafts). Synthesize updates into `CURRENT_GAPS.md`.

Counterpart to `/gaps channel`, and keyed the same way: by a tracked Discord
channel. Everything this mode tracks (`spec_prs`, code paths, `references`,
`ext_repos`, `mcpconformance`, `gapt`) comes from that channel's manifest,
`<channels-dir>/<channel>.yaml`. Use both modes for a channel with Discord
chatter and PR or doc activity, and only this one when the channel's Discord
side is quiet.

Shared conventions live in `SKILL.md` and this mode depends on them. Read it first
for the **config resolver**, the **`--window` lens** and its three resolved forms,
the **gap frame and lifecycle**, the **per-gap shape rules**, and the **shared
style notes and gotchas**. Then read `references/format.md` for the generation
template and the re-run responsibilities.

## Arguments

`<channel-or-pr-url> [<pr-url>] [--window <duration>]`

Three invocation forms:

1. **By channel**: `/gaps proposal <channel>`. Reads `<channels-dir>/<channel>.yaml` and runs the normal flow.
2. **By PR URL**: `/gaps proposal https://github.com/<repo>/pull/<n>`. Resolves to the channel whose manifest lists that PR in `spec_prs[]`. One match uses that channel. Multiple matches get listed for the user to pick. No match fails (see step 1).
3. **Attach a PR**: `/gaps proposal <channel> <pr-url>`. The channel must already be tracked. If its manifest does not list the PR yet, append it to `spec_prs[]`, then run the normal flow.

## Output

`<gaps-dir>/<channel>/CURRENT_GAPS.md`, the same file `/gaps channel` writes
to. The two coexist via stable gap IDs and the don't-re-propose rule.

This mode does NOT:
- Read Discord messages. That is `/gaps channel`'s job.
- File GitHub issues. Filing happens separately, via manual `gh` or `/gaps update`. Once an issue exists it carries the work into a fresh session, so no paste-prompt is needed.
- Modify the channel folder's `README.md`. Stale findings get surfaced in `CURRENT_GAPS.md`.
- Promote gaps between Discussing, Finalized, Ticketed and Closed automatically.
- Create a channel or its folder. `disctrack onboard` does that.

## Steps

### 1. Validate inputs (fail fast, lookup-by-url, or attach)

Resolve the first positional argument:

**A. PR URL form** (matches `https://github.com/<owner>/<repo>/pull/<n>`):
- Parse out `<owner>/<repo>` and `<n>`.
- Read every non-thread manifest under `<channels-dir>/` (`disctrack channels-dir` prints the path) and look for a `spec_prs[]` entry where `repo == "<owner>/<repo>"` (or `<owner>/<repo>` after canonical-name redirect normalization) and `pr == <n>`.
- If exactly one match, use that channel and proceed.
- If multiple matches, list them and ask the user which one.
- If no match, bail with:
  ```
  error: no channel tracks <owner>/<repo>#<n>

  add it to the channel that owns this work:
    /gaps proposal <channel> <pr-url>
  or add a spec_prs entry to <channels-dir>/<channel>.yaml by hand.
  if no tracked channel fits, onboard one first: disctrack onboard <name> <discord-url>
  ```

**B. Channel form** (non-URL first arg):
- Run `disctrack channel <channel>`. Exit 0 means the manifest exists. Read it directly.
- Exit 1 means it is missing, or it still carries a legacy `area_folder` or `proposal_folder`. Surface disctrack's error verbatim (it lists the available channels on a miss) and stop. Do not create a manifest.
- If the manifest is a thread (`kind: thread`, or a legacy `parent:` block), stop and name the parent: proposals are keyed by the parent channel.
- Run `disctrack dir <channel>` to confirm `<gaps-dir>/<channel>/` exists. If it doesn't, surface the error and stop.

**Attach** (channel form with a second positional PR URL):
- Parse `<owner>/<repo>` and `<n>` from the URL.
- If the manifest already lists it in `spec_prs[]`, proceed with the normal flow.
- Otherwise, if another channel's manifest already lists it, say which one and ask before adding it here too.
- `GH_TOKEN="$GH_PERSONAL_TOKEN" gh pr view <n> --repo <owner>/<repo> --json title,state,headRefOid` to fetch the title and state.
- Append to the manifest's `spec_prs:` list, editing the yaml in place and leaving every other line untouched:
  ```yaml
  spec_prs:
    - repo: <owner>/<repo>
      pr: <n>
      label: SEP-<n>            # human can refine to SEP-<n>-<short-name>
  ```
- Proceed with the normal flow.

After resolution (any form), extract from the channel manifest:
- `spec_prs[]` holds `{repo, pr, label}` entries
- code-side `paths[]` and topics (under `code:` or `mcpkit:` block)
- `references[]` holds external docs (google-doc, ietf-draft, external-spec, meeting-notes, discussion, external-pr) plus local refs (gap-table, spec-snapshot, spec-notes, pr-comments). Local `location:` paths resolve against `<gaps-dir>/<channel>/`.
- `ext_repos[]` holds other repos to watch for this channel
- `mcpconformance` describes conformance work for this channel, if any
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
- **External repo activity** (only if `ext_repos[]` is set). For each entry with a `pr`, the same three `gh` calls as a spec PR. For an entry without one, `GH_TOKEN="$GH_PERSONAL_TOKEN" gh api "repos/<repo>/pulls?state=all&sort=updated&direction=desc" --jq '.[] | select(.updated_at > "<WINDOW_ISO>")'` and keep what touches this channel's topic.
- **Channel context.** Read `<gaps-dir>/<channel>/README.md`.
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
- The **Spec/channel side** field cites PR comment IDs plus author plus date rather than Discord message IDs. For doc-derived gaps, meaning something raised in a Google Doc or IETF draft, cite the doc URL plus section plus the `last_manually_checked` date from the channel manifest.
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

- For doc-derived gaps, **Pending** items can include reminders like `[ ] re-read <doc URL>, then bump references[<label>].last_manually_checked in <channels-dir>/<channel>.yaml when done`. The human applies the YAML bump by hand, and this mode never modifies those stamps.

### 4. Write the file

Write to `<gaps-dir>/<channel>/CURRENT_GAPS.md`. Preserve `Ticketed` and `Closed` sections from the prior file. If `/gaps channel` has also written to this file, treat its entries as existing gaps, so augment them rather than overwriting Discord-derived sections.

### 5. Print a summary

To stdout (not to the file):

```
gaps proposal: <channel>
  PR activity:    <N comments, M commits across K PRs>
  <code.name>:    <X commits in declared paths>
  docs flagged:   <Y reviews needed>
  gaps:           <D discussing, F finalized, T ticketed, C closed>
  README stale:   <S findings>
  written:        <gaps-dir>/<channel>/CURRENT_GAPS.md
```

## Parallel sweep pattern

This mode is parallel-safe, with no Playwright profile contention and no DB
writes, only reads from gapt, git and gh. Fan out N subagents, each running
`/gaps proposal <channel>`. The gh api may rate-limit, so serialize if you hit
5000 req/h.

## Gotchas

- **Cursor state for PRs lives in gapt.** The Discord-sync DB has only Discord cursors. If a PR's comment-since state is needed and gapt has not cached it, use `gh api` with the gap's `last_manually_checked` as the lower bound, or fall back to the window.
- **CURRENT_GAPS.md is the only doc this mode writes.** The one manifest edit it makes is appending a `spec_prs[]` entry in the attach form. Don't update `last_manually_checked` stamps, which the human does during gap evaluation, and don't create issues.
- **Every proposal needs a channel.** A spec PR with no Discord home of its own goes on the channel that owns the topic. There is no standalone PR-only folder.
