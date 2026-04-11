#!/usr/bin/env python3
"""
gap-tracker: Track feature gaps between public reference repos and a local project.

Supports many-to-one: multiple source repos feeding into a single target repo.
Source of truth is the SQLite DB (`<project>/gaps.db`). YAML snapshots are
export-on-demand via `triage export`. See `<project>/docs/STATE_MACHINE.md`.

Sync:
    python main.py sync <project> <ref:offset> [--count N]    # one source or target
    python main.py sync-all <project> [--count N]             # all sources + target

Triage (DB-primary):
    python main.py triage <project> status
    python main.py triage <project> list [--bucket X] [--source X]
    python main.py triage <project> set <ref> <bucket> [target] "<rationale>" [--test path::Name]
    python main.py triage <project> unset <ref>
    python main.py triage <project> close <ref> [target]      # real-gap → done
    python main.py triage <project> export [--out path]       # YAML snapshot
    python main.py triage <project> import <path>             # bulk load YAML
    python main.py triage <project> verify                    # drift check (stub)

Per-issue:
    python main.py show <project> <issue_ref>
    python main.py note <project> <issue_ref> "<text>" [--set]
    python main.py notes <project> [filter]

Legacy (pre-YAML gap workflow, still works):
    python main.py link <project> <feature> <target#> [source_evidence]
    python main.py mark <project> <feature> <status>
    python main.py report <project> [--by-priority]
    python main.py crossref <project>
    python main.py reconcile <project>

Examples:
    python main.py sync-all mcpkit
    python main.py triage mcpkit status
    python main.py triage mcpkit set ext-apps:298 real-gap mcpkit#190 "No template support"
    python main.py triage mcpkit list --bucket real-gap
    python main.py triage mcpkit close ext-apps:298
    python main.py triage mcpkit export --out mcpkit/triage.snapshot.yaml
"""

import json
import os
import re
import sqlite3
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

import yaml
from rich.console import Console
from rich.table import Table
from rich.text import Text

