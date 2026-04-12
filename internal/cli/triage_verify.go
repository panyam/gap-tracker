package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panyam/gap-tracker/internal/app"
	"github.com/panyam/gap-tracker/internal/render"
	"github.com/panyam/gap-tracker/internal/store"
)

// newTriageVerifyCmd walks every classified row that has a pinned test
// path and runs the test against the target_path checkout. The outcome
// is compared to the row's bucket to surface drift:
//
//   - real-gap test now PASSES   → should-close drift
//     (either the fix landed without retiring the skip, or the test
//     got weakened)
//   - real-gap test SKIPs        → aligned (the PENDING: skip is the
//     expected gate — the repro test is installed but guarded)
//   - real-gap test FAILS        → aligned (the gap is still a gap)
//   - already-covered PASSES     → aligned
//   - already-covered FAILS      → regression
//   - done PASSES                → aligned
//   - done FAILS                 → reopened drift
//
// Other buckets (defer-*, not-applicable) don't get tested — their
// test: field is typically empty, and if it isn't we treat them as
// aligned regardless of outcome (there's nothing to reconcile).
//
// This is a read-only command — it never writes to the triage table.
// Drift rows are surfaced for human review; use `gapt triage set` or
// `gapt triage close` to reconcile.
func newTriageVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Run pinned tests against target_path and flag drift vs triage buckets",
		Long: `Reads every classified row with a non-empty 'test:' field, runs the
test in <config.target_path> using 'go test -run <TestName> <dir>',
and compares the outcome to the row's bucket. Mismatches are reported
as drift for human review.

Typical drift:
  should-close   real-gap repro test now passes without being closed
  regression     already-covered verification test now fails
  reopened       a 'done' repro test now fails

Tests that skip (via t.Skip("PENDING: ...")) are treated as aligned
for real-gap rows — the PENDING skip is the intended repro gate.

Requires config.target_path in <project>/config.json.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.Load(rootDir, project, outputMode)
			if err != nil {
				return err
			}
			defer a.Close()

			if a.Cfg.TargetPath == "" {
				return errf("config.json has no 'target_path'; add the local checkout path for %s", a.Cfg.TargetRepo)
			}

			report, err := runVerify(cmd.Context(), a)
			if err != nil {
				return err
			}
			return a.Renderer.RenderVerify(os.Stdout, report)
		},
	}
}

// runVerify walks triage rows, runs the pinned test for each row that
// has one, and builds a VerifyReport. Rows without a test: field are
// counted as skipped in the report but not emitted as drift.
func runVerify(ctx context.Context, a *app.App) (render.VerifyReport, error) {
	rows, err := a.Store.AllTriage()
	if err != nil {
		return render.VerifyReport{}, err
	}

	report := render.VerifyReport{
		TargetPath: a.Cfg.TargetPath,
	}

	for _, t := range rows {
		if t.Test == "" {
			report.Skipped++
			continue
		}
		report.Tested++

		label := a.Cfg.RepoToSourceLabel(t.Repo)
		ref := fmt.Sprintf("%s:%d", label, t.Number)

		path, testName, err := parseTestRef(t.Test)
		if err != nil {
			report.Rows = append(report.Rows, render.VerifyRow{
				Ref: ref, Bucket: t.Bucket, Test: t.Test,
				Outcome: "error", Drift: "unknown",
				Message: err.Error(),
			})
			continue
		}

		outcome, msg := runGoTest(ctx, a.Cfg.TargetPath, path, testName)
		drift := classifyDrift(t.Bucket, outcome)
		report.Rows = append(report.Rows, render.VerifyRow{
			Ref: ref, Bucket: t.Bucket, Test: t.Test,
			Outcome: outcome, Drift: drift, Message: msg,
		})
	}
	return report, nil
}

// parseTestRef splits "path/to/file_test.go::TestName" into the
// containing directory (for `go test` path arg) and the test name
// (for `-run`). Accepts both "::" and ":" as the separator because
// the Python source used ":" while the /gap-track skill uses "::".
func parseTestRef(s string) (dir string, testName string, err error) {
	idx := strings.LastIndex(s, "::")
	if idx < 0 {
		idx = strings.LastIndex(s, ":")
		if idx < 0 {
			return "", "", fmt.Errorf("test ref must be '<path>::<TestName>', got %q", s)
		}
	}
	path := s[:idx]
	testName = s[idx+2:]
	if strings.HasPrefix(testName, ":") {
		// We matched ':' but also had '::' — adjust.
		testName = testName[1:]
	}
	dir = filepath.Dir(path)
	if dir == "" || dir == "." {
		dir = "./"
	} else if !strings.HasPrefix(dir, "./") {
		dir = "./" + dir
	}
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	return dir, testName, nil
}

// runGoTest runs `go test -run <TestName> ./` from the nearest go.mod
// directory enclosing the test file, and classifies the result as
// pass / fail / skip / missing / error.
//
// Why the nearest-go.mod walk: mcpkit (and projects like it) often
// have sub-modules under the repo root (e.g. ext/ui/ with its own
// go.mod). Running 'go test' from the root against a path inside a
// sub-module fails with "main module does not contain package". We
// detect the real module by walking up from the test file's directory
// until we find a go.mod, and cd there before invoking go test.
//
// We parse Go's test output directly instead of just checking the
// exit code so we can distinguish "test skipped" from "test passed" —
// critical for the PENDING: gate in real-gap rows.
func runGoTest(ctx context.Context, targetPath, dir, testName string) (outcome, message string) {
	workDir, relDir, err := resolveModuleDir(targetPath, dir)
	if err != nil {
		return "error", err.Error()
	}

	cmd := exec.CommandContext(ctx, "go", "test", "-run", "^"+testName+"$", "-v", relDir)
	cmd.Dir = workDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	err = runErr
	text := out.String()

	// Extract the salient line for the message column — the first
	// --- PASS / FAIL / SKIP line if present.
	lines := strings.Split(text, "\n")
	var summary string
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "--- PASS:") ||
			strings.HasPrefix(trimmed, "--- FAIL:") ||
			strings.HasPrefix(trimmed, "--- SKIP:") {
			summary = trimmed
			break
		}
	}

	// Prefer marker-based classification because exit code is 0 for
	// both pass and skip.
	if strings.Contains(text, "--- SKIP:") {
		return "skip", summary
	}
	if strings.Contains(text, "--- PASS:") {
		return "pass", summary
	}
	if strings.Contains(text, "--- FAIL:") {
		return "fail", summary
	}

	// No marker found — either a compile error, a missing test, or
	// a run error. Classify based on exit code.
	if err != nil {
		// Compile failure / test runner error.
		return "error", firstErrLine(text)
	}
	// go test ran but matched nothing (e.g. test renamed).
	return "missing", fmt.Sprintf("no matching test in %s", dir)
}

// resolveModuleDir turns (targetPath, "./ext/ui/") into the Go
// module root for that directory + the test dir relative to it.
//
// Example: given targetPath=/path/to/mcpkit, dir="./ext/ui/", and a
// go.mod at /path/to/mcpkit/ext/ui/go.mod, this returns
// workDir=/path/to/mcpkit/ext/ui, relDir="./".
//
// If no submodule go.mod is found, we fall back to targetPath + the
// original dir — which is correct for tests in the root module.
func resolveModuleDir(targetPath, dir string) (workDir, relDir string, err error) {
	cleanDir := strings.TrimPrefix(dir, "./")
	cleanDir = strings.TrimSuffix(cleanDir, "/")
	abs := filepath.Join(targetPath, cleanDir)

	// Walk up from abs looking for go.mod. Stop at targetPath.
	cur := abs
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			// Found a go.mod. Use this as the work dir.
			rel, err := filepath.Rel(cur, abs)
			if err != nil {
				return "", "", fmt.Errorf("compute rel dir: %w", err)
			}
			if rel == "" || rel == "." {
				rel = "./"
			} else {
				rel = "./" + rel + "/"
			}
			return cur, rel, nil
		}
		if cur == targetPath || cur == "/" || cur == "." {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	// No go.mod found walking up — fall back to targetPath, keeping
	// the original relative dir. The subsequent go test will still
	// fail clearly if there's genuinely no module.
	return targetPath, dir, nil
}

// classifyDrift maps (bucket, outcome) to a drift label. "aligned" is
// the no-op state; anything else is surfaced in the report.
//
// Drift classes:
//
//	should-close   real-gap test now passes — fix landed, flip to done
//	regression     already-covered test now fails — we broke something
//	reopened       done test now fails — the fix regressed
//	stale-done     done row but the repro is still PENDING-skipped —
//	               usually means the bucket got set to done without the
//	               un-skip step, or the fix never actually landed
//	unknown        test didn't run (compile error, missing file)
//	aligned        no drift
func classifyDrift(bucket, outcome string) string {
	switch bucket {
	case "real-gap":
		switch outcome {
		case "fail", "skip":
			return "aligned"
		case "pass":
			return "should-close"
		default:
			return "unknown"
		}
	case "already-covered":
		switch outcome {
		case "pass":
			return "aligned"
		case "fail":
			return "regression"
		default:
			return "unknown"
		}
	case "done":
		switch outcome {
		case "pass":
			return "aligned"
		case "fail":
			return "reopened"
		case "skip":
			return "stale-done"
		default:
			return "unknown"
		}
	default:
		// defer-*, not-applicable — no drift model.
		return "aligned"
	}
}

// firstErrLine pulls the first non-empty line from captured output,
// used for the error message column. Tiny util — inline would be
// fine, extracted for readability.
func firstErrLine(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			return ln
		}
	}
	return "unknown error"
}

// Force store import to stay used even if we later drop direct
// references from this file. Keeps the build graph simple.
var _ = store.ValidBuckets
