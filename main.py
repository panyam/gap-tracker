#!/usr/bin/env python3
"""
gap-tracker: Track feature gaps between public reference repos and a local project.

Supports many-to-one: multiple source repos feeding into a single target repo.

Usage:
    python main.py sync <project> <offset> [--count N]
    python main.py report <project>
    python main.py crossref <project>
    python main.py show <project> <issue_ref>
    python main.py link <project> <feature> <target_issue#> [source_evidence]
    python main.py mark <project> <feature> <status>

Example:
    python main.py sync mcpkit go-sdk:0        # sync go-sdk from offset 0
    python main.py sync mcpkit spec:100        # sync spec from offset 100
    python main.py sync mcpkit 0               # auto-select if single source
    python main.py report mcpkit
    python main.py crossref mcpkit
    python main.py show mcpkit go-sdk:666
    python main.py show mcpkit 56
"""

import json
import os
import re
import sqlite3
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

SCRIPT_DIR = Path(__file__).parent


def get_config(project: str) -> dict:
    config_path = SCRIPT_DIR / project / "config.json"
    if not config_path.exists():
        print(f"ERROR: {config_path} not found")
        sys.exit(1)
    return json.loads(config_path.read_text())


def get_db(project: str) -> sqlite3.Connection:
    db_path = SCRIPT_DIR / project / "gaps.db"
    db = sqlite3.connect(str(db_path))
    db.row_factory = sqlite3.Row
    db.execute("PRAGMA journal_mode=WAL")
    _create_tables(db)
    return db


def _create_tables(db: sqlite3.Connection):
    db.executescript("""
        CREATE TABLE IF NOT EXISTS issues (
            repo        TEXT NOT NULL,
            number      INTEGER NOT NULL,
            title       TEXT NOT NULL,
            state       TEXT NOT NULL,
            labels      TEXT DEFAULT '',
            created_at  TEXT NOT NULL,
            updated_at  TEXT NOT NULL,
            url         TEXT NOT NULL,
            body        TEXT DEFAULT '',
            synced_at   TEXT NOT NULL DEFAULT (datetime('now')),
            PRIMARY KEY (repo, number)
        );

        CREATE TABLE IF NOT EXISTS gaps (
            id              INTEGER PRIMARY KEY AUTOINCREMENT,
            feature         TEXT NOT NULL UNIQUE,
            description     TEXT DEFAULT '',
            target_issue    INTEGER,
            source_evidence TEXT DEFAULT '',
            status          TEXT NOT NULL DEFAULT 'open',
            priority        TEXT DEFAULT '',
            notes           TEXT DEFAULT '',
            created_at      TEXT NOT NULL DEFAULT (datetime('now')),
            updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
        );

        CREATE TABLE IF NOT EXISTS sync_log (
            id          INTEGER PRIMARY KEY AUTOINCREMENT,
            repo        TEXT NOT NULL,
            synced_at   TEXT NOT NULL DEFAULT (datetime('now')),
            issue_count INTEGER NOT NULL,
            offset_used INTEGER DEFAULT 0,
            count_used  INTEGER DEFAULT 100
        );
    """)


# ---------------------------------------------------------------------------
# Sync
# ---------------------------------------------------------------------------

def _gh_env() -> dict:
    token = os.environ.get("GH_TOKEN", os.environ.get("GH_PERSONAL_TOKEN", ""))
    env = {**os.environ}
    if token:
        env["GH_TOKEN"] = token
    return env


