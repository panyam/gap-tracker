#!/usr/bin/env bash
# Install/update gap-tracker skills into Claude Code and Cursor.
# Re-run safely — replaces existing symlinks or files.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SKILLS_DIR="$SCRIPT_DIR/.claude/commands"

CLAUDE_CMD_DIR="$HOME/.claude/commands"
CURSOR_CMD_DIR="$HOME/.cursor/commands"

link_skill() {
    local src="$1" dest_dir="$2" name="$3"
    mkdir -p "$dest_dir"
    local dest="$dest_dir/$name"
    if [ -L "$dest" ]; then
        rm "$dest"
    elif [ -f "$dest" ]; then
        echo "  backing up existing $dest → ${dest}.bak"
        mv "$dest" "${dest}.bak"
    fi
    ln -s "$src" "$dest"
    echo "  $dest → $src"
}

echo "Installing gap-tracker skills..."

for skill in "$SKILLS_DIR"/*.md; do
    name="$(basename "$skill")"
    echo ""
    echo "Skill: $name"
    link_skill "$skill" "$CLAUDE_CMD_DIR" "$name"
    link_skill "$skill" "$CURSOR_CMD_DIR" "$name"
done

echo ""
echo "Done. Skills available as /gap-track in Claude Code and Cursor."
