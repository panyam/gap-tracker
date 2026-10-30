---
name: social
description: "Build a daily reply habit on Reddit (X later) from socialtrack's digest: sync, pick the threads worth answering today, draft a reply for each in the user's voice, and record what was replied to or skipped. Never posts anything; the user posts by hand. Use when the user says \"/social\", \"social today\", \"what should I reply to\", \"reddit threads\", \"reply digest\", \"my streak\", or \"mark that replied\"."
---

# social

The daily reply loop over `socialtrack` (gap-tracker, `internal/socialtrack`). The tracking root is
found by walking up from cwd for `.social-tracking.yaml`, else `$SOCIALTRACK_ROOT`, else pass
`-C <root>`. Keep the root in a private repo: its config names what you watch, and its store is
your reply history.

**Nothing in this skill posts, votes, comments or messages on any site.** Drafts go to the user,
who posts them by hand. That holds even if the user asks to "just post it": say it is not wired to
post, and hand over the text.

## Modes

### `/social` or `/social today`

1. Run `socialtrack -C <root> digest -o json` (add `--topic <name>` when the user names one). If it
   is empty or the newest posts are hours old, run `socialtrack -C <root> today` instead, which
   syncs, checks reply counts on the top candidates and then digests. It honours Reddit's rate
   window of about one request a minute, so it takes several minutes: run it in the background
   and say so rather than waiting silently.
2. The digest is ranked by impact (matched terms, the subreddit's weight, whether it asks
   something) times urgency (age, and how many replies it already has). Take the top **five**
   the user can add something to today. Prefer, in order: a direct
   question in the user's domain (schematic review, a missing pull-up, decoupling, ESD, connectors,
   KiCad and EDIF tooling), a review request with a concrete schematic, a mention of agni. Skip
   threads that are already well answered, that want a buying recommendation, or where the honest
   reply is "it depends" with nothing to add.
3. Read each of the five with `socialtrack -C <root> show <id>`, which prints the post and its
   comments from the thread's own feed (the access Reddit offers without an API key; about a minute
   a thread within its rate window). Do not open Reddit pages with WebFetch or a browser: Reddit
   refuses automated page loads. If the user pastes a thread instead, use that.
4. Draft one reply per thread, following the voice and rules below. Show each as: the thread title,
   its URL, one line on why it is worth answering, then the draft in a quote block.
5. End with the commands to record the outcome, one per thread:
   `socialtrack -C <root> mark <id> replied --note "<one line>"` or `... skipped`.
   Then `socialtrack -C <root> streak`.

### `/social x`

Run `socialtrack -C <root> browse` (add `--topic <name>` if named). It opens a live X search over
the topic's curated accounts in the user's browser and records the visit. **Do not read X yourself**:
X forbids automated reading, so the user scrolls, and pastes or names the posts worth answering.
Then `add` each one, and draft from what the user pasted.

### `/social add <url>`

Run `socialtrack -C <root> add <url>` for a thread the user found themselves, on any network. A
Reddit link fills itself in from the thread's feed; any other link needs `--title "<title>"`, which
you take from what the user pasted or said. Add `--topic <name>` when no topic's keywords match.

### `/social streak`

Run `socialtrack -C <root> streak` and show it as is.

### `/social mark <id> replied|skipped`

Run the mark command for the user, with a one-line note if they gave one.

## Voice

Write as the user, in first person, the way they write to people: casual, short lines, one point
each, raised as an observation rather than a verdict ("I wonder if SDA needs its own pull-up here",
"Also not sure the TVS is on the connector side"). No headers, no bullet lists unless the thread
itself is a list, no "Great question!", no sign-off. Keep the electrical facts exact; if a fact is
uncertain, say what would settle it (a datasheet section, a measurement).

## Rules for mentioning the user's projects

- Answer the question first, fully, without agni. A reply that only makes sense as an ad is skipped.
- Mention one of the user's projects (agni, or another topic's) in at most one reply in three across all topics, and only where it does the thing being asked (finding
  a missing pull-up, checking a KiCad schematic in the browser). Say plainly that it is the user's
  own project: "I've been building a checker that flags exactly this (disclosure: it's mine)".
- Never in a subreddit whose rules forbid self-promotion. Check the sidebar rules when unsure.
- Never claim a check agni does not ship. When in doubt, look it up in
  the agni repo's `docsite/content/reference/rules/`.
