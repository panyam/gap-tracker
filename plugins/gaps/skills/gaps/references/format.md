# CURRENT_GAPS.md format

The generation template, loaded by `/gaps channel` and `/gaps proposal` when they
synthesize the file. `/gaps update` edits an existing file rather than
regenerating one, so it does not need this reference. The per-gap shape rules
that every mode needs live in `SKILL.md`.

## Re-running over an existing CURRENT_GAPS.md

`channel` and `proposal` both overwrite the file, and both carry these four
responsibilities when a prior version exists. `update` edits in place and does
not re-synthesize.

- **Preserve `Ticketed` and `Closed` verbatim.** Those sections carry history that synthesis must not overwrite.
- **Walk existing Pending checklists.** For each gap, scan the `**Pending**` list. Items verifiable as done (commit landed, comment posted, file mtime moved past the cited date, issue closed) flip from `[ ]` to `[x]` with a verifier reference. Items that became invalid get `[~]` plus a one-line reason. New items get appended.
- **Don't re-propose what's already tracked.** Before drafting any new Discussing or Finalized entry, scan every section for subject overlap. Skip a candidate that looks substantially the same as a tracked gap (same SEP, feature, or decision). Surface anything ambiguous under "Notes for next refresh" rather than re-proposing it.
- **Stable IDs.** Read the existing gap IDs first. A gap the user moved between sections keeps its ID. New gaps get `G<max-existing + 1>`. IDs never decrement and never collide.

When both modes write the same file, treat the other mode's entries as existing
gaps. Augment them rather than overwriting.

## CURRENT_GAPS.md format

Written by `channel` and `proposal`, read and edited by `update`. Sections in
this order:

````markdown
# Current gaps: <area-name>

Last refreshed: YYYY-MM-DD by /gaps <mode> [--window <w>]
Window: <ISO_START> → <ISO_END>

This file tracks **deltas between upstream (channel/spec) and (<code.name>)**, meaning places where upstream and our implementation are out of sync. Each gap names the spec side, the code side, and what would close the delta. Sections track lifecycle: **Discussing** (delta exists, direction unclear) → **Finalized** (direction decided, work pending) → **Ticketed** (filed as upstream issue or downstream issue/PR) → **Closed** (delta gone, by alignment OR intentional divergence). Gap IDs are stable across runs. The human owns transitions between sections.

## Activity in window

Bulleted. What happened since `<ISO_START>`:

- Discord: <N messages from M authors, with a one-line characterization of each substantive thread>
- <code.name>: <commits in declared paths within window, or "no commits">
- gapt: <classification changes within window, or "no classification changes">
- Spec PR: <new comments/commits if discoverable, otherwise note "not synced via gapt yet">

If the window is empty, say so in one line. Don't pad.

## Summary

One paragraph cumulative view. What is upstream converging toward, where does <code.name> stand relative to that, what is the strategic situation right now. Window-independent.

## Discussing

For each gap where the delta exists and the direction is still unclear:

### G<n>: <short title>

**Where this stands**: <one-line tl;dr the human can read in 5 seconds>

**Spec/channel side**: <citation, meaning comment ID + author + date, or message ID + author + date, and what upstream is saying>
**<code.name> side**: <file:line citation, commit SHA, or "no impl yet", meaning where we actually are>
**Closes when**: <criterion, meaning what state of the world makes this gap gone>

**Pending**:
- [ ] <action item: research, decision, draft a reply, run a grep, etc.>
- [ ] <action item>
- [x] <done item>, verified by <commit SHA, comment ID, file path:line, issue URL>
- [~] <invalidated item>, <one-line reason>

## Finalized

Same shape as Discussing. The difference is that the direction is decided. "Where this stands" reads like a conclusion ("ready to file as `<code.name>` issue: X"). Pending items are concrete enough to file as tickets.

## Ticketed

Carry over from the existing CURRENT_GAPS.md. Format:

### G<n>: <title> → <repo>#<N> (filed YYYY-MM-DD)

**Where this stands**: <one line covering issue state and what is blocking close>

**Spec/channel side**: <unchanged from when filed, or noted if upstream has shifted>
**<code.name> side**: <pointer to PR/branch if work is in flight>
**Closes when**: <unchanged from when filed>

**Pending** (post-filing follow-ups):
- [ ] <e.g. shepherd review, update on upstream movement>

## Closed

Carry over from the existing CURRENT_GAPS.md. Format:

### G<n>: <title>, closed YYYY-MM-DD

<one-line resolution: how the delta went away, whether by alignment, intentional divergence, upstream withdrawal, or something else>

Closed gaps are history. No Pending list.

## Where README looks stale

Bullets. Quote the stale claim and contrast with current reality. Do NOT auto-fix README, surface it for the user to review.

- README says "X", but DB / gapt / `<code.name>` shows "Y" (as of <ts>).

## Notes for next refresh

Anything the user should think about by next invocation. Stale Pending items (open `[ ]` for more than 2 weeks since last update), auth-gated docs that need manual review (Google Docs, IETF drafts), unsynced sources (Discord threads, gapt config gaps).
````

