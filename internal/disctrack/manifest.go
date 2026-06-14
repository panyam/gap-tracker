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
}

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
