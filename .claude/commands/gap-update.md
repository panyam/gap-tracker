Reconcile an area's `CURRENT_GAPS.md` against the code repo and open issue list. Proposes per-gap actions (close / fold / file new ticket / update existing ticket / hold), surfaces backlink-firing references, takes per-action confirmation, then executes the approved actions. Mutates BOTH `CURRENT_GAPS.md` AND `gh` state. Pair with `/channel-refresh` or `/proposal-refresh` (which write CURRENT_GAPS.md but don't touch tickets).

## Configuration

First, resolve the tracking root and load `<tracking-root>/.discord-tracking.yaml`. See `~/.claude/discord-tracking-playbook.md` for the resolver and config schema. `<areas-dir>`, `<code.name>`, `<code.repo>`, `<code.default_worktree>`, `<gapt.project_id>` refer to values from that config.

If the resolver fails, bail with the playbook's error message.

## Arguments

`$ARGUMENTS` — `<area-folder-name> [--worktree <path>] [--repo <org/repo>] [--label <label>] [--dry-run]`

- `<area-folder-name>` (required) — name of the folder under `<areas-dir>/`. Used to locate `CURRENT_GAPS.md` and `area.yaml`. If `area.yaml` has a `worktree:` field, that's the default code-repo checkout; otherwise default is `<code.default_worktree>` from config.
- `--worktree <path>` (optional) — override the worktree path. Used when the gap-side analysis should run against a feature branch worktree, not main.
- `--repo <org/repo>` (optional) — the GitHub repo to file/edit issues against. Default: `area.yaml`'s `repo:` if set, else `<code.repo>` from config. Different areas may target different downstream repos.
- `--label <label>` (optional) — filter the existing-issue query to a single label (e.g. `prod-events`). Defaults to whatever `area.yaml` declares in a `default_label:` field, otherwise no filter.
- `--dry-run` (optional) — produce the plan file and stop; skip the per-action confirmation + execution phase. Useful for review-only passes.

## Output

- **Always**: `<areas-dir>/<name>/gap-update-plan.md` (DO-NOT-COMMIT working file, overwritten each run, gitignored via `<areas-dir>/*/gap-update-plan.md` pattern). This is the proposal document the user reads.
- **On execution**: edits to `<areas-dir>/<name>/CURRENT_GAPS.md`. New issues filed via `gh issue create`. Existing issues edited via `gh issue edit`. Closed issues closed via `gh issue close`.

The skill does NOT:
- Modify the area's `README.md`. Stale findings about README stay surfaced in `CURRENT_GAPS.md` for the human.
- Run `/channel-refresh` or `/proposal-refresh`. Inputs are taken at face value — if `CURRENT_GAPS.md` is stale, refresh it first.
- Re-classify gaps that are already in the `Closed` section. Those are history.
- Auto-promote gaps between `Discussing` → `Finalized` → `Ticketed` without an explicit "file ticket" or "update ticket" action proposed and approved. The lifecycle transition is the *result* of executing an approved action, not a separate move.

## Steps

### 1. Validate inputs

Resolve `<area-folder-name>`:

- Look for `<areas-dir>/<name>/CURRENT_GAPS.md`. If missing, fail fast with a "did you mean" list of folders that have `CURRENT_GAPS.md`. Suggest running `/channel-refresh <name>` first.
- Look for `<areas-dir>/<name>/area.yaml`. If present, read optional `worktree:`, `default_label:`, `repo:` fields. Missing area.yaml is fine; defaults apply.

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

For every gap NOT in the `Closed` section, propose exactly one of five actions. Verify each against worktree code + ticket state before proposing — don't propose blind off CURRENT_GAPS.md alone.

The five actions:

#### HOLD

No action. The gap exists, direction isn't firm enough to file a tracker, and worktree code can't close it. Output: just the gap ID + a one-line rationale ("waiting on spec text from <author>'s <date> design").

#### CLOSE-as-aligned

Worktree code already satisfies the gap's "Closes when" criterion. Run grep / read against the worktree to verify the file:line citations in the gap. Surface the verification evidence (commit SHA, file:line, snippet). Propose CURRENT_GAPS.md edit: move from Discussing to Closed with a "verified at commit <SHA>" line.

#### CLOSE-as-superseded

Another gap absorbs this one's delta. Name the target gap explicitly. Propose Pending-item absorption (move open `[ ]` items from this gap into the target gap's Pending list). Propose CURRENT_GAPS.md edit: move from Discussing to Closed with "superseded by G<n>" line.

#### FILE-new-issue

Direction firm enough to file an issue. Two sub-shapes:

- **Actionable** — title without `[tracker]` prefix. Body has "What this changes" + "Acceptance" sections. Direction is normative (spec text landed, or a clearly-firm WG decision).
- **Tracker** — title prefixed with `[tracker]`. Body has "Spec direction" + "Current state" + "When spec text lands" sections. Direction signalled but spec text not landed.

In both cases, draft title + body + labels, AND list backlink-firing references explicitly. Backlink rules per `~/.claude/CLAUDE.md`:

