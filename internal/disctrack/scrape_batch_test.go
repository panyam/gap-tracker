package disctrack

import (
	"strings"
	"testing"
)

func TestBatchArgs(t *testing.T) {
	env := scrapeEnv{ScriptPath: "/x/snapshot.py", GuildID: "111", ProfileDir: "/p"}
	got := strings.Join(batchArgs(env, []ScrapeTarget{
		{ChannelID: "200", Since: "2026-09-01T00:00:00Z"},
		{ChannelID: "300"},
	}, 5, true), " ")
	want := "--server-id 111 --profile-dir /p --max-iters 5 --headless --target 200=2026-09-01T00:00:00Z --target 300"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestParseBatchStream(t *testing.T) {
	stream := strings.Join([]string{
		`{"channel_id":"200","message_id":"1","ts":"2026-09-01T00:00:00Z","author":"a","content":"x"}`,
		`{"channel_id":"200","message_id":"2","ts":"2026-09-02T00:00:00Z","author":"a","content":"y"}`,
		`{"event":"target_done","channel_id":"200","ok":true,"emitted":2}`,
		`{"channel_id":"300","message_id":"","ts":"2026-09-02T00:00:00Z"}`,
		`{"event":"target_done","channel_id":"300","ok":false,"error":"no messages rendered"}`,
		`not json`,
		`{"channel_id":"777","message_id":"9","ts":"2026-09-02T00:00:00Z"}`,
		`{"channel_id":"400","message_id":"5","ts":"2026-09-03T00:00:00Z"}`,
		// stream ends before 400's target_done: the process died mid-target
	}, "\n")
	targets := []ScrapeTarget{{ChannelID: "200"}, {ChannelID: "300"}, {ChannelID: "400"}}
	got, err := parseBatchStream(strings.NewReader(stream), targets)
	if err != nil {
		t.Fatal(err)
	}
	if r := got["200"]; r.Err != nil || len(r.Messages) != 2 {
		t.Errorf("200: %+v", r)
	}
	if r := got["300"]; r.Err == nil || r.Err.Error() != "no messages rendered" || r.ParseErrors != 1 {
		t.Errorf("300: %+v", r)
	}
	if r := got["400"]; r.Err == nil || len(r.Messages) != 1 || !strings.Contains(r.Err.Error(), "ended before") {
		t.Errorf("400: %+v", r)
	}
	if _, ok := got["777"]; ok {
		t.Errorf("untargeted channel leaked into results")
	}
}
