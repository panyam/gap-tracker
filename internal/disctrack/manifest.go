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
// Only the fields disctrack reads are modelled. Unknown keys are tolerated,
// which covers the fields only the /gaps skill reads (worktree, repo,
// default_label, related_channels, spec_prs, ext_repos, references,
// mcpconformance, mcpkit/code paths, gapt). The playbook documents them.
type ChannelManifest struct {
	Name        string `yaml:"name"`
	ChannelID   string `yaml:"channel_id"`
	DisplayName string `yaml:"display_name"`
	Status      string `yaml:"status"`

	// LegacyAreaFolder and LegacyProposalFolder are the removed folder
	// indirection. A channel's folder is always <gaps-dir>/<name>, so
	// LoadChannel rejects a manifest whose value points anywhere else.
	LegacyAreaFolder     string `yaml:"area_folder" json:"-"`
	LegacyProposalFolder string `yaml:"proposal_folder" json:"-"`

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

// checkLegacyFolder rejects a manifest that still points area_folder or
// proposal_folder at a folder other than its own. A value equal to the
// manifest name is harmless and ignored, so a half-migrated root keeps
// working. A thread may also name its parent, which is where its chatter
// already lands.
func (m *ChannelManifest) checkLegacyFolder(path string) error {
	for _, f := range []struct{ key, val string }{
		{"area_folder", m.LegacyAreaFolder},
		{"proposal_folder", m.LegacyProposalFolder},
	} {
		if f.val == "" || f.val == m.Name {
			continue
		}
		if m.IsThread() && f.val == m.ParentChannel {
			continue
		}
		return fmt.Errorf("%s: %s: %s is no longer supported. "+
			"A channel's docs folder is always <gaps_dir>/<name>, here %q. "+
			"Move the folder to match the channel name (or rename the channel), then delete the %s line",
			path, f.key, f.val, m.Name, f.key)
	}
	return nil
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
	if err := m.checkLegacyFolder(path); err != nil {
		return nil, nil, err
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

// LookupChannelDir returns the absolute docs folder of a tracked non-thread
// channel, <gaps-dir>/<name>. spec is a manifest name or a channel id. It
// errors when the channel is untracked, when it is a thread (threads read
// under their parent's folder), and when the folder does not exist yet.
func (r *Resolved) LookupChannelDir(spec string) (string, error) {
	cs, err := r.ResolveChannelSpec(spec)
	if err != nil {
		return "", err
	}
	if cs.Name == "" {
		return "", fmt.Errorf("channel %s is not tracked: no manifest under %s has that channel_id", spec, r.ChannelsDir())
	}
	m, _, err := r.LoadChannel(cs.Name)
	if err != nil {
		return "", err
	}
	if m.IsThread() {
		return "", fmt.Errorf("%s is a thread and has no docs folder of its own: use its parent, disctrack dir %s", m.Name, m.ParentChannel)
	}
	dir := r.ChannelDir(m.Name)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("no docs folder at %s\n  create it (mkdir -p %s), or move this channel's existing docs there", dir, dir)
	}
	return dir, nil
}
