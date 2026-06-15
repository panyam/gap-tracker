package disctrack

import (
	"fmt"
	"regexp"
)

var digitsOnly = regexp.MustCompile(`^\d+$`)

// ChannelSpec is a parsed channel reference: either a name (manifest lookup)
// or an all-digits channel ID. Mirrors mcpcontribd's resolveChannelSpec
// return shape so the CRUD commands behave the same.
type ChannelSpec struct {
	ChannelID string
	Name      string // empty if user passed a raw ID with no manifest hit
}

// ResolveChannelSpec accepts either a name (looked up via the manifest
// directory) or a numeric channel ID. For a raw ID it returns the manifest
// name when one exists (best-effort reverse lookup), empty otherwise.
func (r *Resolved) ResolveChannelSpec(spec string) (*ChannelSpec, error) {
	if digitsOnly.MatchString(spec) {
		name := r.ReverseLookupChannelName(spec)
		return &ChannelSpec{ChannelID: spec, Name: name}, nil
	}
	m, available, err := r.LoadChannel(spec)
	if err != nil {
		return nil, err
	}
	if m == nil {
		msg := fmt.Sprintf("no channel manifest at %s/%s.yaml", r.ChannelsDir(), spec)
		if len(available) > 0 {
			msg += " (available: "
			for i, a := range available {
				if i > 0 {
					msg += ", "
				}
				msg += a
			}
			msg += ")"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if m.ChannelID == "" {
		return nil, fmt.Errorf("%s/%s.yaml has no channel_id field", r.ChannelsDir(), spec)
	}
	return &ChannelSpec{ChannelID: m.ChannelID, Name: m.Name}, nil
}

// ReverseLookupChannelName scans the channels directory for a manifest whose
// channel_id matches the given ID. Returns empty string on no match.
func (r *Resolved) ReverseLookupChannelName(id string) string {
	for _, name := range r.ListChannels() {
		m, _, err := r.LoadChannel(name)
		if err != nil || m == nil {
			continue
		}
		if m.ChannelID == id {
			return m.Name
		}
	}
	return ""
}
