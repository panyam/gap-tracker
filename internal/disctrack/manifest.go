package disctrack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ChannelManifest is the per-channel yaml at <channels-dir>/<name>.yaml.
// Only the fields disctrack reads are modelled; unknown keys are tolerated.
type ChannelManifest struct {
	Name           string `yaml:"name"`
	ChannelID      string `yaml:"channel_id"`
	DisplayName    string `yaml:"display_name"`
	Status         string `yaml:"status"`
	AreaFolder     string `yaml:"area_folder"`
	ProposalFolder string `yaml:"proposal_folder"` // legacy alias of AreaFolder

	// Kind is the Discord channel kind. Empty defaults to "text". Other
	// values: "thread" (a thread under a parent channel), "forum"
	// (Discord's forum-style channel where every post is itself a thread).
	Kind string `yaml:"kind"`

	// ParentChannel is the name (or channel_id) of the containing channel
	// when Kind == "thread". Empty otherwise.
	ParentChannel string `yaml:"parent_channel"`
}

// EffectiveKind returns Kind with the default applied: empty → "text".
func (m *ChannelManifest) EffectiveKind() string {
	if m.Kind == "" {
		return ChannelKindText
	}
	return m.Kind
}

// IsThread reports whether this manifest names a Discord thread (not a
// top-level text channel or forum).
func (m *ChannelManifest) IsThread() bool {
	return m.EffectiveKind() == ChannelKindThread
}

// Channel kinds recognized by disctrack. Stored in ChannelManifest.Kind.
const (
	ChannelKindText   = "text"
	ChannelKindThread = "thread"
	ChannelKindForum  = "forum"
)

// AreaSlug returns the area folder name for this channel: AreaFolder if set,
// then ProposalFolder (legacy), else Name.
func (m *ChannelManifest) AreaSlug() string {
	if m.AreaFolder != "" {
		return m.AreaFolder
	}
	if m.ProposalFolder != "" {
		return m.ProposalFolder
	}
	return m.Name
}

// LoadChannel reads <channels-dir>/<name>.yaml. On miss, returns a nil
// manifest plus the list of available channel names (for did-you-mean).
func (r *Resolved) LoadChannel(name string) (*ChannelManifest, []string, error) {
	path := filepath.Join(r.ChannelsDir(), name+".yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, r.ListChannels(), nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m ChannelManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Name == "" {
		m.Name = name
	}
	return &m, nil, nil
}

// ListChannels returns sorted channel names found under <channels-dir>.
func (r *Resolved) ListChannels() []string {
	entries, err := os.ReadDir(r.ChannelsDir())
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(out)
	return out
}

// AreaPath returns the absolute path of <areas-dir>/<name>. Does NOT verify
// existence; callers that need that should stat the returned path.
func (r *Resolved) AreaPath(name string) string {
	return filepath.Join(r.AreasDir(), name)
}

// ListThreads returns every channel manifest whose Kind == "thread" and
// whose ParentChannel matches the given parent (by name or by channel_id).
// Result is ordered by manifest name.
func (r *Resolved) ListThreads(parent string) ([]ChannelManifest, error) {
	// Pre-resolve the parent's channel_id so the caller can pass either a
	// name ("strategy") or a numeric id (1040651639276699680). When the
	// arg looks like a channel id, manifest-by-name lookup may miss; both
	// match modes are tried below.
	var parentID, parentName string
	if digitsOnly.MatchString(parent) {
		parentID = parent
		parentName = r.ReverseLookupChannelName(parent)
	} else {
		parentName = parent
		m, _, err := r.LoadChannel(parent)
		if err == nil && m != nil {
			parentID = m.ChannelID
		}
	}

	out := []ChannelManifest{}
	for _, name := range r.ListChannels() {
		m, _, err := r.LoadChannel(name)
		if err != nil || m == nil {
			continue
		}
		if m.EffectiveKind() != ChannelKindThread {
			continue
		}
		if m.ParentChannel == "" {
			continue
		}
		if (parentName != "" && m.ParentChannel == parentName) ||
			(parentID != "" && m.ParentChannel == parentID) {
			out = append(out, *m)
		}
	}
	return out, nil
}

// ResolveParentChannelID returns the channel_id of a thread's parent, or
// "" when the manifest isn't a thread / has no resolvable parent. Used by
// ingest to denormalize parent_channel_id onto each Message row.
func (r *Resolved) ResolveParentChannelID(m *ChannelManifest) string {
	if m == nil || !m.IsThread() || m.ParentChannel == "" {
		return ""
	}
	if digitsOnly.MatchString(m.ParentChannel) {
		return m.ParentChannel
	}
	parent, _, err := r.LoadChannel(m.ParentChannel)
	if err != nil || parent == nil {
		return ""
	}
	return parent.ChannelID
}

// ListAreas returns sorted area folder names, excluding meta dirs (those
// starting with "_" like _groups).
func (r *Resolved) ListAreas() []string {
	entries, err := os.ReadDir(r.AreasDir())
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}
