Review a source issue from the gap-tracker DB, validate whether the gap exists by writing a test, then triage.

**Source of truth is `gaps.db`** — specifically the `triage` table. YAML snapshots are export-only via `triage export`. All verbs below write directly to the DB. See `~/newstack/gap-tracker/docs/STATE_MACHINE.md` for the full state machine and flows.

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

   c. **Run the test** and observe the raw result (pass/fail). This is the "truth" for the validation note below — record what happens WITHOUT any skip in place.

   d. **Record the result** as a note on the source issue:
      - Pass: `cd ~/newstack/gap-tracker && uv run python main.py note <project> <source_ref> "[pass] <test_file>:<test_name> — <what it proved>"`
      - Fail: `cd ~/newstack/gap-tracker && uv run python main.py note <project> <source_ref> "[fail] <test_file>:<test_name> — <what's missing>"`
      - Skip: `cd ~/newstack/gap-tracker && uv run python main.py note <project> <source_ref> "[skip-validation] <reason>"`

   e. **What to do with the test file**:
      - If the test **passed** (no gap): ask the user whether to keep it as regression coverage or discard it
      - If the test **failed** (gap confirmed): **keep the test, but mark it pending with a `PENDING:` skip as the FIRST statement of the test body**, so CI stays green until the gap is actually worked on. The un-skipped test is the repro; the skipped test is the searchable TODO that lives alongside the code.

      **Skip format** — the marker MUST reference the filed target issue so `grep "PENDING:"` surfaces all pending gaps:

      ```go
      // Go (testing)
      func TestFoo(t *testing.T) {
          t.Skip("PENDING: gap repro for panyam/mcpkit#190 — unskip when picking up the issue, then fix the gap until the test passes")
          // ... rest of the test body unchanged ...
      }
      ```

      ```python
      # Python (pytest)
      def test_foo():
          pytest.skip("PENDING: gap repro for panyam/mcpkit#190 — unskip when picking up the issue, then fix the gap until the test passes")
          # ... rest of the test body unchanged ...
      ```

      ```java
      // Java (JUnit 5)
      @Test
      void testFoo() {
          Assumptions.abort("PENDING: gap repro for panyam/mcpkit#190 — unskip when picking up the issue, then fix the gap until the test passes");
          // ... rest of the test body unchanged ...
      }
      ```

      **Rules for the skip:**
      - Place it as the FIRST statement of the test body. The remainder must be the real failing test so removing the skip immediately reproduces the gap — no separate reconstruction step for whoever picks up the issue.
      - Use the exact prefix `PENDING:` (caps + colon) so it's greppable across the repo.
      - Reference the filed target issue (`<repo>#<number>`). If the gap has no filed issue yet, do not commit the test at all — triage must finish first.
      - Run the test suite one more time after adding the skip to confirm the test now reports SKIP and the suite is green.
      - Record the filed-issue number in the `--test` argument to `triage set` (`<path>::<TestName>`), so `triage verify` (when it lands) can correlate pending-skipped tests to their filed issues.

6. **Triage decision** — informed by validation result. Each maps to a single `triage set` bucket:

   | Action | When to use | Bucket | Side effect |
   |---|---|---|---|
   | **Link (existing)** | An existing target issue already covers this | `real-gap` | `triage set <ref> real-gap <existing#> "rationale"` |
   | **Create + link** | Gap confirmed (test failed) | `real-gap` | File new GH issue → `triage set <ref> real-gap <new#> "rationale" --test path::Name` |
   | **Already implemented** | Test passed | `already-covered` | `triage set <ref> already-covered [target] "rationale" --test path::Name` |
   | **Defer (spec moving)** | Upstream still in flight | `defer-spec-flux` | `triage set <ref> defer-spec-flux "rationale"` |
   | **Defer (out of scope)** | Not aligned with our philosophy | `defer-scope` | `triage set <ref> defer-scope "rationale"` |
   | **Not-applicable** | Truly unrelated | `not-applicable` | `triage set <ref> not-applicable "rationale"` |

   **Validation guides the default choice:**
   - Test failed → default to **Create + link** (the failing test is the repro)
   - Test passed → default to **Already implemented**
   - Skipped/untestable → fall back to judgment

   Present the recommendation and ask the user to confirm.

7. **Execute the chosen action**:
   - **Link (existing)**: `cd ~/newstack/gap-tracker && uv run python main.py triage <project> set <source_ref> real-gap <existing#> "rationale"`
   - **Create + link**: Use `GH_TOKEN="$GH_PERSONAL_TOKEN" gh issue create --repo <target_repo>` — write the title and body **in our own words** describing the gap. If a failing test exists, mention the test file and what it demonstrates. **Never reference source repos, issue numbers, or copy text from upstream.**

     **Then**: edit the test file to add the `PENDING: gap repro for <target_repo>#<new_issue#>` skip as the first statement of the test body (step 5e), re-run the suite to confirm it now SKIPs, and finally:

     `uv run python main.py triage <project> set <source_ref> real-gap <target_repo>#<new_issue#> "rationale" --test <path>::<TestName>`
   - **Already implemented**: `uv run python main.py triage <project> set <source_ref> already-covered [target] "rationale" --test <path>::<TestName>`
   - **Defer (spec moving)**: `uv run python main.py triage <project> set <source_ref> defer-spec-flux "rationale"`
   - **Defer (out of scope)**: `uv run python main.py triage <project> set <source_ref> defer-scope "rationale"`
   - **Not-applicable**: `uv run python main.py triage <project> set <source_ref> not-applicable "rationale"`

8. **Confirm**: Run `show <project> <source_ref>` to display the final state with notes and the triage row