def fetch_issues(repo: str, offset: int = 0, count: int = 100) -> list[dict]:
    """Fetch issues from a GitHub repo starting at a given issue number offset.

    Uses `since` param with issue number sorting to approximate offset.
    GitHub doesn't support issue-number-based offset directly, so we fetch
    by page and filter.
    """
    env = _gh_env()

    # GitHub REST API: per_page max is 100. We use page-based pagination
    # to cover the requested range. offset=0 means start from most recent.
    page = (offset // 100) + 1
    per_page = min(count, 100)

    all_issues = []
    fetched_pages = 0
    remaining = count

    while remaining > 0 and fetched_pages < 10:  # hard cap: 10 pages max per call
        batch_size = min(remaining, 100)
        endpoint = f"repos/{repo}/issues?per_page={batch_size}&page={page}&state=all&sort=created&direction=asc"

        result = subprocess.run(
            ["gh", "api", endpoint],
            capture_output=True, text=True, env=env,
        )
        if result.returncode != 0:
            print(f"    ERROR page {page}: {result.stderr.strip()}")
            break

        try:
            raw = json.loads(result.stdout)
        except json.JSONDecodeError:
            break

        # Filter out PRs
        issues = [
            {
                "number": i["number"],
                "title": i["title"],
                "state": i["state"].upper(),
                "labels": [l["name"] for l in i.get("labels", [])],
                "createdAt": i["created_at"],
                "updatedAt": i["updated_at"],
                "url": i["html_url"],
                "body": (i.get("body") or "")[:2000],
            }
            for i in raw
            if "pull_request" not in i
        ]

        all_issues.extend(issues)
        remaining -= len(raw)  # count raw items (including PRs) for pagination
        fetched_pages += 1

        if len(raw) < batch_size:
            break  # last page
        page += 1

    return all_issues


def _upsert_issues(db: sqlite3.Connection, repo: str, issues: list[dict]):
    for issue in issues:
        raw_labels = issue.get("labels", [])
        if raw_labels and isinstance(raw_labels[0], dict):
            labels = ",".join(l.get("name", "") for l in raw_labels)
        else:
            labels = ",".join(str(l) for l in raw_labels)

        db.execute("""
            INSERT INTO issues (repo, number, title, state, labels, created_at, updated_at, url, body, synced_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
            ON CONFLICT(repo, number) DO UPDATE SET
                title=excluded.title, state=excluded.state, labels=excluded.labels,
                updated_at=excluded.updated_at, body=excluded.body, synced_at=excluded.synced_at
        """, (repo, issue["number"], issue["title"], issue.get("state", "OPEN"),
              labels, issue.get("createdAt", ""), issue.get("updatedAt", ""),
              issue.get("url", ""), issue.get("body", "")))


def sync_repo(db: sqlite3.Connection, repo: str, offset: int = 0, count: int = 100):
    print(f"  Syncing {repo} (offset={offset}, count={count})...")
    issues = fetch_issues(repo, offset=offset, count=count)
    _upsert_issues(db, repo, issues)
    db.execute(
        "INSERT INTO sync_log (repo, issue_count, offset_used, count_used) VALUES (?, ?, ?, ?)",
        (repo, len(issues), offset, count),
    )
    db.commit()

    if issues:
        nums = [i["number"] for i in issues]
        print(f"    {len(issues)} issues (#{min(nums)}-#{max(nums)})")
    else:
        print(f"    0 issues")


def cmd_sync(project: str, source_label: str | None, offset: int, count: int = 100):
    """Sync issues from a source repo incrementally into the local DB."""
    config = get_config(project)
    db = get_db(project)

    # Resolve which source to sync
    if source_label:
        repo = None
        for s in config["sources"]:
            if s["label"] == source_label:
                repo = s["repo"]
                break
        if not repo:
            labels = [s["label"] for s in config["sources"]]
            print(f"Unknown source: {source_label}")
            print(f"Available: {', '.join(labels)}")
            sys.exit(1)
        label = source_label
    elif len(config["sources"]) == 1:
        repo = config["sources"][0]["repo"]
        label = config["sources"][0]["label"]
    else:
        labels = [s["label"] for s in config["sources"]]
        print(f"Multiple sources. Specify which to sync: {', '.join(labels)}")
        print(f"  e.g., sync {project} go-sdk:0")
        sys.exit(1)

    print(f"=== Gap Tracker: {project} ===")
    print(f"Syncing: {label} ({repo})")
    print(f"Range: offset={offset}, count={count}")
    print(f"Time: {datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M:%S UTC')}")
    print()

    sync_repo(db, repo, offset=offset, count=count)

    total = db.execute("SELECT COUNT(*) as c FROM issues").fetchone()["c"]
    print(f"\nTotal issues in DB: {total}")


# ---------------------------------------------------------------------------
# Cross-reference
# ---------------------------------------------------------------------------

def _normalize(text: str) -> set[str]:
    text = text.lower()
    stop = {"the", "a", "an", "is", "are", "for", "to", "of", "in", "on", "with",
            "and", "or", "not", "this", "that", "add", "implement", "support", "proposal"}
    words = set(re.findall(r"[a-z][a-z0-9]{2,}", text))
    return words - stop


def _match_score(src_title: str, src_body: str, tgt_title: str, tgt_body: str) -> int:
    src_words = _normalize(src_title + " " + (src_body or "")[:500])
    tgt_words = _normalize(tgt_title + " " + (tgt_body or "")[:500])
    if not src_words or not tgt_words:
        return 0
    overlap = src_words & tgt_words
    title_overlap = _normalize(src_title) & _normalize(tgt_title)
    return len(overlap) * 2 + len(title_overlap) * 5


def cmd_crossref(project: str):
    config = get_config(project)
    db = get_db(project)
    target = config["target_repo"]

    target_issues = db.execute(
        "SELECT number, title, body FROM issues WHERE repo = ?", (target,)
    ).fetchall()

    print(f"=== Cross-reference: {project} ===\n")

    for source in config["sources"]:
        label = source["label"]
        repo = source["repo"]
        src_issues = db.execute(
            "SELECT number, title, body FROM issues WHERE repo = ? AND state = 'OPEN'",
            (repo,),
        ).fetchall()

        print(f"--- {label} ({len(src_issues)} open) → {target} ---")
        matched, unmatched_list = [], []

        for si in src_issues:
            best_score, best_num, best_title = 0, None, ""
            for ti in target_issues:
                score = _match_score(si["title"], si["body"], ti["title"], ti["body"])
                if score > best_score:
                    best_score = score
                    best_num = ti["number"]
                    best_title = ti["title"]

            if best_score >= 15:
                matched.append((si["number"], si["title"], best_num, best_title, best_score))
            else:
                unmatched_list.append(si)

        for sn, st, tn, tt, score in sorted(matched, key=lambda x: -x[4]):
            print(f"  {label}#{sn} → #{tn} (score={score})")
            print(f"    src: {st[:70]}")
            print(f"    tgt: {tt[:70]}")

        if unmatched_list:
            print(f"\n  Unmatched ({len(unmatched_list)}):")
            for si in unmatched_list[:20]:
                print(f"    {label}#{si['number']}: {si['title'][:70]}")
            if len(unmatched_list) > 20:
                print(f"    ... and {len(unmatched_list) - 20} more")
        print()


# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------

def cmd_report(project: str):
    config = get_config(project)
    db = get_db(project)
    target = config["target_repo"]

    print(f"=== Gap Report: {project} ===")
    print(f"Target: {target}\n")

    # Build a lookup: source evidence → gap info
    gaps = db.execute("SELECT * FROM gaps").fetchall()
    # Index by source ref for quick lookup (e.g., "go-sdk:148" → gap row)
    evidence_to_gap = {}
    for g in gaps:
        for ref in (g["source_evidence"] or "").split(","):
            ref = ref.strip()
            if ref:
                evidence_to_gap[ref] = g

    # Report per source
    for source in config["sources"]:
        label = source["label"]
        repo = source["repo"]

        src_issues = db.execute(
            "SELECT number, title, state, labels FROM issues WHERE repo = ? AND state = 'OPEN' ORDER BY number",
            (repo,),
        ).fetchall()

        tracked = []
        untracked = []
        for si in src_issues:
            ref = f"{label}:{si['number']}"
            gap = evidence_to_gap.get(ref)
            if gap:
                tracked.append((si, gap))
            else:
                untracked.append(si)

        print(f"--- {label} ({repo}) — {len(src_issues)} open ---")
        print()

        if tracked:
            print(f"  Tracked ({len(tracked)}):")
            print()
            for si, gap in tracked:
                ours_num = gap["target_issue"]
                ours_str = f"#{ours_num}" if ours_num else "—"
                status_badge = {"open": "[ ]", "in-progress": "[~]", "closed": "[x]", "not-applicable": "[-]"}.get(gap["status"], "[ ]")
                priority = f" {gap['priority']}" if gap["priority"] else ""

                # Source line
                print(f"  {status_badge} {label}#{si['number']}  →  {ours_str}{priority}")
                print(f"       src: {si['title'][:70]}")

                # Our issue details
                if ours_num:
                    our_issue = db.execute(
                        "SELECT title, state FROM issues WHERE repo = ? AND number = ?",
                        (target, ours_num),
                    ).fetchone()
                    if our_issue:
                        print(f"       ours: {our_issue['title'][:70]}  [{our_issue['state']}]")
                print()

        if untracked:
            print(f"  Untracked ({len(untracked)}):")
            for si in untracked:
                print(f"    {label}#{si['number']:<7} {si['title'][:70]}")
            print()

    # Summary: tracked gaps table
    if gaps:
        print(f"--- All tracked gaps ({len(gaps)}) ---")
        print()
        for g in sorted(gaps, key=lambda g: ({"P0":0,"P1":1,"P2":2,"P3":3}.get(g["priority"] or "",4), g["status"])):
            ours_num = g["target_issue"]
            ours_str = f"#{ours_num}" if ours_num else "—"
            status_badge = {"open": "[ ]", "in-progress": "[~]", "closed": "[x]", "not-applicable": "[-]"}.get(g["status"], "[ ]")
            priority = f" {g['priority']}" if g["priority"] else ""

            print(f"  {status_badge} {g['feature']}{priority}  →  {ours_str}  (evidence: {g['source_evidence'] or 'none'})")
            if ours_num:
                our_issue = db.execute(
                    "SELECT title, state FROM issues WHERE repo = ? AND number = ?",
                    (target, ours_num),
                ).fetchone()
                if our_issue:
                    print(f"       {our_issue['title'][:70]}  [{our_issue['state']}]")
            print()
        print()

    # Sync info
    print("--- Last synced ---")
    for row in db.execute(
        "SELECT repo, synced_at, issue_count, offset_used FROM sync_log ORDER BY synced_at DESC LIMIT 8"
    ).fetchall():
        print(f"  {row['repo']:<45} {row['synced_at']}  {row['issue_count']} issues (offset={row['offset_used']})")


# ---------------------------------------------------------------------------
# Manual linking
# ---------------------------------------------------------------------------

def _ensure_target_issue(db: sqlite3.Connection, target_repo: str, number: int):
    """Fetch and cache a target issue if not already in the DB."""
    if number <= 0:
        return
    existing = db.execute(
        "SELECT number FROM issues WHERE repo = ? AND number = ?", (target_repo, number)
    ).fetchone()
    if existing:
        return

    # Fetch from GitHub
    env = _gh_env()
    result = subprocess.run(
        ["gh", "api", f"repos/{target_repo}/issues/{number}"],
        capture_output=True, text=True, env=env,
    )
    if result.returncode != 0:
        return

    try:
        i = json.loads(result.stdout)
    except json.JSONDecodeError:
        return

    labels = ",".join(l["name"] for l in i.get("labels", []))
    db.execute("""
        INSERT INTO issues (repo, number, title, state, labels, created_at, updated_at, url, body, synced_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
        ON CONFLICT(repo, number) DO UPDATE SET
            title=excluded.title, state=excluded.state, labels=excluded.labels,
            updated_at=excluded.updated_at, synced_at=excluded.synced_at
    """, (target_repo, i["number"], i["title"], i["state"].upper(),
          labels, i["created_at"], i["updated_at"], i["html_url"],
          (i.get("body") or "")[:2000]))
    db.commit()


def cmd_link(project: str, feature: str, target_issue: int, source_evidence: str = ""):
    config = get_config(project)
    db = get_db(project)

    # Check for duplicate source evidence in existing gaps
    for ref in (source_evidence or "").split(","):
        ref = ref.strip()
        if not ref:
            continue
        existing = db.execute(
            "SELECT feature, target_issue FROM gaps WHERE source_evidence LIKE ? AND feature != ?",
            (f"%{ref}%", feature),
        ).fetchone()
        if existing:
            print(f"WARNING: {ref} is already linked to gap '{existing['feature']}' → #{existing['target_issue']}")
            print(f"  Use a different feature name or update the existing gap instead.")
            return

    # Cache the target issue so report/show can display its title
    _ensure_target_issue(db, config["target_repo"], target_issue)

    db.execute("""
        INSERT INTO gaps (feature, target_issue, source_evidence, updated_at)
        VALUES (?, ?, ?, datetime('now'))
        ON CONFLICT(feature) DO UPDATE SET
            target_issue=excluded.target_issue,
            source_evidence=excluded.source_evidence,
            updated_at=datetime('now')
    """, (feature, target_issue, source_evidence))
    db.commit()
    print(f"Linked: {feature} → #{target_issue} (evidence: {source_evidence or 'none'})")


def cmd_show(project: str, issue_ref: str):
    """Show details for an issue. Accepts '#72', '72', 'go-sdk:148', or 'spec:430'."""
    config = get_config(project)
    db = get_db(project)

    # Parse ref: "go-sdk:148" → (repo, 148)
    if ":" not in issue_ref:
        labels = [s["label"] for s in config["sources"]]
        print(f"Specify source: <label>:<number>  (e.g., go-sdk:148)")
        print(f"Available labels: {', '.join(labels)}")
        sys.exit(1)

    label, num_str = issue_ref.split(":", 1)
    repo = None
    for s in config["sources"]:
        if s["label"] == label:
            repo = s["repo"]
            break
    if not repo:
        print(f"Unknown source label: {label}")
        print(f"Available: {', '.join(s['label'] for s in config['sources'])}")
        sys.exit(1)
    number = int(num_str)

    row = db.execute(
        "SELECT * FROM issues WHERE repo = ? AND number = ?", (repo, number)
    ).fetchone()

    if not row:
        print(f"Issue {repo}#{number} not found in DB. Try syncing first.")
        sys.exit(1)

    print(f"{'─' * 80}")
    print(f"  {repo}#{row['number']}  [{row['state']}]")
    print(f"  {row['title']}")
    if row["labels"]:
        print(f"  Labels: {row['labels']}")
    print(f"  URL: {row['url']}")
    print(f"  Created: {row['created_at']}  Updated: {row['updated_at']}")

    # Show linked gap if this source issue is tracked
    ref = f"{label}:{number}"
    gap = db.execute(
        "SELECT * FROM gaps WHERE source_evidence LIKE ?", (f"%{ref}%",)
    ).fetchone()
    if gap:
        target = config["target_repo"]
        ours_str = f"#{gap['target_issue']}" if gap["target_issue"] else "none"
        print(f"  ┌─ Linked gap: {gap['feature']}  →  {target}{ours_str}  [{gap['status']}]")
        if gap["target_issue"]:
            # Show our issue title if we have it in the DB
            our_issue = db.execute(
                "SELECT title, state FROM issues WHERE repo = ? AND number = ?",
                (target, gap["target_issue"]),
            ).fetchone()
            if our_issue:
                print(f"  │  {our_issue['title']}  [{our_issue['state']}]")
        if gap["priority"]:
            print(f"  │  Priority: {gap['priority']}")
        print(f"  └─")
    else:
        print(f"  ⚠ Not linked to any gap (use: link {project} \"<feature>\" <issue#> \"{ref}\")")

    print(f"{'─' * 80}")
    if row["body"]:
        print(row["body"])
    else:
        print("  (no body)")
    print()


def cmd_mark(project: str, feature: str, status: str):
    db = get_db(project)
    db.execute("UPDATE gaps SET status = ?, updated_at = datetime('now') WHERE feature = ?", (status, feature))
    db.commit()
    print(f"Marked: {feature} → {status}")


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def _parse_int_flag(args: list[str], flag: str, default: int) -> int:
    if flag in args:
        idx = args.index(flag)
        if idx + 1 < len(args):
            return int(args[idx + 1])
    return default


def main():
    if len(sys.argv) < 3:
        print(__doc__)
        sys.exit(1)

    cmd, project = sys.argv[1], sys.argv[2]
    extra = sys.argv[3:]

    match cmd:
        case "sync":
            # sync <project> <ref:offset> [--count N]
            # e.g., sync mcpkit go-sdk:0     → sync go-sdk from offset 0
            #        sync mcpkit target:0     → sync target repo from offset 0
            #        sync mcpkit 0            → single source only, else error
            if not extra:
                print("usage: sync <project> <ref:offset> [--count N]")
                print("  e.g., sync mcpkit go-sdk:0")
                print("        sync mcpkit target:0")
                sys.exit(1)
            ref_offset = extra[0]
            if ":" in ref_offset:
                source_label, offset_str = ref_offset.rsplit(":", 1)
                offset = int(offset_str)
            else:
                source_label = None
                offset = int(ref_offset)
            count = _parse_int_flag(extra[1:], "--count", 100)
            cmd_sync(project, source_label, offset=offset, count=count)
        case "report":
            cmd_report(project)
        case "crossref":
            cmd_crossref(project)
        case "link":
            if len(extra) < 2:
                print("usage: link <project> <feature> <target_issue#> [source_evidence]")
                sys.exit(1)
            cmd_link(project, extra[0], int(extra[1]), extra[2] if len(extra) > 2 else "")
        case "show":
            if not extra:
                print("usage: show <project> <issue_ref>  (e.g., '72', '#72', 'go-sdk:148', 'spec:430')")
                sys.exit(1)
            cmd_show(project, extra[0])
        case "mark":
            if len(extra) < 2:
                print("usage: mark <project> <feature> <status>")
                sys.exit(1)
            cmd_mark(project, extra[0], extra[1])
        case _:
            print(f"Unknown command: {cmd}")
            sys.exit(1)


if __name__ == "__main__":
    main()
