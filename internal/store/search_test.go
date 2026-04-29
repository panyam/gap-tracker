package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/panyam/gap-tracker/internal/github"
)

// openTestStore opens a fresh in-memory store for testing. The schema
// (including FTS5 virtual table) is applied automatically by Open.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedIssues inserts test issues and rebuilds the FTS index.
func seedIssues(t *testing.T, s *Store) {
	t.Helper()
	issues := []github.Issue{
		{Number: 1, Title: "OAuth token refresh support", State: "OPEN", Body: "Add refresh_token grant type to the auth flow", Labels: []string{"auth", "enhancement"}, CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02", URL: "https://example.com/1"},
		{Number: 2, Title: "Streaming HTTP transport", State: "OPEN", Body: "Server-sent events for streaming responses", Labels: []string{"transport"}, CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02", URL: "https://example.com/2"},
		{Number: 3, Title: "Session recovery", State: "CLOSED", Body: "Recover sessions after network interruption via OAuth token cache", Labels: []string{"auth", "reliability"}, CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02", URL: "https://example.com/3"},
		{Number: 4, Title: "Admin endpoint for cache stats", State: "OPEN", Body: "Expose /admin/stats with hit rate, latency percentiles", Labels: []string{"admin"}, CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02", URL: "https://example.com/4"},
		{Number: 5, Title: "Distributed lock provider", State: "OPEN", Body: "SETNX-based lock for cross-node deduplication", Labels: []string{"core"}, CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02", URL: "https://example.com/5"},
	}
	_, err := s.UpsertIssues("test/repo", issues)
	if err != nil {
		t.Fatalf("UpsertIssues: %v", err)
	}
}

func TestSearch_BasicQuery(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	results, err := s.Search("oauth", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'oauth', got none")
	}
	// Should match issues 1 and 3 (both mention oauth)
	numbers := map[int]bool{}
	for _, r := range results {
		numbers[r.Number] = true
	}
	if !numbers[1] {
		t.Error("expected issue #1 (OAuth token refresh) in results")
	}
	if !numbers[3] {
		t.Error("expected issue #3 (Session recovery, mentions OAuth in body) in results")
	}
	if numbers[2] || numbers[4] || numbers[5] {
		t.Error("unexpected non-OAuth issue in results")
	}
}

func TestSearch_PhraseQuery(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	results, err := s.Search(`"token refresh"`, SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for exact phrase, got %d", len(results))
	}
	if results[0].Number != 1 {
		t.Errorf("expected issue #1, got #%d", results[0].Number)
	}
}

func TestSearch_FilterByRepo(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	// Also seed a second repo with an oauth issue
	_, err := s.UpsertIssues("other/repo", []github.Issue{
		{Number: 10, Title: "OAuth bug", State: "OPEN", Body: "oauth broken", CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02", URL: "https://example.com/10"},
	})
	if err != nil {
		t.Fatalf("UpsertIssues: %v", err)
	}

	results, err := s.Search("oauth", SearchFilter{Repo: "test/repo"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Repo != "test/repo" {
			t.Errorf("expected repo test/repo, got %s", r.Repo)
		}
	}
}

func TestSearch_FilterByState(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	results, err := s.Search("oauth", SearchFilter{State: "OPEN"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.State != "OPEN" {
			t.Errorf("expected state OPEN, got %s", r.State)
		}
	}
	if len(results) != 1 {
		t.Errorf("expected 1 OPEN oauth result, got %d", len(results))
	}
}

func TestSearch_FilterByBucket(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	// Classify issue #1 as real-gap
	err := s.UpsertTriage(UpsertTriageInput{
		Repo:      "test/repo",
		Number:    1,
		Bucket:    "real-gap",
		Rationale: "test",
	})
	if err != nil {
		t.Fatalf("UpsertTriage: %v", err)
	}

	results, err := s.Search("oauth", SearchFilter{Bucket: "real-gap"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with bucket=real-gap, got %d", len(results))
	}
	if results[0].Number != 1 {
		t.Errorf("expected issue #1, got #%d", results[0].Number)
	}
	if results[0].Bucket != "real-gap" {
		t.Errorf("expected bucket real-gap, got %q", results[0].Bucket)
	}
}

func TestSearch_Limit(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	results, err := s.Search("the OR admin OR oauth OR streaming OR lock", SearchFilter{Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	s := openTestStore(t)

	_, err := s.Search("", SearchFilter{})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearch_NoResults(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	results, err := s.Search("zzzznonexistent", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_SnippetContainsHighlight(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	results, err := s.Search("streaming", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'streaming'")
	}
	// FTS5 snippet should contain [...] markers around the match
	found := false
	for _, r := range results {
		if containsBracketHighlight(r.Snippet) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected snippet with [...] highlight markers, got: %q", results[0].Snippet)
	}
}

func containsBracketHighlight(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '[' {
			for j := i + 1; j < len(s); j++ {
				if s[j] == ']' {
					return true
				}
			}
		}
	}
	return false
}

func TestSearch_NotesIndexed(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	// Add a note to issue #4 mentioning "thundering herd"
	err := s.SetNotes("test/repo", 4, []Note{{Text: "thundering herd protection needed", At: "2026-04-01"}})
	if err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	results, err := s.Search("thundering", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'thundering' (in notes)")
	}
	if results[0].Number != 4 {
		t.Errorf("expected issue #4, got #%d", results[0].Number)
	}
}

func TestSearch_LabelsIndexed(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	results, err := s.Search("reliability", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'reliability' (in labels)")
	}
	if results[0].Number != 3 {
		t.Errorf("expected issue #3, got #%d", results[0].Number)
	}
}

func TestSearch_PrefixMatch(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	results, err := s.Search("stream*", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for prefix 'stream*'")
	}
	if results[0].Number != 2 {
		t.Errorf("expected issue #2 (streaming), got #%d", results[0].Number)
	}
}

func TestRebuildFTS_Idempotent(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	// Multiple rebuilds should be fine
	for i := 0; i < 3; i++ {
		if err := s.RebuildFTS(); err != nil {
			t.Fatalf("RebuildFTS (call %d): %v", i, err)
		}
	}

	results, err := s.Search("oauth", SearchFilter{})
	if err != nil {
		t.Fatalf("Search after multiple rebuilds: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results after multiple rebuilds")
	}
}

func TestSearch_TriageJoin(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	// Classify issue #2 with a target
	err := s.UpsertTriage(UpsertTriageInput{
		Repo:      "test/repo",
		Number:    2,
		Bucket:    "real-gap",
		Target:    "mcpkit#99",
		Rationale: "streaming gap",
	})
	if err != nil {
		t.Fatalf("UpsertTriage: %v", err)
	}

	results, err := s.Search("streaming", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].Bucket != "real-gap" {
		t.Errorf("expected bucket 'real-gap', got %q", results[0].Bucket)
	}
	if results[0].Target != "mcpkit#99" {
		t.Errorf("expected target 'mcpkit#99', got %q", results[0].Target)
	}
}

func TestSearch_ExistingDB_RebuildOnOpen(t *testing.T) {
	// Simulate opening an existing DB that predates FTS5: create a
	// DB, insert data, then re-open. The FTS index should auto-rebuild.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	_, err = s1.UpsertIssues("test/repo", []github.Issue{
		{Number: 1, Title: "OAuth support", State: "OPEN", Body: "add oauth", CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02", URL: "https://example.com/1"},
	})
	if err != nil {
		t.Fatalf("UpsertIssues: %v", err)
	}
	_ = s1.Close()

	// Re-open. EnsureSchema should rebuild FTS.
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	defer s2.Close()

	results, err := s2.Search("oauth", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results after re-open + auto-rebuild")
	}
}

// TestSearch_OnDiskDB verifies FTS5 works on a real file (not just :memory:).
func TestSearch_OnDiskDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	seedIssues(t, s)

	results, err := s.Search("oauth", SearchFilter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results on on-disk DB")
	}

	// Verify the file exists
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("DB file not found: %v", err)
	}
}
