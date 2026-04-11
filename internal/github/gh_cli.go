package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// CLIClient implements Client by shelling out to the `gh` CLI.
//
// Why shell out instead of using google/go-github: `gh` already handles
// auth (`gh auth login` has set up a token on this machine), rate
// limiting, and EMU nonsense. For read-heavy workflows the subprocess
// overhead is negligible. When we need streaming or rate-limit headers
// we'll add a second impl and the interface stays the same.
type CLIClient struct {
	// Path overrides the `gh` binary location. Empty means use $PATH.
	Path string
}

// NewCLIClient returns a CLIClient that uses `gh` from $PATH.
func NewCLIClient() *CLIClient { return &CLIClient{} }

// ghEnv builds the environment for a gh subprocess. If GH_PERSONAL_TOKEN
// is set and GH_TOKEN is not, we copy it across — matches the Python
// _gh_env helper and the EMU auth workaround documented in the user's
// global CLAUDE.md memory.
func ghEnv() []string {
	env := os.Environ()
	if os.Getenv("GH_TOKEN") == "" {
		if personal := os.Getenv("GH_PERSONAL_TOKEN"); personal != "" {
			env = append(env, "GH_TOKEN="+personal)
		}
	}
	return env
}

// FetchIssues calls `gh api repos/<repo>/issues?per_page=N&page=M&...`
// and returns the parsed result. Filters out pull requests.
//
// Pagination: the page index is derived from offset / count, matching
// the Python implementation. Hard cap at 10 pages per call as a safety
// valve against runaway syncs.
func (c *CLIClient) FetchIssues(ctx context.Context, repo string, opts FetchOptions) ([]Issue, error) {
	perPage := opts.Count
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}
	page := (opts.Offset / perPage) + 1

	const maxPages = 10
	var all []Issue
	remaining := opts.Count
	fetched := 0

	for remaining > 0 && fetched < maxPages {
		batch := remaining
		if batch > 100 {
			batch = 100
		}
		endpoint := fmt.Sprintf(
			"repos/%s/issues?per_page=%d&page=%d&state=all&sort=created&direction=asc",
			repo, batch, page,
		)

		out, err := c.runGh(ctx, "api", endpoint)
		if err != nil {
			return nil, fmt.Errorf("gh api page %d: %w", page, err)
		}

		// Raw shape from the GitHub REST API. We only need a few fields.
		var raw []struct {
			Number      int    `json:"number"`
			Title       string `json:"title"`
			State       string `json:"state"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
			HTMLURL     string `json:"html_url"`
			Body        string `json:"body"`
			Labels      []struct {
				Name string `json:"name"`
			} `json:"labels"`
			PullRequest *json.RawMessage `json:"pull_request,omitempty"`
		}
		if err := json.Unmarshal(out, &raw); err != nil {
			return nil, fmt.Errorf("decode gh api response: %w", err)
		}

		for _, r := range raw {
			if r.PullRequest != nil {
				continue // skip PRs
			}
			labels := make([]string, 0, len(r.Labels))
			for _, l := range r.Labels {
				labels = append(labels, l.Name)
			}
			body := r.Body
			if len(body) > 2000 {
				body = body[:2000]
			}
			all = append(all, Issue{
				Number:    r.Number,
				Title:     r.Title,
				State:     upperState(r.State),
				Labels:    labels,
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
				URL:       r.HTMLURL,
				Body:      body,
			})
		}

		// Python decrements remaining by len(raw) (pre-PR filter) so that
		// a page full of PRs still advances pagination. Doing the same.
		remaining -= len(raw)
		fetched++

		// If the API returned fewer than the page size, we're at the end.
		if len(raw) < batch {
			break
		}
		page++
	}

	return all, nil
}

// CreateIssue shells out to `gh issue create --repo ... --title ... --body ...`
// and parses the issue number from the returned URL (format:
// https://github.com/owner/repo/issues/NNN).
func (c *CLIClient) CreateIssue(ctx context.Context, repo string, req CreateIssueRequest) (int, error) {
	out, err := c.runGh(ctx, "issue", "create",
		"--repo", repo,
		"--title", req.Title,
		"--body", req.Body,
	)
	if err != nil {
		return 0, fmt.Errorf("gh issue create: %w", err)
	}
	// gh prints the new issue URL on the last line.
	line := bytes.TrimSpace(out)
	// URL ends with /issues/<number>
	var number int
	_, err = fmt.Sscanf(string(lastSegment(line, '/')), "%d", &number)
	if err != nil || number == 0 {
		return 0, fmt.Errorf("could not parse issue number from %q", line)
	}
	return number, nil
}

// IssueState fetches just the state field of one issue.
func (c *CLIClient) IssueState(ctx context.Context, repo string, number int) (string, error) {
	endpoint := fmt.Sprintf("repos/%s/issues/%d", repo, number)
	out, err := c.runGh(ctx, "api", endpoint)
	if err != nil {
		return "", fmt.Errorf("gh api: %w", err)
	}
	var raw struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return upperState(raw.State), nil
}

// FetchIssue retrieves a single issue by number. Returns (nil, nil) if
// gh reports the issue doesn't exist (non-zero exit, typically 404) —
// matches Python's _ensure_target_issue which silently bails on failure.
func (c *CLIClient) FetchIssue(ctx context.Context, repo string, number int) (*Issue, error) {
	endpoint := fmt.Sprintf("repos/%s/issues/%d", repo, number)
	out, err := c.runGh(ctx, "api", endpoint)
	if err != nil {
		return nil, nil
	}
	var raw struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		State     string `json:"state"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		HTMLURL   string `json:"html_url"`
		Body      string `json:"body"`
		Labels    []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode issue %s#%d: %w", repo, number, err)
	}
	labels := make([]string, 0, len(raw.Labels))
	for _, l := range raw.Labels {
		labels = append(labels, l.Name)
	}
	body := raw.Body
	if len(body) > 2000 {
		body = body[:2000]
	}
	return &Issue{
		Number:    raw.Number,
		Title:     raw.Title,
		State:     upperState(raw.State),
		Labels:    labels,
		CreatedAt: raw.CreatedAt,
		UpdatedAt: raw.UpdatedAt,
		URL:       raw.HTMLURL,
		Body:      body,
	}, nil
}

// runGh runs `gh` with the given args and returns stdout.
func (c *CLIClient) runGh(ctx context.Context, args ...string) ([]byte, error) {
	bin := c.Path
	if bin == "" {
		bin = "gh"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = ghEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}

// upperState normalizes GitHub's lower-case state ("open"/"closed") to
// the upper-case variant we use everywhere else ("OPEN"/"CLOSED").
func upperState(s string) string {
	switch s {
	case "open":
		return "OPEN"
	case "closed":
		return "CLOSED"
	}
	return s
}

// lastSegment returns the trailing segment of b after the last sep byte.
// Used to pull the issue number out of `gh issue create`'s URL output.
func lastSegment(b []byte, sep byte) []byte {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == sep {
			return b[i+1:]
		}
	}
	return b
}
