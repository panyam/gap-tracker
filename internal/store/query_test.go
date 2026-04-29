package store

import (
	"testing"
)

func TestQueryReadOnly(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	// Set PRAGMA query_only = ON
	if err := s.DB().Exec("PRAGMA query_only = ON").Error; err != nil {
		t.Fatalf("set query_only: %v", err)
	}

	sqlDB, err := s.DB().DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}

	// SELECT should work
	rows, err := sqlDB.Query("SELECT count(*) FROM issues")
	if err != nil {
		t.Fatalf("SELECT should work in read-only mode: %v", err)
	}
	rows.Close()

	// DELETE should fail
	_, err = sqlDB.Exec("DELETE FROM issues WHERE 1=0")
	if err == nil {
		t.Fatal("expected error for DELETE in read-only mode")
	}

	// INSERT should fail
	_, err = sqlDB.Exec("INSERT INTO issues (repo, number, title, state, created_at, updated_at, url) VALUES ('x', 999, 'x', 'x', 'x', 'x', 'x')")
	if err == nil {
		t.Fatal("expected error for INSERT in read-only mode")
	}
}

func TestQueryGenericColumns(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	sqlDB, err := s.DB().DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}

	rows, err := sqlDB.Query("SELECT repo, number, title FROM issues ORDER BY number LIMIT 2")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}
	if cols[0] != "repo" || cols[1] != "number" || cols[2] != "title" {
		t.Errorf("unexpected columns: %v", cols)
	}

	count := 0
	for rows.Next() {
		count++
		var repo, number, title string
		if err := rows.Scan(&repo, &number, &title); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if repo != "test/repo" {
			t.Errorf("expected repo test/repo, got %s", repo)
		}
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestQueryAggregation(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	sqlDB, err := s.DB().DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}

	rows, err := sqlDB.Query("SELECT state, count(*) AS n FROM issues GROUP BY state ORDER BY state")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()

	type stateCount struct {
		state string
		n     string
	}
	var results []stateCount
	for rows.Next() {
		var sc stateCount
		if err := rows.Scan(&sc.state, &sc.n); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		results = append(results, sc)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 state groups, got %d", len(results))
	}
	// 1 CLOSED (issue #3), 4 OPEN
	stateMap := map[string]string{}
	for _, r := range results {
		stateMap[r.state] = r.n
	}
	if stateMap["OPEN"] != "4" {
		t.Errorf("expected 4 OPEN, got %s", stateMap["OPEN"])
	}
	if stateMap["CLOSED"] != "1" {
		t.Errorf("expected 1 CLOSED, got %s", stateMap["CLOSED"])
	}
}

func TestQueryEmptyResult(t *testing.T) {
	s := openTestStore(t)
	seedIssues(t, s)

	sqlDB, err := s.DB().DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}

	rows, err := sqlDB.Query("SELECT * FROM issues WHERE 1=0")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()

	if rows.Next() {
		t.Error("expected no rows")
	}
}
