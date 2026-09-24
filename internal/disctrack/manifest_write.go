package disctrack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// writeNewFile creates path with content, failing if it already exists.
func writeNewFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; not overwriting", path)
		}
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Close()
}

// manifestFields is the subset of a channel manifest disctrack writes.
type manifestFields struct {
	Name          string
	ChannelID     string
	DisplayName   string
	Kind          string
	ParentChannel string
	Tags          []string
	Notes         string
}

// renderManifest renders a manifest in the playbook's field order. Written
// by hand rather than yaml.Marshal so the notes stay a literal block and the
// ids stay quoted, matching the hand-written manifests beside it.
func renderManifest(f manifestFields) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", f.Name)
	fmt.Fprintf(&b, "channel_id: %s\n", yamlQuote(f.ChannelID))
	fmt.Fprintf(&b, "display_name: %s\n", yamlQuote(f.DisplayName))
	b.WriteString("status: active\n")
	fmt.Fprintf(&b, "kind: %s\n", f.Kind)
	if f.ParentChannel != "" {
		fmt.Fprintf(&b, "parent_channel: %s\n", f.ParentChannel)
	}
	b.WriteString("baseline_window: 4w\n")
	fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(f.Tags, ", "))
	b.WriteString("notes: |\n")
	for _, line := range strings.Split(strings.TrimRight(f.Notes, "\n"), "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

// yamlQuote renders s as a double-quoted scalar. A JSON string is a valid
// YAML double-quoted scalar, escapes included.
func yamlQuote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}
