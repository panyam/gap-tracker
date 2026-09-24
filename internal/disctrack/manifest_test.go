package disctrack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testGuild = "111"

// newTestRoot builds a tracking root in a temp dir with a guild id and an
// empty channels dir.
func newTestRoot(t *testing.T) (*Resolved, string) {
	t.Helper()
	root := t.TempDir()
	channels := filepath.Join(root, "channels")
	if err := os.MkdirAll(channels, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Resolved{Root: root, Config: Config{
		ChannelsDir: "channels", GapsDir: "proposals",
		Discord: DiscordSection{GuildID: testGuild},
	}}
	return r, channels
}

func TestLegacyParentSchema(t *testing.T) {
	r, channels := newTestRoot(t)
	writeManifest(t, channels, "mcp-apps-wg", "name: mcp-apps-wg\nchannel_id: \"222\"\n")
	// Legacy shape: parent: block, no kind.
	writeManifest(t, channels, "mcp-apps-wg-cross-host-rendering-thread", `name: mcp-apps-wg-cross-host-rendering-thread
channel_id: "300"
status: active
parent:
  channel_id: "222"
  channel_name: mcp-apps-wg
  message_id: "300"
`)
	// Legacy block whose channel_name drifted: still matches by id.
	writeManifest(t, channels, "renamed-thread", `name: renamed-thread
channel_id: "301"
parent:
  channel_id: "222"
  channel_name: apps-wg-old-name
`)
	// Current shape.
	writeManifest(t, channels, "mcp-apps-wg-new-thread",
		"name: mcp-apps-wg-new-thread\nchannel_id: \"302\"\nkind: thread\nparent_channel: mcp-apps-wg\n")
	// Unrelated legacy thread of another parent.
	writeManifest(t, channels, "elsewhere-thread", "name: elsewhere-thread\nchannel_id: \"303\"\nparent:\n  channel_id: \"999\"\n")

	for _, arg := range []string{"mcp-apps-wg", "222"} {
		got, err := r.ListThreads(arg)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, m := range got {
			names = append(names, m.Name)
		}
		want := "mcp-apps-wg-cross-host-rendering-thread,mcp-apps-wg-new-thread,renamed-thread"
		if strings.Join(names, ",") != want {
			t.Errorf("ListThreads(%s) = %v, want %s", arg, names, want)
		}
	}

	m, _, _ := r.LoadChannel("mcp-apps-wg-cross-host-rendering-thread")
	if !m.IsThread() || m.ParentChannel != "mcp-apps-wg" {
		t.Errorf("legacy manifest not normalized: %+v", m)
	}
	if got := r.ResolveParentChannelID(m); got != "222" {
		t.Errorf("ResolveParentChannelID = %q", got)
	}
	m, _, _ = r.LoadChannel("renamed-thread")
	if got := r.ResolveParentChannelID(m); got != "222" {
		t.Errorf("drifted legacy ResolveParentChannelID = %q", got)
	}
}

func TestLegacyFolderKeys(t *testing.T) {
	r, channels := newTestRoot(t)
	writeManifest(t, channels, "same", "name: same\nchannel_id: \"1\"\nproposal_folder: same\narea_folder: same\n")
	writeManifest(t, channels, "moved", "name: moved\nchannel_id: \"2\"\nproposal_folder: elsewhere\n")
	writeManifest(t, channels, "moved-area", "name: moved-area\nchannel_id: \"3\"\narea_folder: elsewhere\n")
	writeManifest(t, channels, "same-thread",
		"name: same-thread\nchannel_id: \"4\"\nkind: thread\nparent_channel: same\nproposal_folder: same\n")
	writeManifest(t, channels, "stray-thread",
		"name: stray-thread\nchannel_id: \"5\"\nkind: thread\nparent_channel: same\narea_folder: other\n")

	for _, name := range []string{"same", "same-thread"} {
		if m, _, err := r.LoadChannel(name); err != nil || m == nil {
			t.Errorf("%s: want accepted, got %v", name, err)
		}
	}
	for name, key := range map[string]string{"moved": "proposal_folder", "moved-area": "area_folder", "stray-thread": "area_folder"} {
		_, _, err := r.LoadChannel(name)
		if err == nil {
			t.Errorf("%s: want migration error", name)
			continue
		}
		msg := err.Error()
		for _, want := range []string{filepath.Join(channels, name+".yaml"), key, "no longer supported", "<gaps_dir>/<name>"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: error %q missing %q", name, msg, want)
			}
		}
	}
}

func TestLookupChannelDir(t *testing.T) {
	r, channels := newTestRoot(t)
	writeManifest(t, channels, "gateways-ig", "name: gateways-ig\nchannel_id: \"222\"\n")
	writeManifest(t, channels, "no-folder", "name: no-folder\nchannel_id: \"333\"\n")
	writeManifest(t, channels, "gateways-ig-x-thread",
		"name: gateways-ig-x-thread\nchannel_id: \"444\"\nkind: thread\nparent_channel: gateways-ig\n")
	dir := filepath.Join(r.Root, "proposals", "gateways-ig")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, spec := range []string{"gateways-ig", "222"} {
		got, err := r.LookupChannelDir(spec)
		if err != nil || got != dir {
			t.Errorf("LookupChannelDir(%s) = %q, %v; want %s", spec, got, err, dir)
		}
	}
	cases := map[string]string{
		"nope":                 "no channel manifest",
		"999":                  "not tracked",
		"gateways-ig-x-thread": "disctrack dir gateways-ig",
		"no-folder":            "no docs folder at " + filepath.Join(r.Root, "proposals", "no-folder"),
	}
	for spec, want := range cases {
		_, err := r.LookupChannelDir(spec)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("LookupChannelDir(%s): want error containing %q, got %v", spec, want, err)
		}
	}
}
