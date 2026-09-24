package disctrack

import (
	"strings"
	"testing"
)

// uv resolves its interpreter from $VIRTUAL_ENV and from a .venv in the cwd
// or any ancestor, so the scraper must run from the script's own directory
// with VIRTUAL_ENV unset, whatever directory disctrack was started from.
func TestUVCommandPinsCwdAndDropsVirtualEnv(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "/somewhere/else/.venv")
	cmd := uvCommand("/opt/gap-tracker/python/snapshot.py", "--channel", "1")
	if cmd.Dir != "/opt/gap-tracker/python" {
		t.Errorf("Dir = %q", cmd.Dir)
	}
	if got := strings.Join(cmd.Args[1:], " "); got != "run /opt/gap-tracker/python/snapshot.py --channel 1" {
		t.Errorf("args = %q", got)
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "VIRTUAL_ENV=") {
			t.Errorf("VIRTUAL_ENV leaked into the child env")
		}
	}
	if len(cmd.Env) == 0 {
		t.Errorf("child env is empty; want the parent env minus VIRTUAL_ENV")
	}
}
