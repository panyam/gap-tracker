package disctrack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ConfigFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGapsDir(t *testing.T) {
	cases := []struct {
		name, body, wantDir string
		wantWarn            bool
	}{
		{"default", "channels_dir: channels\n", "proposals", false},
		{"gaps_dir", "gaps_dir: docs/gaps\n", "docs/gaps", false},
		{"legacy areas_dir", "areas_dir: areas\n", "areas", true},
		{"gaps_dir wins over areas_dir", "gaps_dir: g\nareas_dir: a\n", "g", false},
		{"groups_dir ignored", "gaps_dir: g\ngroups_dir: _groups\n", "g", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := writeConfig(t, c.body)
			r, err := ResolveFrom(root)
			if err != nil {
				t.Fatal(err)
			}
			if r.Config.GapsDir != c.wantDir {
				t.Errorf("GapsDir = %q, want %q", r.Config.GapsDir, c.wantDir)
			}
			if got := r.GapsDir(); got != filepath.Join(root, c.wantDir) {
				t.Errorf("abs GapsDir = %q", got)
			}
			cfg := Config{GapsDir: r.Config.GapsDir}
			if c.wantWarn {
				cfg = Config{LegacyAreasDir: c.wantDir}
			}
			warn := applyDefaults(&cfg)
			if (warn != "") != c.wantWarn {
				t.Errorf("warning = %q, want warning %t", warn, c.wantWarn)
			}
			if c.wantWarn && !strings.Contains(warn, "gaps_dir") {
				t.Errorf("warning does not name gaps_dir: %q", warn)
			}
		})
	}
}
