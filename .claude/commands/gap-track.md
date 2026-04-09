Review a source issue from the gap-tracker DB, find or create a matching issue in our repo, and link them.

## Arguments
$ARGUMENTS — `<project> [source_ref]`
- `gap-track mcpkit` — show gap report for mcpkit
- `gap-track mcpkit go-sdk:666` — review go-sdk#666 and link to mcpkit
- `gap-track oneauth spec:282` — review spec#282 and link to oneauth

The project name must match a subdirectory in `~/newstack/gap-tracker/` with a `config.json`.

## Steps

1. **Parse arguments**: split into `project` (first word) and optional `source_ref` (second word)
   - Read `~/newstack/gap-tracker/<project>/config.json` to get `target_repo`
   - If config doesn't exist, tell user to create it

2. **If no source_ref**: run report mode
   - Run: `cd ~/newstack/gap-tracker && uv run python main.py report <project> --by-priority`
   - Show the output (untracked items are sorted by label-based priority: bugs/blockers first, then ready-for-work, enhancements, docs last)
   - Stop here

3. **Show the source issue**:
   - Run: `cd ~/newstack/gap-tracker && uv run python main.py show <project> <source_ref>`
   - Read the output — title, labels, body
   - **If the output shows "Linked gap:"** → this issue is already tracked. Show the link and stop. Don't re-link.

4. **Search for matching issues in our repo**:
   - Extract `target_repo` from config.json
   - Run: `GH_TOKEN="$GH_PERSONAL_TOKEN" gh issue list --repo <target_repo> --state all --limit 20 --search "<keywords from title>" --json number,title,state`
   - If a good match exists, suggest linking to it

5. **Triage decision** — choose one of four actions per issue:

   | Action | When to use | Result |
   |--------|-------------|--------|
   | **Link** | Existing issue in our repo already covers this | Gap points to existing issue |
   | **Create + link** | Aligned with our project's philosophy — we'd accept a PR for this, even if low priority | New issue created in our repo, gap linked to it |
   | **Defer** | Not aligned with our philosophy for now — we'll come back later when the landscape changes | Gap recorded with issue 0, status `deferred` `[>]`, resurfaces in reports |
   | **Not-applicable** | Truly outside our project's scope (use sparingly) | Gap recorded with issue 0, status `not-applicable` `[-]` |

   **Rule of thumb:** prefer Create + link over Defer. If the feature is something we'd eventually want, create a ticket even at low priority. Defer is for things where we're not yet convinced the direction is right.

   When processing a batch, present the triage as a summary table and ask the user to confirm before executing.

6. **Execute the chosen action**:
   - **Link**: `cd ~/newstack/gap-tracker && uv run python main.py link <project> "<feature-name>" <issue#> "<source_ref>"`
   - **Create + link**: Use `GH_TOKEN="$GH_PERSONAL_TOKEN" gh issue create --repo <target_repo>` — write the title and body **in our own words** describing the need/symptom. **Never reference source repos, issue numbers, or copy text from upstream.** The gap-tracker DB stores the provenance linkage separately. Then link the new issue number.
   - **Defer**: `cd ~/newstack/gap-tracker && uv run python main.py link <project> "<feature-name>" 0 "<source_ref>"` then `uv run python main.py mark <project> "<feature-name>" deferred`
   - **Not-applicable**: link with issue 0 then mark not-applicable

7. **Confirm**: Show the updated gap entry
