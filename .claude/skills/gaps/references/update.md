# /gaps update

Reconcile an area's `CURRENT_GAPS.md` against the code repo and the open issue
list. Proposes a per-gap action (close, fold, file a new ticket, update an
existing ticket, hold), surfaces backlink-firing references, takes per-action
confirmation, then executes the approved actions.

**This is the only mode that mutates GitHub.** It writes both `CURRENT_GAPS.md`
and `gh` state, where `/gaps channel` and `/gaps proposal` only write the doc.
Pair it with either of them, and note that it takes its input at face value, so
refresh first if the file is stale.

Shared conventions live in `SKILL.md` and this mode depends on them. Read it first
for the **config resolver**, the **gap frame and lifecycle**, the **per-gap shape
rules**, and the **shared style notes and gotchas**. This mode edits an existing
`CURRENT_GAPS.md` rather than regenerating one, so it does not need
`references/format.md`.

## Arguments

`<area-folder-name> [--worktree <path>] [--repo <org/repo>] [--label <label>] [--dry-run]`

- `<area-folder-name>` (required) is the folder name under `<areas-dir>/`, used to locate `CURRENT_GAPS.md` and `area.yaml`. If `area.yaml` has a `worktree:` field, that is the default code-repo checkout, otherwise the default is `<code.default_worktree>` from config.
- `--worktree <path>` (optional) overrides the worktree path. Use it when the gap-side analysis should run against a feature-branch worktree rather than main.
- `--repo <org/repo>` (optional) is the GitHub repo to file or edit issues against. Defaults to `area.yaml`'s `repo:` if set, else `<code.repo>` from config. Different areas may target different downstream repos.
- `--label <label>` (optional) filters the existing-issue query to a single label (e.g. `prod-events`). Defaults to whatever `area.yaml` declares in a `default_label:` field, otherwise no filter.
- `--dry-run` (optional) produces the plan file and stops, skipping the confirmation and execution phase. Useful for review-only passes.

## Output

- **Always**: `<areas-dir>/<name>/gap-update-plan.md`, a DO-NOT-COMMIT working file overwritten each run and gitignored via the `<areas-dir>/*/gap-update-plan.md` pattern. This is the proposal document the user reads.
- **On execution**: edits to `<areas-dir>/<name>/CURRENT_GAPS.md`. New issues filed via `gh issue create`, existing issues edited via `gh issue edit`, closed issues closed via `gh issue close`.

This mode does NOT:
- Modify the area's `README.md`. Stale findings about README stay surfaced in `CURRENT_GAPS.md` for the human.
- Run `/gaps channel` or `/gaps proposal`. Inputs are taken at face value.
- Re-classify gaps already in the `Closed` section. Those are history.
- Auto-promote gaps between `Discussing`, `Finalized` and `Ticketed` without an explicit file-ticket or update-ticket action proposed and approved. The lifecycle transition is the *result* of executing an approved action, never a separate move.

## Steps

### 1. Validate inputs

Resolve `<area-folder-name>`:

- Look for `<areas-dir>/<name>/CURRENT_GAPS.md`. If missing, fail fast with a "did you mean" list of folders that have `CURRENT_GAPS.md`. Suggest running `/gaps channel <name>` first.
- Look for `<areas-dir>/<name>/area.yaml`. If present, read optional `worktree:`, `default_label:`, `repo:` fields. Missing area.yaml is fine, and defaults apply.

Resolve the worktree:

- `--worktree` arg if passed, else `area.yaml`'s `worktree:` field if set, else `<code.default_worktree>` from config.
- Verify the path exists and is a git checkout. Fail fast if not.

Resolve the repo:

- `--repo` arg if passed, else `area.yaml`'s `repo:` field if set, else `<code.repo>` from config.

Resolve the label filter:

- `--label` arg if passed, else `area.yaml`'s `default_label:` field if set, else no filter.

Resolve plan file path: `<areas-dir>/<name>/gap-update-plan.md`.

### 2. Gather state

Run in parallel where possible:

