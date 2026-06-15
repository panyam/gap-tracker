#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["playwright>=1.40"]
# ///
"""discord/snapshot.py - Discord channel scroll-aware reader.

Drives Playwright + Chromium with a persistent profile to navigate to a Discord
channel, scroll backward until --since is reached, extract message data from
the DOM, and emit one JSONL record per message to stdout.

Logs and progress go to stderr. JSONL records go to stdout. Pipe stdout to
`mcpcontribd ingest` (or capture to a file for inspection).

Stop conditions (whichever comes first):
  - oldest visible ts <= --since
  - max iterations reached
  - no new messages after a scroll (top of channel, or rate-limited)
"""

import argparse
import asyncio
import json
import sys
from pathlib import Path

# Local sibling import for shared profile resolution. PEP 723 manages deps,
# but Python's normal sys.path resolution still finds modules adjacent to the
# script. The leading-underscore name signals "not a CLI, importable only."
sys.path.insert(0, str(Path(__file__).resolve().parent))
from _profile import resolve_profile_dir, ensure_profile_dir

from playwright.async_api import async_playwright

DEFAULT_MAX_ITERS = 10
SCROLL_WAIT_MS = 2000
RENDER_TIMEOUT_MS = 60000


def log(msg: str) -> None:
    print(msg, file=sys.stderr, flush=True)


EXTRACT_JS = r"""
() => {
  function findAuthor(item) {
    const directEl = item.querySelector('[id^="message-username-"]');
    if (directEl) {
      const t = directEl.textContent.trim();
      if (t) return t;
    }
    let prev = item.previousElementSibling;
    while (prev) {
      if (prev.id && prev.id.startsWith('chat-messages-')) {
        const prevAuthor = prev.querySelector('[id^="message-username-"]');
        if (prevAuthor) {
          const t = prevAuthor.textContent.trim();
          if (t) return t;
        }
      }
      prev = prev.previousElementSibling;
    }
    return '';
  }

  const items = document.querySelectorAll('li[id^="chat-messages-"]');
  const out = [];
  for (const item of items) {
    const idMatch = item.id.match(/^chat-messages-(\d+)-(\d+)$/);
    if (!idMatch) continue;
    const channelId = idMatch[1];
    const messageId = idMatch[2];

    const timeEl = item.querySelector(
      `#message-timestamp-${messageId}, time[id="message-timestamp-${messageId}"]`
    ) || item.querySelector('time[datetime]:not([id^="message-edited-timestamp-"])');
    const ts = timeEl ? timeEl.getAttribute('datetime') : null;
    if (!ts) continue;

    const author = findAuthor(item);

    const contentEl = item.querySelector(`[id="message-content-${messageId}"]`);
    const content = contentEl ? contentEl.innerText.trim() : '';

    const urls = [];
    if (contentEl) {
      const anchors = contentEl.querySelectorAll('a[href]');
      for (const a of anchors) {
        const href = a.getAttribute('href');
        if (href && href.startsWith('http')) urls.push(href);
      }
    }

    const editedEl = item.querySelector(`time[id="message-edited-timestamp-${messageId}"]`);
    const editedTs = editedEl ? editedEl.getAttribute('datetime') : null;

    let replyTo = null;
    const replyContent = item.querySelector('[class*="repliedMessage"] [id^="message-content-"]');
    if (replyContent && replyContent.id) {
      const m = replyContent.id.match(/^message-content-(\d+)$/);
      if (m) replyTo = m[1];
    }

    out.push({
      channel_id: channelId,
      message_id: messageId,
      ts: ts,
      author: author,
      content: content,
      urls: urls,
      edited_ts: editedTs,
      reply_to: replyTo,
    });
  }
  return out;
}
"""


LOGIN_TIMEOUT_MS = 5 * 60 * 1000  # 5 minutes