- Default to plain text or backtick-wrapped form for upstream `org/repo#N` cross-refs (suppresses backlink on the upstream item).
- Same-repo `#N` refs fire backlinks on own-repo items; that's expected and fine.
- URLs (e.g. `https://github.com/foo/bar`) don't fire cross-reference backlinks; only `org/repo#N` syntax does.

#### UPDATE-existing-issue

An open issue already covers part of this gap, but its body is stale or its scope has shifted. Fetch the current body via `gh issue view --json body`, show the proposed diff in the plan, list backlink-firing refs in the new body, ask per-item confirmation.

The body REWRITE pattern (full overwrite) is fine when explicitly authorized. The fetch-before-write rule from `CLAUDE.md` applies: re-fetch right before the edit to catch any concurrent change.

For closed gaps that still have an OPEN issue: propose `gh issue close <N>` with a closing comment that references the closing rationale from CURRENT_GAPS.md.

### 4. Write the plan file

`<areas-dir>/<name>/gap-update-plan.md` — full overwrite, no merge.

Plan-file header MUST start with this exact block (one trailing blank line):

```markdown
> [!WARNING]
> DO NOT COMMIT. This file is ephemeral working state produced by `/gap-update`.
> It is overwritten on every run and gitignored. Do not check it in.

# Gap-update plan: <area-name>

Generated <YYYY-MM-DD HH:MM TZ> by `/gap-update`
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

Per the user's global backlink rule: brand-new GitHub issue creation REQUIRES per-issue confirmation that surfaces the planned title, body, and backlink-firing refs. Don't batch-confirm across multiple new issues; one question per issue.

For CLOSE-as-aligned and CLOSE-as-superseded actions (which mutate CURRENT_GAPS.md only): batch-confirm at the top of the run with a single "approve all close/fold actions?" question, or per-item if there are more than 5. The CURRENT_GAPS.md edits are reversible via git; the lower-confirmation threshold is fine.

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
gap-update: <area-name>
  CURRENT_GAPS.md edits: <N> applied (<F> aligned, <S> superseded, <T> Discussing→Ticketed)
  GitHub mutations: <N> filed, <M> updated, <K> closed
  plan file: <areas-dir>/<name>/gap-update-plan.md (DO-NOT-COMMIT)
```

## Five-action decision rubric

Use this when classifying gaps in step 3. When uncertain between two actions, prefer the lower-mutation one (HOLD over FILE; CLOSE-as-superseded over UPDATE-existing).

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

Always verify with a fresh grep / read against the worktree. Don't trust the gap's citations blind — file paths drift.

### When to CLOSE-as-superseded

- Another gap (typically newer) captures the same delta from a more current angle
- The spec direction shifted so the original framing is obsolete
- Multiple gaps form a single writeup or single ticket — collapse to one

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

- Plan-file prose: no em dashes, no semicolons (per user style). Match CURRENT_GAPS.md voice.
- Issue body prose: same.
- Cite worktree state by short SHA + file:line. "verified at `9b913b62`, `events.go:824`" reads better than "verified in the codebase."
- Quote stale citations from CURRENT_GAPS.md verbatim in backticks before contrasting with the verified state. Helps the user find and fix the original gap text.
- When proposing CLOSE-as-superseded: explicitly name which target gap absorbs which Pending items. Don't say "moved into G28"; say "G28 Pending now also has `[ ] watch Casey response to CIMD design` (from G23) and `[ ] confirm tuple-keying admits per-client URL` (from G12)."

## Gotchas

- **`gh issue edit --body-file` is a full overwrite.** Re-fetch right before the edit and surface any drift. Authorized full-body rewrites are fine; silent overwrites are not.
- **`gh issue create` is irreversible** in the cross-reference sense. Even closing the issue later doesn't undo the backlinks created on linked items in the body. Confirm per item.
- **Worktree on feature branch**. If the worktree HEAD differs from `<repo>/main`, the plan-file header flags this loudly. Some "verified at" findings may not be on main yet; mark them as "verified on branch `<branch>`, not yet on main" so the user knows what's actually merged.
- **Backlinks already created** by past commits / PRs cannot be retroactively removed. Editing this skill's plan or body before execution prevents NEW backlinks; old ones stay.
- **gapt re-classification is NOT part of this skill.** If the gap involves a gapt-classified upstream PR, surface the current classification + rationale + classified-at date as evidence in the plan but don't propose to reclassify. That's a separate `gapt classify` workflow.
- **Multi-gap consolidation**: when proposing CLOSE-as-superseded, double-check that the absorbing gap isn't itself proposed for closure in the same run. If it is, propose a chain ("close G23 → G28 → ...") and execute in the right order.
- **Channel.yaml `paths`** is a Phase A (channel-refresh) hint about which paths to git-log. It does not change the worktree path — that's `area.yaml`'s `worktree:` field. Don't confuse the two.

## Example invocations

```bash
# Standard refresh + groom flow:
/channel-refresh <area-name>
/gap-update <area-name>

# Review-only pass:
/gap-update <area-name> --dry-run

# Against a feature branch worktree:
/gap-update <area-name> --worktree <path>

# Different downstream repo:
/gap-update <area-name> --repo <org/repo> --label <label>
```
