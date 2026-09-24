package disctrack

import (
	"fmt"
	"regexp"
	"strings"
)

// discordChannelURL matches the channel and thread URLs Discord's "Copy Link"
// produces:
//
//	https://discord.com/channels/<guild>/<channel>
//	https://discord.com/channels/<guild>/<parent>/threads/<thread>
//
// plus the discordapp.com, ptb. and canary. hosts. Trailing segments (a
// message id) are tolerated and ignored.
var discordChannelURL = regexp.MustCompile(
	`^https?://(?:(?:www|ptb|canary)\.)?(?:discord|discordapp)\.com/channels/(\d+)/(\d+)(?:/threads/(\d+))?(?:[/?#].*)?$`,
)

// DiscordRef is a parsed Discord channel reference.
type DiscordRef struct {
	// GuildID is the server id. Empty when the ref was a bare channel id.
	GuildID string
	// ChannelID is the channel to track: the thread's own id for a thread
	// URL, else the channel segment.
	ChannelID string
	// ParentID is the containing channel for a /threads/ URL. Empty for a
	// plain channel URL, which may still name a thread (a thread opened in
	// its own view has a plain /channels/<guild>/<thread> URL).
	ParentID string
}

// IsThreadURL reports whether the ref came from a /threads/ URL.
func (d DiscordRef) IsThreadURL() bool { return d.ParentID != "" }

// ParseDiscordURL parses a Discord channel or thread URL. A bare all-digits
// channel id is accepted too, with an empty GuildID.
func ParseDiscordURL(s string) (DiscordRef, error) {
	s = strings.TrimSpace(s)
	if digitsOnly.MatchString(s) {
		return DiscordRef{ChannelID: s}, nil
	}
	m := discordChannelURL.FindStringSubmatch(s)
	if m == nil {
		return DiscordRef{}, fmt.Errorf("not a Discord channel URL: %s (want https://discord.com/channels/<guild>/<channel>[/threads/<thread>])", s)
	}
	if m[3] != "" {
		return DiscordRef{GuildID: m[1], ChannelID: m[3], ParentID: m[2]}, nil
	}
	return DiscordRef{GuildID: m[1], ChannelID: m[2]}, nil
}

// ChannelURL renders the canonical https://discord.com/channels/... URL for
// a channel in guildID.
func ChannelURL(guildID, channelID string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s", guildID, channelID)
}
