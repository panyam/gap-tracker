"""Shared profile resolution for Playwright scripts.

Any Python+Playwright script in this repo that needs a Chromium user-data-dir
should import resolve_profile_dir() so the --profile / --profile-dir / env-var
precedence is consistent.

Layout: by default profiles live at ~/.local/share/disctrack/profiles/<name>/.
The "default" profile is the implicit no-flag choice. Sibling profiles enable
parallel sessions (different Chromium instances against different cookie
stores). disctrack normally passes --profile-dir explicitly (resolved from
the project's .discord-tracking.yaml), so this fallback only matters when the
script is invoked standalone.

Precedence (highest to lowest):
  1. --profile-dir <path>     explicit override (absolute or expanded)
  2. --profile <name>          resolves to PROFILE_BASE / <name>
  3. $DISCTRACK_PROFILE        env var, treated as --profile <value>
  4. "default"                 implicit fallback
"""

from __future__ import annotations

import os
from pathlib import Path

PROFILE_BASE = Path.home() / ".local" / "share" / "disctrack" / "profiles"
DEFAULT_PROFILE_NAME = "default"
ENV_VAR = "DISCTRACK_PROFILE"


def resolve_profile_dir(name: str | None = None, dir_override: str | None = None) -> Path:
    """Return the Chromium user-data-dir to launch with.

    Caller passes args.profile (string or None) and args.profile_dir (string or
    None). The function picks the right one per precedence above.
    """
    if dir_override:
        return Path(dir_override).expanduser().resolve()
    if not name:
        name = os.environ.get(ENV_VAR, "").strip() or DEFAULT_PROFILE_NAME
    return (PROFILE_BASE / name).resolve()


def ensure_profile_dir(path: Path) -> None:
    """Create the profile directory if it doesn't exist."""
    path.mkdir(parents=True, exist_ok=True)