- **Read `CURRENT_GAPS.md`.** Parse out every gap by section: Discussing, Finalized, Ticketed, Closed. Extract per-gap: ID, title, "`<code.name>` side" file:line citations, Pending checklist items, any `→ <repo>#<N>` ticket references.
- **Query existing issues.** `GH_TOKEN="$GH_PERSONAL_TOKEN" gh issue list --repo <repo> --label <label> --state all --limit 200 --json number,state,title,labels,updatedAt,body`. Cache the body for issues currently referenced from CURRENT_GAPS.md (via Ticketed-section `→ #N` markers).
- **Worktree HEAD + branch.** `git -C <worktree> log -1 --format='%H %s' && git -C <worktree> branch --show-current`. Surface in the plan file's header.
- **gapt classification (optional)**. If `gapt.enabled` in config AND `area.yaml` declares sources in a `gapt.sources:` list, run `gapt status <gapt.project_id> -o yaml` and pull the relevant rows. Skip if gapt config is incomplete.

### 3. Per-gap classification

For every gap NOT in the `Closed` section, propose exactly one of five actions. Verify each against worktree code plus ticket state before proposing, and don't propose blind off CURRENT_GAPS.md alone.

The five actions:

#### HOLD

No action. The gap exists, direction isn't firm enough to file a tracker, and worktree code can't close it. Output: just the gap ID + a one-line rationale ("waiting on spec text from <author>'s <date> design").

#### CLOSE-as-aligned

Worktree code already satisfies the gap's "Closes when" criterion. Run grep / read against the worktree to verify the file:line citations in the gap. Surface the verification evidence (commit SHA, file:line, snippet). Propose CURRENT_GAPS.md edit: move from Discussing to Closed with a "verified at commit <SHA>" line.

#### CLOSE-as-superseded

Another gap absorbs this one's delta. Name the target gap explicitly. Propose Pending-item absorption (move open `[ ]` items from this gap into the target gap's Pending list). Propose CURRENT_GAPS.md edit: move from Discussing to Closed with "superseded by G<n>" line.

#### FILE-new-issue

Direction firm enough to file an issue. Two sub-shapes:

- **Actionable**, meaning a title without the `[tracker]` prefix. Body has "What this changes" + "Acceptance" sections. Direction is normative (spec text landed, or a clearly-firm WG decision).
- **Tracker**, meaning a title prefixed with `[tracker]`. Body has "Spec direction" + "Current state" + "When spec text lands" sections. Direction signalled but spec text not landed.

In both cases, draft title + body + labels, AND list backlink-firing references explicitly. Backlink rules per `~/.claude/CLAUDE.md`:

- Default to plain text or backtick-wrapped form for upstream `org/repo#N` cross-refs (suppresses backlink on the upstream item).
- Same-repo `#N` refs fire backlinks on own-repo items, which is expected and fine.
- URLs (e.g. `https://github.com/foo/bar`) don't fire cross-reference backlinks. Only `org/repo#N` syntax does.

#### UPDATE-existing-issue

An open issue already covers part of this gap, but its body is stale or its scope has shifted. Fetch the current body via `gh issue view --json body`, show the proposed diff in the plan, list backlink-firing refs in the new body, ask per-item confirmation.

The body REWRITE pattern (full overwrite) is fine when explicitly authorized. The fetch-before-write rule from `CLAUDE.md` applies: re-fetch right before the edit to catch any concurrent change.

For closed gaps that still have an OPEN issue: propose `gh issue close <N>` with a closing comment that references the closing rationale from CURRENT_GAPS.md.

### 4. Write the plan file

`<areas-dir>/<name>/gap-update-plan.md`, a full overwrite with no merge.

Plan-file header MUST start with this exact block (one trailing blank line):

```markdown
> [!WARNING]
> DO NOT COMMIT. This file is ephemeral working state produced by `/gaps update`.
> It is overwritten on every run and gitignored. Do not check it in.

# Gap-update plan: <area-name>

Generated <YYYY-MM-DD HH:MM TZ> by `/gaps update`
Worktree: `<path>` @ `<HEAD-sha>` on branch `<branch>`
Repo: `<org/repo>`
Label filter: `<label>` (or "none")

## Summary

- HOLD: <N> gaps
- CLOSE-as-aligned: <N> gaps
- CLOSE-as-superseded: <N> gaps
- FILE-new-issue: <N> actionable, <M> trackers
- UPDATE-existing-issue: <N> bodies, <K> closures

## Per-gap recommendations

(grouped by action, then by gap ID)
```