async def login_flow(ctx, channel_url: str) -> int:
    """Interactive login mode. Opens Discord; waits up to 5 minutes for the
    chat container to render (which means user logged in and channel loaded);
    then closes the context cleanly so the profile persists."""
    page = ctx.pages[0] if ctx.pages else await ctx.new_page()
    await page.goto(channel_url, wait_until="domcontentloaded")
    log("")
    log("=" * 60)
    log("LOGIN MODE")
    log("A Chromium window has opened. Log into Discord and the channel")
    log("should load. The script will detect login automatically and exit.")
    log(f"Waiting up to {LOGIN_TIMEOUT_MS // 1000}s for chat to render...")
    log("=" * 60)
    log("")
    try:
        await page.wait_for_selector(
            'li[id^="chat-messages-"]', timeout=LOGIN_TIMEOUT_MS
        )
        log("login detected (chat container rendered). Saving profile.")
        return 0
    except Exception:
        log("timeout: did not detect login within window.")
        log("Profile may still have partial state. Retry --login if needed.")
        return 1


async def main_async(args) -> int:
    profile_dir = resolve_profile_dir(args.profile, args.profile_dir)
    ensure_profile_dir(profile_dir)

    channel_url = f"https://discord.com/channels/{args.server_id}/{args.channel}"
    log(f"navigating to {channel_url}")
    log(f"profile: {args.profile or 'default (from $DISCTRACK_PROFILE or fallback)'}")
    log(f"profile dir: {profile_dir}")

    async with async_playwright() as p:
        ctx = await p.chromium.launch_persistent_context(
            str(profile_dir),
            headless=args.headless,
            viewport={"width": 1280, "height": 900},
        )
        try:
            if args.login:
                return await login_flow(ctx, channel_url)

            page = ctx.pages[0] if ctx.pages else await ctx.new_page()
            await page.goto(channel_url, wait_until="domcontentloaded")

            try:
                await page.wait_for_selector(
                    'li[id^="chat-messages-"]', timeout=RENDER_TIMEOUT_MS
                )
            except Exception:
                log(
                    "error: no messages rendered within "
                    f"{RENDER_TIMEOUT_MS}ms. Likely not logged in."
                )
                log(
                    "Run once with --login to log in interactively. Future "
                    "runs will reuse the saved profile."
                )
                return 1

            seen_ids: set[tuple[str, str]] = set()
            iter_count = 0
            oldest_seen: str | None = None
            newest_seen: str | None = None
            emitted = 0
            since = args.since.strip()

            while iter_count < args.max_iters:
                iter_count += 1
                batch = await page.evaluate(EXTRACT_JS)
                new_in_batch = 0
                batch_oldest: str | None = None
                batch_newest: str | None = None

                for m in batch:
                    key = (m["channel_id"], m["message_id"])
                    if key in seen_ids:
                        continue
                    seen_ids.add(key)
                    new_in_batch += 1
                    print(json.dumps(m), flush=True)
                    emitted += 1
                    if batch_oldest is None or m["ts"] < batch_oldest:
                        batch_oldest = m["ts"]
                    if batch_newest is None or m["ts"] > batch_newest:
                        batch_newest = m["ts"]

                if batch_oldest is not None and (
                    oldest_seen is None or batch_oldest < oldest_seen
                ):
                    oldest_seen = batch_oldest
                if batch_newest is not None and (
                    newest_seen is None or batch_newest > newest_seen
                ):
                    newest_seen = batch_newest

                log(
                    f"  scroll {iter_count}/{args.max_iters}  "
                    f"new={new_in_batch}  oldest={batch_oldest}  "
                    f"newest={batch_newest}"
                )

                if since and batch_oldest and batch_oldest <= since:
                    log(f"  cursor reached (oldest {batch_oldest} <= since {since})")
                    break
                if new_in_batch == 0:
                    log("  no new messages after scroll, stopping")
                    break

                # Scroll the chat container directly via the DOM instead of
                # pressing PageUp. PageUp targets whatever element holds
                # keyboard focus — after page.goto Discord typically focuses
                # the message input box, where PageUp is a no-op. Result:
                # snapshot.py would stop after the first viewport with
                # new_in_batch=0 even though years of history sit above.
                #
                # Walk up from any visible message to find the scrollable
                # ancestor, then jump scrollTop up by ~2 viewport heights.
                # Discord's virtualizer lazy-loads older messages as the
                # container's scrollTop changes.
                scroll_result = await page.evaluate(
                    """
                    () => {
                      const msg = document.querySelector('li[id^="chat-messages-"]');
                      if (!msg) return { error: 'no message element' };
                      let el = msg.parentElement;
                      while (el && el !== document.body) {
                        const s = window.getComputedStyle(el);
                        const isScroll =
                          (s.overflowY === 'auto' || s.overflowY === 'scroll') &&
                          el.scrollHeight > el.clientHeight;
                        if (isScroll) {
                          const before = el.scrollTop;
                          const heightBefore = el.scrollHeight;
                          const step = Math.max(el.clientHeight * 2, 1500);
                          el.scrollTop = Math.max(0, before - step);
                          return {
                            ok: true,
                            before,
                            after: el.scrollTop,
                            clientHeight: el.clientHeight,
                            heightBefore,
                            step,
                          };
                        }
                        el = el.parentElement;
                      }
                      return { error: 'no scrollable ancestor found' };
                    }
                    """
                )
                if isinstance(scroll_result, dict) and scroll_result.get("error"):
                    log(f"  scroll failed: {scroll_result['error']}; stopping")
                    break
                await page.wait_for_timeout(SCROLL_WAIT_MS)
                # Check whether Discord actually lazy-loaded more history by
                # comparing scrollHeight before vs after the wait. Growing
                # scrollHeight = older messages were prepended into the DOM.
                # Unchanged at scrollTop=0 = real top of channel.
                post_scroll = await page.evaluate(
                    """
                    () => {
                      const msg = document.querySelector('li[id^="chat-messages-"]');
                      if (!msg) return null;
                      let el = msg.parentElement;
                      while (el && el !== document.body) {
                        const s = window.getComputedStyle(el);
                        if ((s.overflowY === 'auto' || s.overflowY === 'scroll') && el.scrollHeight > el.clientHeight) {
                          return { scrollTop: el.scrollTop, scrollHeight: el.scrollHeight };
                        }
                        el = el.parentElement;
                      }
                      return null;
                    }
                    """
                )
                height_after = (
                    f"{post_scroll['scrollHeight']:.0f}" if post_scroll else "N/A"
                )
                log(
                    f"    scroll: before={scroll_result['before']:.0f} "
                    f"after={scroll_result['after']:.0f} "
                    f"height_before={scroll_result['heightBefore']:.0f} "
                    f"height_after={height_after} "
                    f"viewport={scroll_result['clientHeight']:.0f}"
                )

            summary = {
                "iterations": iter_count,
                "emitted": emitted,
                "oldest_seen": oldest_seen,
                "newest_seen": newest_seen,
            }
            log("summary: " + json.dumps(summary))
            return 0
        finally:
            await ctx.close()


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Scroll-aware Discord channel reader. Emits JSONL on stdout."
    )
    parser.add_argument(
        "--server-id",
        required=True,
        help="Discord server (guild) ID. Combined with --channel forms the URL.",
    )
    parser.add_argument("--channel", required=True, help="Discord channel ID")
    parser.add_argument(
        "--since",
        default="",
        help="ISO8601 timestamp. Stop scrolling once oldest visible ts <= this.",
    )
    parser.add_argument(
        "--max-iters",
        type=int,
        default=DEFAULT_MAX_ITERS,
        help=f"Maximum scroll iterations (default {DEFAULT_MAX_ITERS})",
    )
    parser.add_argument(
        "--headless",
        action="store_true",
        help="Run with an invisible browser. Default is headed so you can see "
        "what's happening (and catch a login-expired state immediately).",
    )
    parser.add_argument(
        "--login",
        action="store_true",
        help="Interactive login mode. Opens browser, waits until chat renders, "
        "then closes cleanly so the profile saves. Run once before normal sync.",
    )
    parser.add_argument(
        "--profile",
        default=None,
        help="Named profile resolved under ~/.local/share/disctrack/profiles/<name>/. "
        "Falls back to $DISCTRACK_PROFILE then 'default'.",
    )
    parser.add_argument(
        "--profile-dir",
        default=None,
        help="Explicit Chromium user-data-dir path (overrides --profile).",
    )
    args = parser.parse_args()
    sys.exit(asyncio.run(main_async(args)))


if __name__ == "__main__":
    main()
