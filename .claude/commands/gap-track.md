Review a source issue from the gap-tracker DB, validate whether the gap exists by writing a test, then triage and link.

## Arguments
$ARGUMENTS — `<project> [source_ref]`
- `gap-track mcpkit` — show gap report for mcpkit
- `gap-track mcpkit go-sdk:666` — review go-sdk#666 and triage for mcpkit
- `gap-track oneauth spec:282` — review spec#282 and triage for oneauth

The project name must match a subdirectory in `~/newstack/gap-tracker/` with a `config.json`.

## Steps

1. **Parse arguments**: split into `project` (first word) and optional `source_ref` (second word)
   - Read `~/newstack/gap-tracker/<project>/config.json` to get `target_repo`
   - If config doesn't exist, tell user to create it

2. **If no source_ref**: run report mode
   - Run: `cd ~/newstack/gap-tracker && uv run python main.py report <project> --by-priority`
   - Show the output and stop here

3. **Show the source issue**:
   - Run: `cd ~/newstack/gap-tracker && uv run python main.py show <project> <source_ref>`
   - Read the output — title, labels, body
   - **If the output shows "Linked gap:"** → already tracked. Show the link and stop.

4. **Search for matching issues in our repo**:
   - Extract `target_repo` from config.json
   - Run: `GH_TOKEN="$GH_PERSONAL_TOKEN" gh issue list --repo <target_repo> --state all --limit 20 --search "<keywords from title>" --json number,title,state`
   - If a good match exists, note it for the triage step

5. **Validate the gap** — determine if this is testable, then probe:

   a. **Assess testability**: Read the source issue and decide:
      - Is this a concrete capability, bug, or behavior that can be tested against our codebase?
      - Or is it infra, documentation, tooling, or a design discussion that isn't testable?
      - If not testable, skip to step 6 with a note like `[skip-validation] <reason>`

   b. **Write a test**: You already know this project's test framework and conventions — use them.
      - Formulate what the source issue describes as a capability or behavior
      - Write a focused test that exercises whether our platform already supports it
      - Place the test where it naturally belongs in the project's test structure
      - The test should PASS if we already have the capability, FAIL if we don't

   c. **Run the test** and observe the result

   d. **Record the result** as a note on the source issue:
      - Pass: `cd ~/newstack/gap-tracker && uv run python main.py note <project> <source_ref> "[pass] <test_file>:<test_name> — <what it proved>"`
      - Fail: `cd ~/newstack/gap-tracker && uv run python main.py note <project> <source_ref> "[fail] <test_file>:<test_name> — <what's missing>"`
      - Skip: `cd ~/newstack/gap-tracker && uv run python main.py note <project> <source_ref> "[skip-validation] <reason>"`

   e. **What to do with the test file**:
      - If the test **failed** (gap confirmed): keep the test — it becomes the repro in the ticket
      - If the test **passed** (no gap): ask the user whether to keep it as regression coverage or discard it

6. **Triage decision** — informed by validation result:

   | Action | When to use | Result |
   |--------|-------------|--------|
   | **Link** | Existing issue in our repo already covers this | Gap points to existing issue |
   | **Create + link** | Gap confirmed (test failed) or untestable but aligned with our philosophy | New issue created, gap linked. If a failing test exists, reference it in the issue body |
   | **Already implemented** | Test passed — we already have this | Mark `not-applicable`, note records the proof |
   | **Defer** | Not aligned with our philosophy for now | Gap recorded with issue 0, status `deferred` |
   | **Not-applicable** | Truly outside our scope (use sparingly) | Gap recorded with issue 0, status `not-applicable` |

   **Validation guides the default choice:**
   - Test failed → default to **Create + link** (the failing test is the repro)
   - Test passed → default to **Already implemented**
   - Skipped/untestable → fall back to judgment (same as before)

   Present the recommendation and ask the user to confirm.

7. **Execute the chosen action**:
   - **Link**: `cd ~/newstack/gap-tracker && uv run python main.py link <project> "<feature-name>" <issue#> "<source_ref>"`
   - **Create + link**: Use `GH_TOKEN="$GH_PERSONAL_TOKEN" gh issue create --repo <target_repo>` — write the title and body **in our own words** describing the gap. If a failing test exists, mention the test file and what it demonstrates. **Never reference source repos, issue numbers, or copy text from upstream.** Then link the new issue number.
   - **Already implemented**: `cd ~/newstack/gap-tracker && uv run python main.py link <project> "<feature-name>" 0 "<source_ref>"` then `uv run python main.py mark <project> "<feature-name>" not-applicable`
   - **Defer**: link with issue 0 then mark deferred
   - **Not-applicable**: link with issue 0 then mark not-applicable

8. **Confirm**: Run `show` on the source ref to display the final state with notes and gap link