Per-gap recommendation block shape:

```markdown
### G<n>: <title> → <ACTION>

**Rationale**: <one or two sentences>

**Verified against**: <worktree file:line citations + commit SHA, OR ticket evidence>

[For FILE-new-issue:]
**Proposed title**: <title>
**Proposed labels**: <labels>
**Backlink-firing references**:
- <list of cross-refs that will fire on linked items, with assessment>
**Proposed body**:
```
<full body draft>
```

[For UPDATE-existing-issue:]
**Target issue**: <repo>#<N> (current title: "<title>")
**Proposed retitle**: <new title> (or "unchanged")
**Backlink-firing references in new body**:
- <list>
**Proposed body**:
```
<full body draft>
```

[For CLOSE-as-aligned or CLOSE-as-superseded:]
**CURRENT_GAPS.md edit**: <one-line description of the move>
**Pending items to carry forward**: <list, with target gap if superseded>

[For HOLD:]
(no further fields)
```

### 5. Confirmation phase

For every proposed action that mutates GitHub state (FILE-new-issue, UPDATE-existing-issue, close-existing-issue), use `AskUserQuestion` to confirm per item.

Per the user's global backlink rule: brand-new GitHub issue creation REQUIRES per-issue confirmation that surfaces the planned title, body, and backlink-firing refs. Don't batch-confirm across multiple new issues. One question per issue.

For CLOSE-as-aligned and CLOSE-as-superseded actions (which mutate CURRENT_GAPS.md only): batch-confirm at the top of the run with a single "approve all close/fold actions?" question, or per-item if there are more than 5. The CURRENT_GAPS.md edits are reversible via git, so the lower-confirmation threshold is fine.

HOLD actions need no confirmation.

`--dry-run` skips the entire confirmation phase. The plan file is the deliverable in that mode.

### 6. Execute approved actions

In this order:

1. **CURRENT_GAPS.md edits first** (lower risk, reversible). Apply all close/fold moves. Preserve all `Ticketed` and `Closed` section content verbatim except for the gaps being moved INTO those sections.
2. **`gh issue close` for confirmed closures of existing tickets**. Include a closing comment that cites the CURRENT_GAPS.md rationale and the closing commit SHA if applicable.
3. **`gh issue edit` for confirmed body-update / retitle actions**. Right before each edit, re-fetch the issue body and check for any change since the plan was generated. If the body has diverged, surface the new diff and re-ask. Otherwise apply.
4. **`gh issue create` for confirmed new issues**. Use the body verbatim from the plan. After creation, edit CURRENT_GAPS.md: move the gap from `Discussing` to `Ticketed`, add the `→ <repo>#<N>` line, and convert the Pending checklist items to post-filing follow-ups (most can stay as-is).

After each successful action: log it inline so the user sees progress. After all actions: print a final summary block to stdout (not to a file):

```
gaps update: <area-name>
  CURRENT_GAPS.md edits: <N> applied (<F> aligned, <S> superseded, <T> Discussing→Ticketed)
  GitHub mutations: <N> filed, <M> updated, <K> closed
  plan file: <areas-dir>/<name>/gap-update-plan.md (DO-NOT-COMMIT)
```

## Five-action decision rubric

Use this when classifying gaps in step 3. When uncertain between two actions, prefer the lower-mutation one (HOLD over FILE, CLOSE-as-superseded over UPDATE-existing).

### When to HOLD