console = Console()

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
            notes       TEXT DEFAULT '[]',
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
            notes           TEXT DEFAULT '[]',
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

        CREATE TABLE IF NOT EXISTS triage (
            repo          TEXT NOT NULL,
            number        INTEGER NOT NULL,
            bucket        TEXT NOT NULL,
            target        TEXT DEFAULT '',
            rationale     TEXT DEFAULT '',
            test          TEXT DEFAULT '',
            classified_at TEXT NOT NULL DEFAULT (datetime('now')),
            updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
            PRIMARY KEY (repo, number),
            FOREIGN KEY (repo, number) REFERENCES issues(repo, number)
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
            /* notes column is NOT overwritten by sync — it's manually curated */
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
    if source_label == "target":
        repo = config["target_repo"]
        label = "target"
    elif source_label:
        repo = None
        for s in config["sources"]:
            if s["label"] == source_label:
                repo = s["repo"]
                break
        if not repo:
            labels = [s["label"] for s in config["sources"]] + ["target"]
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


def cmd_sync_all(project: str, count: int = 100):
    """Sync all sources + target, each from offset 0. Convenience wrapper."""
    config = get_config(project)
    db = get_db(project)

    print(f"=== Gap Tracker: {project} — sync-all ===")
    print(f"Time: {datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M:%S UTC')}")
    print()

    targets: list[tuple[str, str]] = [(s["label"], s["repo"]) for s in config["sources"]]
    targets.append(("target", config["target_repo"]))

    for label, repo in targets:
        print(f"[{label}] {repo}")
        sync_repo(db, repo, offset=0, count=count)
    db.commit()

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

def _label_priority_score(labels_str: str) -> int:
    """Return a sort score for an issue based on its labels (lower = higher priority).

    Label tiers:
      0 — bug, blocked, breaking change, security
      1 — ready for work, P0, P1
      2 — P2, enhancement, SEP (spec enhancement proposal)
      3 — P3, help wanted
      4 — needs investigation, needs confirmation
      5 — documentation, question, needs community feedback
      9 — (no labels / unrecognized)
    """
    labels = {l.strip().lower() for l in labels_str.split(",") if l.strip()} if labels_str else set()
    if not labels:
        return 9
    # Check tiers from highest to lowest — return first match
    if labels & {"bug", "blocked", "breaking change", "security"}:
        return 0
    if labels & {"ready for work", "p0", "p1"}:
        return 1
    if labels & {"p2", "enhancement", "sep", "sep-requested", "in-review"}:
        return 2
    if labels & {"p3", "help wanted", "external"}:
        return 3
    if labels & {"needs investigation", "needs confirmation"}:
        return 4
    if labels & {"documentation", "question", "needs community feedback", "awaiting-reply"}:
        return 5
    return 6


_STATUS_BADGE = {"open": "[ ]", "in-progress": "[~]", "closed": "[x]", "not-applicable": "[-]", "deferred": "[>]"}
_STATUS_STYLE = {"open": "bold white", "in-progress": "yellow", "closed": "green", "not-applicable": "dim", "deferred": "cyan"}
_STATUS_SORT = {"open": 0, "in-progress": 1, "deferred": 2, "closed": 3, "not-applicable": 4}


def cmd_report(project: str, by_priority: bool = False):
    config = get_config(project)
    db = get_db(project)
    target = config["target_repo"]

    console.print(f"\n[bold]Gap Report: {project}[/bold]  (target: {target})\n")

    # Build a lookup: source evidence → gap info
    gaps = db.execute("SELECT * FROM gaps").fetchall()
    evidence_to_gap = {}
    for g in gaps:
        for ref in (g["source_evidence"] or "").split(","):
            ref = ref.strip()
            if ref:
                evidence_to_gap[ref] = g

    # Helper: look up our issue title
    def _our_issue(num):
        if not num:
            return None
        return db.execute(
            "SELECT title, state FROM issues WHERE repo = ? AND number = ?",
            (target, num),
        ).fetchone()

    # ── Per-source sections ──────────────────────────────────────────────
    for source in config["sources"]:
        label = source["label"]
        repo = source["repo"]

        src_issues = db.execute(
            "SELECT number, title, state, labels, updated_at FROM issues WHERE repo = ? AND state = 'OPEN' ORDER BY number",
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

        if by_priority:
            untracked.sort(key=lambda si: (_label_priority_score(si["labels"]), -(si["number"])))

        console.rule(f"[bold]{label}[/bold] ({repo}) — {len(src_issues)} open")

        # Tracked table
        if tracked:
            t = Table(title=f"Tracked ({len(tracked)})", show_header=True, title_style="bold",
                      padding=(0, 1), show_edge=False, expand=True)
            t.add_column("", width=3, no_wrap=True)          # status badge
            t.add_column("Source", ratio=1, no_wrap=True)
            t.add_column("Source Title", ratio=3)
            t.add_column("Ours", ratio=1, no_wrap=True)
            t.add_column("Our Title", ratio=3)
            for si, gap in tracked:
                status = gap["status"]
                badge = Text(_STATUS_BADGE.get(status, "[ ]"), style=_STATUS_STYLE.get(status, ""))
                src_ref = f"{label}#{si['number']}"
                ours_num = gap["target_issue"]
                ours_ref = f"#{ours_num}" if ours_num else "—"
                oi = _our_issue(ours_num)
                ours_title = oi["title"][:60] if oi else ""
                t.add_row(badge, src_ref, si["title"][:60], ours_ref, ours_title)
            console.print(t)
            console.print()

        # Untracked table
        if untracked:
            t = Table(title=f"Untracked ({len(untracked)})", show_header=True, title_style="bold",
                      padding=(0, 1), show_edge=False, expand=True)
            t.add_column("Ref", no_wrap=True)
            t.add_column("Title", ratio=4)
            t.add_column("Labels", ratio=1, style="dim")
            for si in untracked:
                t.add_row(
                    f"{label}#{si['number']}",
                    si["title"][:70],
                    si["labels"] or "",
                )
            console.print(t)
            console.print()

    # ── All tracked gaps summary ─────────────────────────────────────────
    if gaps:
        # Separate by status for grouped display
        open_gaps = [g for g in gaps if g["status"] in ("open", "in-progress")]
        deferred_gaps = [g for g in gaps if g["status"] == "deferred"]
        closed_gaps = [g for g in gaps if g["status"] in ("closed", "not-applicable")]

        def _gap_sort_key(g):
            return (
                {"P0": 0, "P1": 1, "P2": 2, "P3": 3}.get(g["priority"] or "", 4),
                _STATUS_SORT.get(g["status"], 9),
                g["feature"],
            )

        def _add_gap_rows(table, gap_list):
            for g in sorted(gap_list, key=_gap_sort_key):
                status = g["status"]
                badge = Text(_STATUS_BADGE.get(status, "[ ]"), style=_STATUS_STYLE.get(status, ""))
                ours_num = g["target_issue"]
                ours_ref = f"#{ours_num}" if ours_num else "—"
                oi = _our_issue(ours_num)
                ours_title = oi["title"][:55] if oi else ""
                pri = g["priority"] or ""
                table.add_row(badge, g["source_evidence"] or "", ours_ref, ours_title, pri)

        # Open/in-progress gaps
        if open_gaps:
            t = Table(title=f"Open gaps ({len(open_gaps)})", show_header=True, title_style="bold green",
                      padding=(0, 1), show_edge=False, expand=True)
            t.add_column("", width=3, no_wrap=True)
            t.add_column("Evidence", ratio=1)
            t.add_column("Ours", no_wrap=True)
            t.add_column("Title", ratio=3)
            t.add_column("Pri", width=3, no_wrap=True)
            _add_gap_rows(t, open_gaps)
            console.print()
            console.rule("[bold]All Tracked Gaps[/bold]")
            console.print(t)

        # Deferred gaps — formatted for easy copy-paste back into /gap-track
        if deferred_gaps:
            t = Table(title=f"Deferred ({len(deferred_gaps)})", show_header=True, title_style="bold cyan",
                      padding=(0, 1), show_edge=False, expand=True)
            t.add_column("", width=3, no_wrap=True)
            t.add_column("Evidence", ratio=1)
            t.add_column("Gap Name", ratio=2)
            _add_deferred_rows(t, deferred_gaps)
            console.print(t)

            # Plain-text block for copy-paste into /gap-track (raw print to avoid wrapping)
            console.print()
            console.print("[dim]Copy-paste block for re-triage:[/dim]")
            for line in _deferred_copy_block(db, config, deferred_gaps):
                print(line)

        # Closed/n-a gaps
        if closed_gaps:
            t = Table(title=f"Closed / N/A ({len(closed_gaps)})", show_header=True, title_style="dim",
                      padding=(0, 1), show_edge=False, expand=True)
            t.add_column("", width=3, no_wrap=True)
            t.add_column("Evidence", ratio=1)
            t.add_column("Ours", no_wrap=True)
            t.add_column("Title", ratio=3)
            t.add_column("Pri", width=3, no_wrap=True)
            _add_gap_rows(t, closed_gaps)
            console.print(t)

        console.print()

    # ── Sync info ────────────────────────────────────────────────────────
    sync_rows = db.execute(
        "SELECT repo, synced_at, issue_count, offset_used FROM sync_log ORDER BY synced_at DESC LIMIT 8"
    ).fetchall()
    if sync_rows:
        t = Table(title="Last synced", show_header=True, title_style="dim", padding=(0, 1), show_edge=False)
        t.add_column("Repo", ratio=2)
        t.add_column("Synced", ratio=1)
        t.add_column("Count", justify="right")
        t.add_column("Offset", justify="right")
        for row in sync_rows:
            t.add_row(row["repo"], row["synced_at"], str(row["issue_count"]), str(row["offset_used"]))
        console.print(t)


def _add_deferred_rows(table, gap_list):
    for g in sorted(gap_list, key=lambda g: g["feature"]):
        badge = Text("[>]", style="cyan")
        table.add_row(badge, g["source_evidence"] or "", g["feature"])


def _deferred_copy_block(db, config, deferred_gaps) -> list[str]:
    """Build plain-text lines for deferred gaps, suitable for pasting into /gap-track."""
    lines = []
    for g in sorted(deferred_gaps, key=lambda g: g["source_evidence"] or ""):
        for ref in (g["source_evidence"] or "").split(","):
            ref = ref.strip()
            if not ref:
                continue
            parts = ref.split(":")
            if len(parts) == 2:
                src_label, src_num = parts
                src_repo = next((s["repo"] for s in config["sources"] if s["label"] == src_label), None)
                if src_repo:
                    si = db.execute(
                        "SELECT title FROM issues WHERE repo = ? AND number = ?",
                        (src_repo, int(src_num)),
                    ).fetchone()
                    title = si["title"][:70] if si else g["feature"]
                else:
                    title = g["feature"]
            else:
                title = g["feature"]
            lines.append(f"    {ref:<12} {title[:70]}")
    return lines


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

    # Show triage classification (authoritative — from triage table)
    t = db.execute(
        "SELECT bucket, target, rationale, test, classified_at, updated_at "
        "FROM triage WHERE repo = ? AND number = ?",
        (repo, number),
    ).fetchone()
    if t:
        print(f"  ┌─ Triage: {t['bucket']}"
              + (f"  →  {t['target']}" if t["target"] else "")
              + f"  (classified {t['classified_at'][:10]}"
              + (f", updated {t['updated_at'][:10]}" if t['updated_at'] != t['classified_at'] else "")
              + ")")
        if t["test"]:
            print(f"  │  test: {t['test']}")
        if t["rationale"]:
            # Wrap long rationales
            rationale = t["rationale"]
            print(f"  │  rationale: {rationale[:70]}")
            for i in range(70, len(rationale), 70):
                print(f"  │             {rationale[i:i+70]}")
        print(f"  └─")

    # Show ad-hoc notes (excluding the legacy [triage:...] entries now
    # superseded by the triage table)
    all_notes = _load_notes(row["notes"])
    notes = [n for n in all_notes
             if not (isinstance(n, dict) and n.get("text", "").startswith("[triage:"))]
    if notes:
        print(f"  Notes ({len(notes)}):")
        for n in notes:
            at_str = f" ({n['at']})" if n.get("at") else ""
            print(f"    • {n['text']}{at_str}")

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


def cmd_reconcile(project: str):
    """Sync target repo issues, then update gap statuses to match."""
    config = get_config(project)
    db = get_db(project)
    target_repo = config["target_repo"]

    # Step 1: sync target repo to get fresh issue states
    console.rule(f"[bold]Syncing target repo: {target_repo}[/bold]")
    sync_repo(db, target_repo, offset=0, count=1000)
    print()

    # Step 2: find all gaps that could change
    gaps = db.execute("""
        SELECT g.feature, g.target_issue, g.status, g.notes,
               i.state AS issue_state, i.title AS issue_title
        FROM gaps g
        LEFT JOIN issues i ON i.repo = ? AND i.number = g.target_issue
        WHERE g.target_issue > 0
          AND g.status IN ('open', 'in-progress')
    """, (target_repo,)).fetchall()

    if not gaps:
        console.print("[dim]No open/in-progress gaps with linked issues.[/dim]")
        return

    # Step 3: reconcile
    changed = []
    unchanged = []
    missing = []

    for gap in gaps:
        feature = gap["feature"]
        issue_num = gap["target_issue"]
        issue_state = gap["issue_state"]
        old_status = gap["status"]

        if issue_state is None:
            # Issue not in our DB — may not have been synced yet
            missing.append((feature, issue_num))
            continue

        if issue_state == "CLOSED":
            # Update gap to closed
            now = datetime.now(timezone.utc).strftime("%Y-%m-%d")
            db.execute(
                "UPDATE gaps SET status = 'closed', updated_at = datetime('now') WHERE feature = ?",
                (feature,),
            )
            # Append a note
            notes = _load_notes(gap["notes"])
            notes.append({
                "text": f"[reconcile] #{issue_num} closed — gap resolved ({now})",
                "at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            })
            db.execute("UPDATE gaps SET notes = ? WHERE feature = ?", (json.dumps(notes), feature))
            changed.append((feature, issue_num, old_status, gap["issue_title"]))
        else:
            unchanged.append((feature, issue_num, old_status))

    db.commit()

    # Step 4: report
    if changed:
        t = Table(title=f"Closed ({len(changed)})", show_header=True, title_style="bold green",
                  padding=(0, 1), expand=True)
        t.add_column("Feature", style="bold")
        t.add_column("Issue", width=6)
        t.add_column("Was", width=12)
        t.add_column("Issue Title")
        for feature, num, old, title in changed:
            t.add_row(feature, f"#{num}", old, title or "")
        console.print(t)
        print()

    if unchanged:
        console.print(f"[dim]Unchanged: {len(unchanged)} gaps still open[/dim]")

    if missing:
        console.print(f"[yellow]Warning: {len(missing)} gaps reference issues not found in DB:[/yellow]")
        for feature, num in missing:
            console.print(f"  {feature} → #{num}")

    console.print(f"\n[bold]Reconciled: {len(changed)} closed, {len(unchanged)} unchanged, {len(missing)} missing[/bold]")


def cmd_mark(project: str, feature: str, status: str):
    db = get_db(project)
    db.execute("UPDATE gaps SET status = ?, updated_at = datetime('now') WHERE feature = ?", (status, feature))
    db.commit()
    print(f"Marked: {feature} → {status}")


def _resolve_issue_ref(config: dict, issue_ref: str) -> tuple[str, int]:
    """Parse '<number>' (target) or '<label>:<number>' (source) into (repo, number)."""
    if ":" in issue_ref:
        label, num_str = issue_ref.split(":", 1)
        repo = next((s["repo"] for s in config["sources"] if s["label"] == label), None)
        if not repo:
            print(f"Unknown source label: {label}")
            sys.exit(1)
    else:
        repo = config["target_repo"]
        num_str = issue_ref.lstrip("#")
    return repo, int(num_str)


def _load_notes(raw: str) -> list[dict]:
    """Parse notes JSON, returning [] on empty/invalid."""
    if not raw or raw == "[]":
        return []
    try:
        parsed = json.loads(raw)
        if isinstance(parsed, list):
            return parsed
    except (json.JSONDecodeError, TypeError):
        pass
    return [{"text": raw, "at": ""}]


def _save_notes(db: sqlite3.Connection, table: str, notes: list[dict], where_clause: str, where_params: tuple):
    db.execute(f"UPDATE {table} SET notes = ? WHERE {where_clause}", (json.dumps(notes), *where_params))
    db.commit()


def cmd_note(project: str, issue_ref: str, note_text: str, overwrite: bool = False):
    """Add a note to an issue. Appends by default, overwrites with --set."""
    config = get_config(project)
    db = get_db(project)
    repo, number = _resolve_issue_ref(config, issue_ref)

    existing = db.execute(
        "SELECT number, title, notes FROM issues WHERE repo = ? AND number = ?",
        (repo, number),
    ).fetchone()
    if not existing:
        print(f"Issue {repo}#{number} not found in DB. Sync first.")
        sys.exit(1)

    entry = {
        "text": note_text,
        "at": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
    }

    if overwrite:
        notes = [entry]
    else:
        notes = _load_notes(existing["notes"])
        notes.append(entry)

    _save_notes(db, "issues", notes, "repo = ? AND number = ?", (repo, number))
    verb = "Set" if overwrite else "Added"
    print(f"{verb} note on {repo}#{number}: {note_text}")


def cmd_notes(project: str, filter_text: str = ""):
    """List issues that have notes, optionally filtered by substring (e.g. 'route:servicekit')."""
    config = get_config(project)
    db = get_db(project)

    if filter_text:
        rows = db.execute(
            "SELECT repo, number, title, state, notes FROM issues WHERE notes LIKE ? AND notes != '[]' AND notes != '' ORDER BY repo, number",
            (f"%{filter_text}%",),
        ).fetchall()
    else:
        rows = db.execute(
            "SELECT repo, number, title, state, notes FROM issues WHERE notes != '[]' AND notes != '' ORDER BY repo, number"
        ).fetchall()

    if not rows:
        print(f"No issues with notes{f' matching \"{filter_text}\"' if filter_text else ''}.")
        return

    t = Table(title=f"Issues with notes{f' (filter: {filter_text})' if filter_text else ''}", show_header=True,
              padding=(0, 1), show_edge=False, expand=True)
    t.add_column("#", no_wrap=True)
    t.add_column("State", width=6, no_wrap=True)
    t.add_column("Title", ratio=3)
    t.add_column("Notes", ratio=2, style="cyan")

    target = config["target_repo"]
    for row in rows:
        repo = row["repo"]
        if repo == target:
            ref = f"#{row['number']}"
        else:
            label = next((s["label"] for s in config["sources"] if s["repo"] == repo), repo)
            ref = f"{label}:{row['number']}"
        state_style = "green" if row["state"] == "CLOSED" else "bold"
        notes = _load_notes(row["notes"])
        # Compact display: show count + most recent entry
        if notes:
            latest = notes[-1]
            summary = latest["text"][:60]
            if len(notes) > 1:
                summary = f"({len(notes)}) {summary}"
        else:
            summary = ""
        t.add_row(ref, Text(row["state"], style=state_style), row["title"][:60], summary)

    console.print(t)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Triage: DB-backed classification of source issues into buckets
#
# Source of truth: the `triage` table in gaps.db. YAML is export-on-demand only.
# See docs/STATE_MACHINE.md for the full flow model.
# ---------------------------------------------------------------------------

TRIAGE_BUCKETS = {
    "real-gap",
    "already-covered",
    "defer-spec-flux",
    "defer-scope",
    "not-applicable",
    "done",
}

BUCKET_ORDER = [
    "real-gap", "already-covered", "defer-spec-flux",
    "defer-scope", "not-applicable", "done",
]

TRIAGE_SNAPSHOT_HEADER = """# GENERATED — DO NOT EDIT
#
# Snapshot of the triage table in gaps.db for project: {project}
# Exported: {exported_at}
#
# This file is a point-in-time export. Edits here are NOT authoritative —
# run `uv run python main.py triage {project} import <file>` to bulk-load a
# YAML back into the DB (rare reverse path).
#
# Buckets:
#   real-gap         — failing repro test exists; GH issue filed; track until fixed.
#   already-covered  — passing verification test exists; no new issue needed.
#   defer-spec-flux  — upstream spec still moving; re-review on daily sync.
#   defer-scope      — host-side / out-of-scope for our libraries; unlikely to implement.
#   not-applicable   — truly unrelated (other languages, end-user bugs, example fixes).
#   done             — previously a real-gap, now fixed and closed.

"""


def _source_label_to_repo(config: dict, label: str) -> str | None:
    for s in config["sources"]:
        if s["label"] == label:
            return s["repo"]
    if label == "target":
        return config["target_repo"]
    return None


def _repo_to_source_label(config: dict, repo: str) -> str:
    if repo == config.get("target_repo"):
        return "target"
    for s in config["sources"]:
        if s["repo"] == repo:
            return s["label"]
    return repo  # fall back to full repo name


def _parse_issue_ref(config: dict, issue_ref: str) -> tuple[str, str, int]:
    """Parse '<label>:<number>' → (label, repo, number). Exits on error."""
    if ":" not in issue_ref:
        print(f"issue_ref must be in form '<source>:<number>' (e.g. 'ext-apps:48'), got {issue_ref!r}")
        sys.exit(1)
    label, num_str = issue_ref.rsplit(":", 1)
    try:
        number = int(num_str)
    except ValueError:
        print(f"Invalid issue number: {num_str!r}")
        sys.exit(1)
    repo = _source_label_to_repo(config, label)
    if not repo:
        available = ", ".join(s["label"] for s in config["sources"]) + ", target"
        print(f"Unknown source label '{label}'. Available: {available}")
        sys.exit(1)
    return label, repo, number


def cmd_triage_set(project: str, issue_ref: str, bucket: str,
                   target: str = "", rationale: str = "", test: str = ""):
    """Upsert a classification. Writes directly to the triage table."""
    if bucket not in TRIAGE_BUCKETS:
        print(f"Invalid bucket '{bucket}'. Must be one of: {', '.join(sorted(TRIAGE_BUCKETS))}")
        sys.exit(1)
    if not rationale:
        print("Rationale is required. Explain why this bucket applies.")
        sys.exit(1)

    config = get_config(project)
    db = get_db(project)
    label, repo, number = _parse_issue_ref(config, issue_ref)

    exists = db.execute(
        "SELECT 1 FROM issues WHERE repo = ? AND number = ?", (repo, number)
    ).fetchone()
    if not exists:
        print(f"Issue {label}:{number} not in DB. Run sync first.")
        sys.exit(1)

    db.execute("""
        INSERT INTO triage (repo, number, bucket, target, rationale, test, classified_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
        ON CONFLICT(repo, number) DO UPDATE SET
            bucket = excluded.bucket,
            target = excluded.target,
            rationale = excluded.rationale,
            test = CASE WHEN excluded.test != '' THEN excluded.test ELSE triage.test END,
            updated_at = datetime('now')
    """, (repo, number, bucket, target or "", rationale, test or ""))
    db.commit()

    extras = []
    if target:
        extras.append(f"target: {target}")
    if test:
        extras.append(f"test: {test}")
    suffix = f" ({', '.join(extras)})" if extras else ""
    print(f"Set {label}:{number} → {bucket}{suffix}")


def cmd_triage_unset(project: str, issue_ref: str):
    """Remove a classification. Deletes the triage row."""
    config = get_config(project)
    db = get_db(project)
    label, repo, number = _parse_issue_ref(config, issue_ref)

    row = db.execute(
        "SELECT 1 FROM triage WHERE repo = ? AND number = ?", (repo, number)
    ).fetchone()
    if not row:
        print(f"{label}:{number} not classified; nothing to unset.")
        return

    db.execute("DELETE FROM triage WHERE repo = ? AND number = ?", (repo, number))
    db.commit()
    print(f"Unset {label}:{number}")


def cmd_triage_close(project: str, issue_ref: str, target_override: str = ""):
    """Flip a real-gap/already-covered row to 'done'. Requires the target mcpkit
    issue to be CLOSED in the DB — refuses otherwise to keep the classification
    honest. Run `sync <project> target:0` first if the target was just closed.
    """
    config = get_config(project)
    db = get_db(project)
    label, repo, number = _parse_issue_ref(config, issue_ref)

    row = db.execute(
        "SELECT bucket, target, rationale, test FROM triage WHERE repo = ? AND number = ?",
        (repo, number),
    ).fetchone()
    if not row:
        print(f"{label}:{number} is not classified. Use `triage set` first.")
        sys.exit(1)

    # Determine which target issue to verify
    target = target_override or row["target"] or ""
    if not target:
        print(f"{label}:{number} has no target issue recorded. "
              f"Pass one explicitly: triage close {project} {issue_ref} <target>")
        sys.exit(1)

    # Parse target like "mcpkit#190" or "panyam/mcpkit#190"
    if "#" not in target:
        print(f"Target {target!r} must be in form '<repo>#<number>' or '<shortname>#<number>'")
        sys.exit(1)
    target_repo_part, target_num_str = target.rsplit("#", 1)
    try:
        target_num = int(target_num_str)
    except ValueError:
        print(f"Invalid target issue number: {target_num_str!r}")
        sys.exit(1)

    # Resolve short name ("mcpkit" → "panyam/mcpkit") via config
    target_repo_full = config["target_repo"]
    if "/" not in target_repo_part:
        # short form — verify it matches the configured target
        if not target_repo_full.endswith("/" + target_repo_part):
            print(f"Target {target_repo_part!r} doesn't match configured target_repo {target_repo_full!r}")
            sys.exit(1)
    else:
        target_repo_full = target_repo_part

    # Look up the target issue state
    target_row = db.execute(
        "SELECT state, title FROM issues WHERE repo = ? AND number = ?",
        (target_repo_full, target_num),
    ).fetchone()
    if not target_row:
        print(f"Target {target_repo_full}#{target_num} not in DB. "
              f"Run `sync {project} target:0` to refresh target state.")
        sys.exit(1)
    if target_row["state"] != "CLOSED":
        print(f"Refusing to close: target {target_repo_full}#{target_num} "
              f"is still {target_row['state']!r}. Fix the gap first, or reclassify manually.")
        sys.exit(1)

    # Flip bucket, append a note
    new_rationale = (row["rationale"] or "") + \
        f" [closed via triage close on {datetime.now(timezone.utc).strftime('%Y-%m-%d')} — target {target_repo_full}#{target_num} is CLOSED]"
    db.execute("""
        UPDATE triage SET bucket = 'done', target = ?, rationale = ?, updated_at = datetime('now')
        WHERE repo = ? AND number = ?
    """, (f"mcpkit#{target_num}" if target_repo_full == "panyam/mcpkit" else target, new_rationale, repo, number))
    db.commit()
    print(f"Closed {label}:{number} → done (target {target_repo_full}#{target_num} CLOSED)")


def cmd_triage_list(project: str, bucket: str | None = None, source: str | None = None):
    """Show classifications grouped by bucket, filtered by optional bucket/source."""
    config = get_config(project)
    db = get_db(project)

    if bucket and bucket not in TRIAGE_BUCKETS:
        print(f"Invalid bucket filter '{bucket}'.")
        sys.exit(1)

    # Optional source filter → repo
    repo_filter = None
    if source:
        repo_filter = _source_label_to_repo(config, source)
        if not repo_filter:
            print(f"Unknown source '{source}'.")
            sys.exit(1)

    query = """
        SELECT t.repo, t.number, t.bucket, t.target, t.rationale, t.test,
               t.updated_at, i.title, i.state
        FROM triage t
        LEFT JOIN issues i ON i.repo = t.repo AND i.number = t.number
        WHERE 1=1
    """
    params: list = []
    if bucket:
        query += " AND t.bucket = ?"
        params.append(bucket)
    if repo_filter:
        query += " AND t.repo = ?"
        params.append(repo_filter)
    query += " ORDER BY t.bucket, t.repo, t.number"

    rows = db.execute(query, params).fetchall()
    if not rows:
        print("No classifications match the filter.")
        return

    from collections import defaultdict
    by_bucket: dict[str, list] = defaultdict(list)
    for r in rows:
        by_bucket[r["bucket"]].append(r)

    for b in BUCKET_ORDER:
        items = by_bucket.get(b, [])
        if not items:
            continue
        t = Table(title=f"{b} ({len(items)})", show_header=True, padding=(0, 1),
                  show_edge=False, expand=True)
        t.add_column("Ref", no_wrap=True, width=16)
        t.add_column("State", width=6, no_wrap=True)
        t.add_column("Title", ratio=2)
        t.add_column("Target", no_wrap=True, width=14)
        t.add_column("Rationale", ratio=3, style="cyan")
        for r in items:
            label = _repo_to_source_label(config, r["repo"])
            state = r["state"] or "?"
            state_style = "green" if state == "CLOSED" else "bold"
            target = r["target"] or "—"
            title = (r["title"] or "(not in DB)")[:60]
            rationale = (r["rationale"] or "")[:120]
            t.add_row(
                f"{label}:{r['number']}",
                Text(state, style=state_style),
                title,
                target,
                rationale,
            )
        console.print(t)
        console.print()


def cmd_triage_status(project: str):
    """Show bucket counts + any open issues that have no triage entry (per source)."""
    config = get_config(project)
    db = get_db(project)

    summary = Table(title=f"Triage status: {project}", show_header=True, padding=(0, 1),
                    show_edge=False)
    summary.add_column("Source", no_wrap=True)
    summary.add_column("Open", justify="right", width=6)
    summary.add_column("Classified", justify="right", width=10)
    summary.add_column("Unclassified", justify="right", width=12, style="yellow")
    for b in BUCKET_ORDER:
        summary.add_column(b, justify="right", width=max(6, len(b)))

    unclassified_by_source: dict[str, list] = {}
    for s in config["sources"]:
        label = s["label"]
        repo = s["repo"]
        open_rows = db.execute(
            "SELECT number, title FROM issues WHERE repo = ? AND state = 'OPEN' ORDER BY number",
            (repo,),
        ).fetchall()

        # Bucket breakdown from triage table
        from collections import Counter
        bucket_counts: Counter = Counter()
        classified_nums: set[int] = set()
        for br in db.execute(
            "SELECT number, bucket FROM triage WHERE repo = ?", (repo,)
        ).fetchall():
            bucket_counts[br["bucket"]] += 1
            classified_nums.add(br["number"])

        total_open = len(open_rows)
        classified_open = sum(1 for r in open_rows if r["number"] in classified_nums)
        unclassified = [r for r in open_rows if r["number"] not in classified_nums]
        unclassified_by_source[label] = unclassified

        row = [label, str(total_open), str(classified_open), str(len(unclassified))]
        for b in BUCKET_ORDER:
            row.append(str(bucket_counts.get(b, 0)))
        summary.add_row(*row)

    console.print(summary)
    console.print()

    shown_any = False
    for label, items in unclassified_by_source.items():
        if not items:
            continue
        shown_any = True
        t = Table(title=f"Unclassified — {label} ({len(items)})",
                  show_header=True, padding=(0, 1), show_edge=False, expand=True)
        t.add_column("Ref", no_wrap=True, width=16)
        t.add_column("Title", ratio=1)
        for r in items:
            t.add_row(f"{label}:{r['number']}", r["title"][:100])
        console.print(t)
        console.print()

    if not shown_any:
        console.print("[green]All open issues across all sources are classified.[/green]")


def cmd_triage_export(project: str, out_path: str | None = None):
    """Write a YAML snapshot of the triage table. Default path: <project>/triage.snapshot.yaml."""
    config = get_config(project)
    db = get_db(project)

    rows = db.execute("""
        SELECT repo, number, bucket, target, rationale, test
        FROM triage ORDER BY repo, number
    """).fetchall()

    # Shape: {source_label: {number: {bucket, ...}}}
    by_label: dict[str, dict[int, dict]] = {}
    for r in rows:
        label = _repo_to_source_label(config, r["repo"])
        entry: dict = {"bucket": r["bucket"], "rationale": r["rationale"] or ""}
        if r["target"]:
            entry["target"] = r["target"]
        if r["test"]:
            entry["test"] = r["test"]
        by_label.setdefault(label, {})[r["number"]] = entry

    ordered = {label: {n: by_label[label][n] for n in sorted(by_label[label])}
               for label in sorted(by_label)}

    path = Path(out_path) if out_path else (SCRIPT_DIR / project / "triage.snapshot.yaml")
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w") as f:
        f.write(TRIAGE_SNAPSHOT_HEADER.format(
            project=project,
            exported_at=datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC"),
        ))
        yaml.safe_dump(ordered, f, sort_keys=False, default_flow_style=False,
                       width=100, allow_unicode=True)

    total = sum(len(v) for v in ordered.values())
    print(f"Exported {total} classifications to {path}")


def cmd_triage_import(project: str, in_path: str):
    """Bulk-upsert triage entries from a YAML file. Rare reverse path."""
    config = get_config(project)
    db = get_db(project)

    path = Path(in_path)
    if not path.exists():
        print(f"File not found: {path}")
        sys.exit(1)

    with path.open() as f:
        data = yaml.safe_load(f) or {}

    imported = 0
    skipped = 0
    for label, entries in data.items():
        repo = _source_label_to_repo(config, label)
        if not repo:
            print(f"WARN: unknown source label '{label}'; skipping {len(entries)} entries.")
            continue
        for num, entry in entries.items():
            try:
                number = int(num)
            except (ValueError, TypeError):
                continue
            if not isinstance(entry, dict) or entry.get("bucket") not in TRIAGE_BUCKETS:
                continue
            exists = db.execute(
                "SELECT 1 FROM issues WHERE repo = ? AND number = ?", (repo, number)
            ).fetchone()
            if not exists:
                skipped += 1
                continue
            db.execute("""
                INSERT INTO triage (repo, number, bucket, target, rationale, test, classified_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
                ON CONFLICT(repo, number) DO UPDATE SET
                    bucket = excluded.bucket,
                    target = excluded.target,
                    rationale = excluded.rationale,
                    test = excluded.test,
                    updated_at = datetime('now')
            """, (
                repo, number,
                entry["bucket"],
                entry.get("target", "") or "",
                entry.get("rationale", "") or "",
                entry.get("test", "") or "",
            ))
            imported += 1
    db.commit()
    print(f"Imported {imported} classifications from {path}"
          + (f" (skipped {skipped} with no matching issue row)" if skipped else ""))


def cmd_triage_verify(project: str):
    """Run pinned tests for real-gap/already-covered rows and flag drift. Stub."""
    console.print("[yellow]triage verify[/yellow] is not implemented yet. "
                  "Planned behavior: for each row with a `test:` field, run the test "
                  "in target_path and compare pass/fail against the bucket. Flag mismatches "
                  "(real-gap → pass = should flip to done; already-covered → fail = regression).")
    config = get_config(project)
    target_path = config.get("target_path")
    if target_path:
        console.print(f"When built, tests will run in: [cyan]{target_path}[/cyan]")
    else:
        console.print("[yellow]WARN:[/yellow] config.json has no `target_path` field; "
                      "triage verify will need one before it can run tests.")


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
            cmd_report(project, by_priority="--by-priority" in extra)
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
        case "note":
            overwrite = "--set" in extra
            args = [a for a in extra if a != "--set"]
            if len(args) < 2:
                print("usage: note <project> <issue_ref> <note_text> [--set]")
                print("  Appends by default. Use --set to overwrite all notes.")
                print("  e.g., note mcpkit go-sdk:148 \"OAuth token caching\"")
                print("        note mcpkit go-sdk:148 \"[pass] test_oauth passes\"")
                sys.exit(1)
            cmd_note(project, args[0], " ".join(args[1:]), overwrite=overwrite)
        case "notes":
            cmd_notes(project, extra[0] if extra else "")
        case "reconcile":
            cmd_reconcile(project)
        case "sync-all":
            count = _parse_int_flag(extra, "--count", 100)
            cmd_sync_all(project, count=count)
        case "triage":
            if not extra:
                print("usage: triage <project> <verb> [args]")
                print("  verbs:")
                print("    set <ref> <bucket> [target] <rationale> [--test path::TestName]")
                print("                                      upsert classification")
                print("    unset <ref>                       remove classification")
                print("    close <ref> [target]              flip real-gap → done (requires target CLOSED)")
                print("    list [--bucket X] [--source X]    grouped view")
                print("    status                            counts + any unclassified open issues")
                print("    export [--out path]               write YAML snapshot")
                print("    import <path>                     bulk-upsert from YAML")
                print("    verify                            drift check (stub)")
                print()
                print(f"  buckets: {', '.join(sorted(TRIAGE_BUCKETS))}")
                print()
                print("  See docs/STATE_MACHINE.md for the full flow model.")
                sys.exit(1)
            verb = extra[0]
            rest = extra[1:]
            match verb:
                case "set":
                    # set <ref> <bucket> [target] <rationale...> [--test path::TestName]
                    # Extract --test first
                    test_path = ""
                    if "--test" in rest:
                        idx = rest.index("--test")
                        if idx + 1 < len(rest):
                            test_path = rest[idx + 1]
                            rest = rest[:idx] + rest[idx + 2:]
                    if len(rest) < 3:
                        print("usage: triage <project> set <ref> <bucket> [target] <rationale...> [--test path::TestName]")
                        print("  e.g., triage mcpkit set ext-apps:298 real-gap mcpkit#190 \"No template support\"")
                        print("  e.g., triage mcpkit set ext-apps:26 not-applicable \"IANA admin task\"")
                        sys.exit(1)
                    ref = rest[0]
                    bucket = rest[1]
                    # If third arg looks like a target token, treat as target; else rationale
                    target = ""
                    rationale_parts = rest[2:]
                    if len(rest) >= 4:
                        candidate = rest[2]
                        if re.fullmatch(r"[A-Za-z0-9_./#-]+", candidate) and " " not in candidate:
                            target = candidate
                            rationale_parts = rest[3:]
                    rationale = " ".join(rationale_parts)
                    cmd_triage_set(project, ref, bucket, target=target,
                                   rationale=rationale, test=test_path)
                case "unset":
                    if not rest:
                        print("usage: triage <project> unset <ref>")
                        sys.exit(1)
                    cmd_triage_unset(project, rest[0])
                case "close":
                    if not rest:
                        print("usage: triage <project> close <ref> [target]")
                        sys.exit(1)
                    target_override = rest[1] if len(rest) > 1 else ""
                    cmd_triage_close(project, rest[0], target_override=target_override)
                case "list":
                    bucket = None
                    source = None
                    i = 0
                    while i < len(rest):
                        if rest[i] == "--bucket" and i + 1 < len(rest):
                            bucket = rest[i + 1]
                            i += 2
                        elif rest[i] == "--source" and i + 1 < len(rest):
                            source = rest[i + 1]
                            i += 2
                        else:
                            i += 1
                    cmd_triage_list(project, bucket=bucket, source=source)
                case "status":
                    cmd_triage_status(project)
                case "export":
                    out_path = None
                    if "--out" in rest:
                        idx = rest.index("--out")
                        if idx + 1 < len(rest):
                            out_path = rest[idx + 1]
                    cmd_triage_export(project, out_path=out_path)
                case "import":
                    if not rest:
                        print("usage: triage <project> import <path>")
                        sys.exit(1)
                    cmd_triage_import(project, rest[0])
                case "verify":
                    cmd_triage_verify(project)
                case _:
                    print(f"Unknown triage verb: {verb}")
                    print("  valid: set, unset, close, list, status, export, import, verify")
                    sys.exit(1)
        case _:
            print(f"Unknown command: {cmd}")
            sys.exit(1)


if __name__ == "__main__":
    main()
