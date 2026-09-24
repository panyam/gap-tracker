package disctrack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// manifestName is what a channel name may look like. It becomes a filename
// (<channels-dir>/<name>.yaml) and, for a non-thread channel, the docs folder
// <gaps-dir>/<name>, so no slashes or leading dots.
var manifestName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// OnboardOptions is one `disctrack onboard` request.
type OnboardOptions struct {
	// Name is the manifest name to register.
	Name string
	// Ref is a Discord channel or thread URL, or a bare channel id.
	Ref string
	// Parent names the tracked parent channel. Makes the ref a thread when
	// the URL has no /threads/ segment. Must agree with the URL's parent
	// segment when both are given.
	Parent string
	// Now stamps the notes line. Zero means time.Now().
	Now time.Time
}

// OnboardPlan is what onboarding will write. PlanOnboard builds it without
// touching disk; ApplyOnboard writes it. Splitting the two gives --dry-run
// for free.
type OnboardPlan struct {
	Name         string `json:"name"`
	ChannelID    string `json:"channel_id"`
	Kind         string `json:"kind"`
	ParentName   string `json:"parent_channel,omitempty"`
	ManifestPath string `json:"manifest"`
	Manifest     string `json:"-"`
	// ChannelDir is the channel's docs folder, <gaps-dir>/<name>. Empty for
	// threads, which read under their parent's folder.
	ChannelDir string `json:"dir,omitempty"`
	// ChannelDirCreate is true when ChannelDir does not exist yet and
	// ApplyOnboard will create it with the README skeleton.
	ChannelDirCreate bool `json:"dir_created"`
}

// PlanOnboard validates an onboard request and renders the manifest. It
// refuses when the name is taken, when the channel id is already tracked
// under any name, when the URL's guild is not this project's guild, and when
// a thread's parent is not tracked.
func (r *Resolved) PlanOnboard(opts OnboardOptions) (*OnboardPlan, error) {
	if !manifestName.MatchString(opts.Name) {
		return nil, fmt.Errorf("invalid channel name %q: use letters, digits, '.', '_' and '-' (no slashes)", opts.Name)
	}
	ref, err := ParseDiscordURL(opts.Ref)
	if err != nil {
		return nil, err
	}
	guild := r.Config.Discord.GuildID
	if ref.GuildID != "" && guild != "" && ref.GuildID != guild {
		return nil, fmt.Errorf("URL is for guild %s but this tracking root (%s) tracks guild %s (discord.guild_id in %s)",
			ref.GuildID, r.Root, guild, ConfigFile)
	}

	manifestPath := filepath.Join(r.ChannelsDir(), opts.Name+".yaml")
	if pathExists(manifestPath) {
		return nil, fmt.Errorf("%s already exists; pick another name or edit that manifest", manifestPath)
	}
	if existing := r.ReverseLookupChannelName(ref.ChannelID); existing != "" {
		return nil, fmt.Errorf("channel %s is already tracked as %q (%s)",
			ref.ChannelID, existing, filepath.Join(r.ChannelsDir(), existing+".yaml"))
	}

	plan := &OnboardPlan{
		Name:         opts.Name,
		ChannelID:    ref.ChannelID,
		Kind:         ChannelKindText,
		ManifestPath: manifestPath,
	}

	isThread := ref.IsThreadURL() || opts.Parent != ""
	if isThread {
		parentName, err := r.resolveOnboardParent(ref, opts.Parent, guild)
		if err != nil {
			return nil, err
		}
		plan.Kind = ChannelKindThread
		plan.ParentName = parentName
	} else {
		plan.ChannelDir = r.ChannelDir(opts.Name)
		plan.ChannelDirCreate = !pathExists(plan.ChannelDir)
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	fields := manifestFields{
		Name:          opts.Name,
		ChannelID:     ref.ChannelID,
		DisplayName:   opts.Name,
		Kind:          plan.Kind,
		ParentChannel: plan.ParentName,
		Notes:         fmt.Sprintf("Onboarded on %s via disctrack onboard.", now.Format("2006-01-02")),
	}
	plan.Manifest = renderManifest(fields)
	return plan, nil
}

// resolveOnboardParent returns the manifest name of a thread's parent. The
// parent comes from the URL's /threads/ segment, the --parent flag, or both
// (which must then agree).
func (r *Resolved) resolveOnboardParent(ref DiscordRef, parentFlag, guild string) (string, error) {
	var fromFlag *ChannelSpec
	if parentFlag != "" {
		spec, err := r.ResolveChannelSpec(parentFlag)
		if err != nil {
			return "", fmt.Errorf("--parent: %w", err)
		}
		if spec.Name == "" {
			return "", fmt.Errorf("--parent %s: no manifest has that channel_id; onboard the parent first", parentFlag)
		}
		fromFlag = spec
	}

	var name, parentID string
	switch {
	case ref.ParentID != "":
		parentID = ref.ParentID
		name = r.ReverseLookupChannelName(ref.ParentID)
		if name == "" {
			g := ref.GuildID
			if g == "" {
				g = guild
			}
			return "", fmt.Errorf("parent channel %s is not tracked; onboard it first:\n  disctrack onboard <parent-name> %s",
				ref.ParentID, ChannelURL(g, ref.ParentID))
		}
		if fromFlag != nil && fromFlag.ChannelID != ref.ParentID {
			return "", fmt.Errorf("--parent %s is channel %s but the URL's parent is %s (%s)",
				parentFlag, fromFlag.ChannelID, ref.ParentID, name)
		}
	case fromFlag != nil:
		name, parentID = fromFlag.Name, fromFlag.ChannelID
	default:
		return "", errors.New("thread has no parent: pass a /threads/ URL or --parent <name>")
	}

	if parentID == ref.ChannelID {
		return "", fmt.Errorf("a thread cannot be its own parent (%s)", parentID)
	}
	parent, _, err := r.LoadChannel(name)
	if err != nil {
		return "", err
	}
	if parent != nil && parent.IsThread() {
		return "", fmt.Errorf("parent %q is itself a thread; Discord threads do not nest", name)
	}
	return name, nil
}

// ApplyOnboard writes the manifest and, for a non-thread channel whose docs
// folder is missing, the README skeleton. It re-checks that the manifest does not
// exist so a racing onboard cannot clobber it.
func (r *Resolved) ApplyOnboard(plan *OnboardPlan) error {
	if err := os.MkdirAll(filepath.Dir(plan.ManifestPath), 0o755); err != nil {
		return fmt.Errorf("ensure channels dir: %w", err)
	}
	if err := writeNewFile(plan.ManifestPath, plan.Manifest); err != nil {
		return err
	}
	if plan.ChannelDirCreate {
		if err := os.MkdirAll(plan.ChannelDir, 0o755); err != nil {
			return fmt.Errorf("create channel folder: %w", err)
		}
		readme := filepath.Join(plan.ChannelDir, "README.md")
		if err := writeNewFile(readme, channelReadmeSkeleton(filepath.Base(plan.ChannelDir))); err != nil {
			return err
		}
	}
	return nil
}

// channelReadmeSkeleton is the heading-only README a fresh channel folder
// gets. The /gaps skill fills it in after the first refresh.
func channelReadmeSkeleton(title string) string {
	return fmt.Sprintf(`# %s

## Summary

## Status

## Strategy

## Next steps

## When you come back
`, title)
}
