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

	// Parent is the legacy thread schema: a `parent:` block carrying the
	// parent's channel_id and channel_name, with no `kind:` field. Older
	// hand-written thread manifests (e.g. mcp-apps-wg's cross-host-rendering
	// and adopter-list) use it. A manifest with Parent.ChannelID set and no
	// Kind is treated as a thread. New manifests use Kind + ParentChannel.
	Parent *LegacyParent `yaml:"parent,omitempty" json:"-"`
}

// LegacyParent is the `parent:` block of the legacy thread schema. Only the
// fields disctrack reads are modelled; message_id / message_author /
// message_ts stay in the yaml as provenance.
type LegacyParent struct {
	ChannelID   string `yaml:"channel_id"`
	ChannelName string `yaml:"channel_name"`
}

// normalizeLegacyParent folds the legacy `parent:` block into Kind and
// ParentChannel so the rest of disctrack sees one schema. An explicit Kind
// or ParentChannel wins over the legacy block.
func (m *ChannelManifest) normalizeLegacyParent() {
	if m.Parent == nil || m.Parent.ChannelID == "" && m.Parent.ChannelName == "" {
		return
	}
	if m.Kind == "" {
		m.Kind = ChannelKindThread
	}
	if m.ParentChannel == "" {
		if m.Parent.ChannelName != "" {
			m.ParentChannel = m.Parent.ChannelName
		} else {
			m.ParentChannel = m.Parent.ChannelID
		}
	}
}

// hasParent reports whether this thread manifest points at the parent given
// by name or channel_id. Either side may be empty. Checks parent_channel
// against both, plus the legacy block's channel_id, so a legacy manifest
// whose channel_name drifted from the parent's manifest name still matches
// by id.
func (m *ChannelManifest) hasParent(parentName, parentID string) bool {
	if m.ParentChannel != "" &&
		((parentName != "" && m.ParentChannel == parentName) ||
			(parentID != "" && m.ParentChannel == parentID)) {
		return true
	}
	return m.Parent != nil && parentID != "" && m.Parent.ChannelID == parentID
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
	m.normalizeLegacyParent()
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

// ListThreads returns every thread manifest whose parent matches the given
// parent (by name or by channel_id). Both the current schema (kind: thread +
// parent_channel) and the legacy `parent:` block are recognized. Result is
// ordered by manifest name.
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
		if m.hasParent(parentName, parentID) {
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
	if m.Parent != nil && m.Parent.ChannelID != "" {
		return m.Parent.ChannelID
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
