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
		ChannelsDir: "channels", AreasDir: "areas",
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
