package disctrack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var onboardNow = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func TestPlanOnboard_TextChannel(t *testing.T) {
	r, _ := newTestRoot(t)
	plan, err := r.PlanOnboard(OnboardOptions{
		Name: "gateways-ig", Ref: "https://discord.com/channels/111/222", Now: onboardNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `name: gateways-ig
channel_id: "222"
display_name: "gateways-ig"
status: active
kind: text
baseline_window: 4w
tags: []
notes: |
  Onboarded on 2026-09-24 via disctrack onboard.
`
	if plan.Manifest != want {
		t.Errorf("manifest:\n%s\nwant:\n%s", plan.Manifest, want)
	}
	if plan.ChannelDir != filepath.Join(r.Root, "proposals", "gateways-ig") || !plan.ChannelDirCreate {
		t.Errorf("channel dir: %+v", plan)
	}
	// Planning writes nothing.
	if _, err := os.Stat(plan.ManifestPath); !os.IsNotExist(err) {
		t.Fatalf("plan wrote the manifest")
	}

	if err := r.ApplyOnboard(plan); err != nil {
		t.Fatal(err)
	}
	m, _, err := r.LoadChannel("gateways-ig")
	if err != nil || m == nil {
		t.Fatalf("load: %v %v", m, err)
	}
	if m.ChannelID != "222" || m.IsThread() || m.Status != "active" {
		t.Errorf("round trip: %+v", m)
	}
	readme, err := os.ReadFile(filepath.Join(r.GapsDir(), "gateways-ig", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"# gateways-ig", "## Summary", "## Status", "## Strategy", "## Next steps", "## When you come back"} {
		if !strings.Contains(string(readme), h+"\n") {
			t.Errorf("README missing %q", h)
		}
	}
}

func TestPlanOnboard_ExistingFolderKept(t *testing.T) {
	r, _ := newTestRoot(t)
	existing := r.ChannelDir("fine-grained-auth")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "README.md"), []byte("# keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := r.PlanOnboard(OnboardOptions{Name: "fine-grained-auth", Ref: "222"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ChannelDirCreate || plan.ChannelDir != existing {
		t.Errorf("want existing folder reused, got %+v", plan)
	}
	for _, key := range []string{"area_folder", "proposal_folder"} {
		if strings.Contains(plan.Manifest, key) {
			t.Errorf("manifest carries %s:\n%s", key, plan.Manifest)
		}
	}
	if err := r.ApplyOnboard(plan); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(existing, "README.md"))
	if string(b) != "# keep me\n" {
		t.Errorf("existing README touched: %q", b)
	}
}

func TestPlanOnboard_Thread(t *testing.T) {
	r, channels := newTestRoot(t)
	writeManifest(t, channels, "mcp-apps-wg", "name: mcp-apps-wg\nchannel_id: \"222\"\n")

	plan, err := r.PlanOnboard(OnboardOptions{
		Name: "mcp-apps-wg-pip-thread", Ref: "https://discord.com/channels/111/222/threads/444", Now: onboardNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Kind != ChannelKindThread || plan.ParentName != "mcp-apps-wg" || plan.ChannelID != "444" {
		t.Errorf("plan: %+v", plan)
	}
	if plan.ChannelDir != "" || plan.ChannelDirCreate {
		t.Errorf("thread got a docs folder: %+v", plan)
	}
	if !strings.Contains(plan.Manifest, "kind: thread\nparent_channel: mcp-apps-wg\n") {
		t.Errorf("manifest:\n%s", plan.Manifest)
	}
	if err := r.ApplyOnboard(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.GapsDir()); !os.IsNotExist(err) {
		t.Errorf("thread onboard created the gaps dir")
	}
	threads, _ := r.ListThreads("mcp-apps-wg")
	if len(threads) != 1 || threads[0].Name != "mcp-apps-wg-pip-thread" {
		t.Errorf("ListThreads: %+v", threads)
	}
}

func TestPlanOnboard_ThreadViaParentFlag(t *testing.T) {
	r, channels := newTestRoot(t)
	writeManifest(t, channels, "mcp-apps-wg", "name: mcp-apps-wg\nchannel_id: \"222\"\n")

	// Bare thread URL (thread opened in its own view) plus --parent.
	plan, err := r.PlanOnboard(OnboardOptions{
		Name: "t1", Ref: "https://discord.com/channels/111/444", Parent: "mcp-apps-wg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Kind != ChannelKindThread || plan.ParentName != "mcp-apps-wg" {
		t.Errorf("plan: %+v", plan)
	}
	// --parent by id works too.
	if _, err := r.PlanOnboard(OnboardOptions{Name: "t2", Ref: "444", Parent: "222"}); err != nil {
		t.Errorf("--parent by id: %v", err)
	}
}

func TestPlanOnboard_Refusals(t *testing.T) {
	r, channels := newTestRoot(t)
	writeManifest(t, channels, "mcp-apps-wg", "name: mcp-apps-wg\nchannel_id: \"222\"\n")
	writeManifest(t, channels, "other", "name: other\nchannel_id: \"999\"\n")
	writeManifest(t, channels, "mcp-apps-wg-old-thread",
		"name: mcp-apps-wg-old-thread\nchannel_id: \"555\"\nkind: thread\nparent_channel: mcp-apps-wg\n")

	cases := []struct {
		name    string
		opts    OnboardOptions
		wantErr string
	}{
		{"existing name", OnboardOptions{Name: "mcp-apps-wg", Ref: "https://discord.com/channels/111/777"}, "already exists"},
		{"duplicate channel id", OnboardOptions{Name: "new-name", Ref: "https://discord.com/channels/111/999"}, `already tracked as "other"`},
		{"guild mismatch", OnboardOptions{Name: "x", Ref: "https://discord.com/channels/42/777"}, "guild 42"},
		{"untracked parent", OnboardOptions{Name: "x", Ref: "https://discord.com/channels/111/888/threads/777"},
			"onboard it first:\n  disctrack onboard <parent-name> https://discord.com/channels/111/888"},
		{"unknown --parent", OnboardOptions{Name: "x", Ref: "777", Parent: "nope"}, "--parent"},
		{"--parent disagrees with url", OnboardOptions{Name: "x", Ref: "https://discord.com/channels/111/222/threads/777", Parent: "other"}, "URL's parent is 222"},
		{"parent is a thread", OnboardOptions{Name: "x", Ref: "777", Parent: "mcp-apps-wg-old-thread"}, "do not nest"},
		{"bad name", OnboardOptions{Name: "../x", Ref: "777"}, "invalid channel name"},
		{"not a url", OnboardOptions{Name: "x", Ref: "general"}, "not a Discord channel URL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.PlanOnboard(c.opts)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestApplyOnboard_NeverOverwrites(t *testing.T) {
	r, channels := newTestRoot(t)
	plan, err := r.PlanOnboard(OnboardOptions{Name: "late", Ref: "777"})
	if err != nil {
		t.Fatal(err)
	}
	// Someone else writes the file between plan and apply.
	writeManifest(t, channels, "late", "name: late\nchannel_id: \"1\"\n")
	if err := r.ApplyOnboard(plan); err == nil || !strings.Contains(err.Error(), "not overwriting") {
		t.Fatalf("want refusal, got %v", err)
	}
}

func TestRenderManifest_QuotingAndNotes(t *testing.T) {
	out := renderManifest(manifestFields{
		Name: "p-x-thread", ChannelID: "1", DisplayName: `He said "hi": <ok> & more (thread)`,
		Kind: ChannelKindThread, ParentChannel: "p", Tags: []string{"thread"},
		Notes: "Thread: a: b # not a comment\nline two",
	})
	var m struct {
		ChannelID   string   `yaml:"channel_id"`
		DisplayName string   `yaml:"display_name"`
		Tags        []string `yaml:"tags"`
		Notes       string   `yaml:"notes"`
	}
	if err := yaml.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("rendered manifest does not parse: %v\n%s", err, out)
	}
	if m.ChannelID != "1" || m.DisplayName != `He said "hi": <ok> & more (thread)` {
		t.Errorf("scalars: %+v", m)
	}
	if len(m.Tags) != 1 || m.Tags[0] != "thread" {
		t.Errorf("tags: %v", m.Tags)
	}
	if m.Notes != "Thread: a: b # not a comment\nline two\n" {
		t.Errorf("notes: %q", m.Notes)
	}
}
