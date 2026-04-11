// Package github is the swap-point for GitHub API access.
//
// The Client interface is the only thing commands import — no code
// outside this package talks to gh or HTTP directly. Today we have one
// implementation, CLIClient, which shells out to the `gh` CLI. When we
// want rate-limit headers, pagination cursors, or no-subprocess overhead,
// we add a second implementation (e.g. one that uses
// github.com/google/go-github) and the interface stays the same.
//
// Types in this package are deliberately NOT the same as internal/model.
// The store owns the persistence shape (with every column); this package
// owns the wire shape (what we fetch from GitHub). Callers map one to
// the other. That keeps a GORM-tag change from rippling into the fetch
// path and vice versa.
package github

import "context"

// Client is the set of GitHub operations gap-tracker needs.
//
// Implementations in this package:
//   - CLIClient — subprocess wrapper around `gh` (default, uses existing
//     gh auth and EMU token workaround).
//
// Planned:
//   - APIClient — direct REST/GraphQL via google/go-github, for when
//     we need rate-limit headers or pagination cursors.
type Client interface {
	// FetchIssues pulls issues from a repo ordered by creation date
	// ascending. Pull requests are filtered out; only real issues
	// are returned. Honors FetchOptions.Offset (by page * per_page)
	// and FetchOptions.Count (how many to return).
	FetchIssues(ctx context.Context, repo string, opts FetchOptions) ([]Issue, error)

	// FetchIssue returns a single issue by number. Used by `link` for
	// lazy target-repo caching (so `report` can display titles of
	// freshly-linked target issues without a separate sync). Returns
	// (nil, nil) if the issue doesn't exist (HTTP 404).
	FetchIssue(ctx context.Context, repo string, number int) (*Issue, error)

	// CreateIssue files a new issue on a repo and returns its number.
	// Not used in M3 — M4's /gap-track automation needs it.
	CreateIssue(ctx context.Context, repo string, req CreateIssueRequest) (int, error)

	// IssueState returns the current state ("OPEN" or "CLOSED") of one
	// issue. Used by `triage close` to refuse done-flipping when the
	// target issue is still open. Not used in M3; sync uses FetchIssues.
	IssueState(ctx context.Context, repo string, number int) (string, error)
}

// FetchOptions controls pagination for FetchIssues.
//
// Mirrors the Python behavior: offset is in units of per_page, count is
// how many issues we'd like (implementations may fetch more pages to
// cover the request). Implementations may cap count at their API limit
// and require multiple calls to fill a larger window.
type FetchOptions struct {
	Offset int // first issue index to return (multiple of Count)
	Count  int // how many issues to return (default 100)
}

// CreateIssueRequest is the payload for CreateIssue. Kept minimal until
// M4 needs labels or assignees.
type CreateIssueRequest struct {
	Title string
	Body  string
}

// Issue is the wire shape of one GitHub issue as seen by gap-tracker.
//
// These fields are the projection we care about — subset of the full
// GitHub API response. Implementations map from API JSON to this type;
// callers map from this type to store models.
type Issue struct {
	Number    int
	Title     string
	State     string // "OPEN" or "CLOSED"
	Labels    []string
	CreatedAt string // ISO8601
	UpdatedAt string // ISO8601
	URL       string // html_url
	Body      string // truncated at 2000 chars to match Python
}
