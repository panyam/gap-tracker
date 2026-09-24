package disctrackcli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/panyam/gap-tracker/internal/disctrack"
)

func TestEmitThreadReport(t *testing.T) {
	rep := disctrack.ThreadSyncReport{
		Threads: []disctrack.ThreadSyncResult{
			{Channel: "wg-a-thread", SyncResult: disctrack.SyncResult{ChannelID: "200", Inserted: 3}, Parent: "wg", AutoTracked: true, Status: "ok"},
			{Channel: "wg-b-thread", SyncResult: disctrack.SyncResult{ChannelID: "201"}, Parent: "wg", Status: "failed", Error: "boom"},
		},
		AutoTracked: []string{"wg-a-thread"},
		Skipped:     []string{},
	}
	var buf bytes.Buffer
	emitThreadReport(&buf, rep, "wg", false)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 2 thread lines + summary, got %d:\n%s", len(lines), buf.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	// Thread lines carry the parent sync's fields (flattened) plus their own.
	for _, k := range []string{"channel", "channel_id", "new", "edits", "status", "auto_tracked", "parent"} {
		if _, ok := first[k]; !ok {
			t.Errorf("thread line missing %q: %s", k, lines[0])
		}
	}
	var sum struct {
		Summary disctrack.ThreadSyncTotals `json:"summary"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &sum); err != nil {
		t.Fatal(err)
	}
	if sum.Summary.Threads != 2 || sum.Summary.OK != 1 || sum.Summary.Failed != 1 || sum.Summary.New != 3 || sum.Summary.AutoTracked != 1 {
		t.Errorf("summary: %+v", sum.Summary)
	}
}