- Spec direction not yet signalled (WG hasn't responded to a proposal)
- Direction signalled but worded vaguely ("we should consider...", "maybe")
- The gap is monitor-only per explicit user direction noted in CURRENT_GAPS.md
- Worktree doesn't have enough surface yet to evaluate alignment
- The action would be a Discord message or content artifact, not a ticket (those belong in CURRENT_GAPS.md Pending items, not GitHub)

### When to CLOSE-as-aligned

- The "`<code.name>` side" file:line citations in the gap point at code that already implements the spec direction
- The "Closes when" criterion has been met by worktree commits since the gap was filed
- The Pending checklist has only verification-style items (`[ ]` grep this, `[ ]` confirm that) and the verifications now pass

Always verify with a fresh grep / read against the worktree. Don't trust the gap's citations blind, because file paths drift.

### When to CLOSE-as-superseded

- Another gap (typically newer) captures the same delta from a more current angle
- The spec direction shifted so the original framing is obsolete
- Multiple gaps form a single writeup or single ticket, so collapse to one

Always name the target gap explicitly. Move any non-trivial Pending items into the target's Pending list.

### When to FILE-new-issue (actionable)

- Spec text has LANDED on the upstream PR (a normative commit, not just a comment)
- `<code.name>` needs concrete code changes to align
- Wire shape, acceptance criteria, and out-of-scope can be written in a single PR description

Title format: `<package/path>: <one-line description of the change>`. No `[tracker]` prefix.

Body MUST include: spec reference (commit SHA + title), Current state, What this changes, Acceptance checklist, Out of scope.

### When to FILE-new-issue (tracker)

- Direction has been signalled by WG lead in a substantive way (not "maybe", but "yeah I'm open to...")
- Spec text has NOT yet landed
- Filing now gives a place to capture wire-shape diffs as the spec evolves

Title format: `[tracker] <package/path>: <one-line description>`. Always prefix with `[tracker]`.

Body MUST include: Spec direction (with comment / message citations), Current state, When spec text lands (acceptance checklist for the eventual merge), Out of scope.

### When to UPDATE-existing-issue

- An open issue already covers part of the gap, but spec direction has shifted (e.g. original scope → three verification paths)
- The original issue scope is now too narrow OR too broad
- A retitle would more accurately describe current scope

For full body replacement: surface the proposed body verbatim. The user reviews and confirms. Apply only with explicit per-item approval.

For closures of existing open tickets (when the gap is Closed but the ticket is still Open): always include a closing comment that explains why, with a link to the CURRENT_GAPS.md section.

## Worktree config in area.yaml

If the area needs a non-default worktree (feature branch, monorepo subdir, alt repo), add to `<areas-dir>/<name>/area.yaml`:

```yaml
worktree: <abs path to git checkout>    # path to git checkout
repo: <org/repo>                        # GitHub repo for issues
default_label: <label>                  # used as --label filter default
```

All three fields optional. CLI args override.

## Style notes

Beyond the shared notes in `SKILL.md`:

- Quote stale citations from CURRENT_GAPS.md verbatim in backticks before contrasting them with the verified state. That helps the user find and fix the original gap text.
- When proposing CLOSE-as-superseded, name explicitly which target gap absorbs which Pending items. Don't say "moved into G28". Say "G28 Pending now also has `[ ] watch Casey response to CIMD design` (from G23) and `[ ] confirm tuple-keying admits per-client URL` (from G12)."

## Gotchas

Beyond the shared gotchas in `SKILL.md`:

- **`gh issue edit --body-file` is a full overwrite.** Re-fetch right before the edit and surface any drift. Authorized full-body rewrites are fine, silent overwrites are not.
- **`gh issue create` is irreversible** in the cross-reference sense. Even closing the issue later does not undo the backlinks created on linked items in the body. Confirm per item.
- **Worktree on a feature branch.** If the worktree HEAD differs from `<repo>/main`, the plan-file header flags this loudly. Some "verified at" findings may not be on main yet, so mark them as "verified on branch `<branch>`, not yet on main" and the user knows what is actually merged.
- **Multi-gap consolidation.** When proposing CLOSE-as-superseded, double-check that the absorbing gap is not itself proposed for closure in the same run. If it is, propose a chain ("close G23 → G28 → ...") and execute in the right order.
- **Channel.yaml `paths`** is a `/gaps channel` hint about which paths to git-log. It does not change the worktree path, which is `area.yaml`'s `worktree:` field. Don't confuse the two.
## Example invocations

```bash
# Standard refresh + groom flow:
/gaps channel <area-name>
/gaps update <area-name>

# Review-only pass:
/gaps update <area-name> --dry-run

# Against a feature branch worktree:
/gaps update <area-name> --worktree <path>

# Different downstream repo:
/gaps update <area-name> --repo <org/repo> --label <label>
```
