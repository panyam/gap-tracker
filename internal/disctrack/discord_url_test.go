package disctrack

import "testing"

func TestParseDiscordURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    DiscordRef
		wantErr bool
	}{
		{name: "channel", in: "https://discord.com/channels/111/222",
			want: DiscordRef{GuildID: "111", ChannelID: "222"}},
		{name: "discordapp host", in: "https://discordapp.com/channels/111/222",
			want: DiscordRef{GuildID: "111", ChannelID: "222"}},
		{name: "ptb host", in: "https://ptb.discord.com/channels/111/222",
			want: DiscordRef{GuildID: "111", ChannelID: "222"}},
		{name: "message link keeps the channel", in: "https://discord.com/channels/111/222/333",
			want: DiscordRef{GuildID: "111", ChannelID: "222"}},
		{name: "thread url", in: "https://discord.com/channels/111/222/threads/444",
			want: DiscordRef{GuildID: "111", ChannelID: "444", ParentID: "222"}},
		{name: "thread message link", in: "https://discord.com/channels/111/222/threads/444/555",
			want: DiscordRef{GuildID: "111", ChannelID: "444", ParentID: "222"}},
		{name: "trailing slash and query", in: "https://discord.com/channels/111/222/?foo=1",
			want: DiscordRef{GuildID: "111", ChannelID: "222"}},
		{name: "surrounding space", in: "  https://discord.com/channels/111/222 ",
			want: DiscordRef{GuildID: "111", ChannelID: "222"}},
		{name: "bare id", in: "222", want: DiscordRef{ChannelID: "222"}},
		{name: "not discord", in: "https://example.com/channels/111/222", wantErr: true},
		{name: "dm url", in: "https://discord.com/channels/@me/222", wantErr: true},
		{name: "lookalike host", in: "https://discord.com.evil.io/channels/111/222", wantErr: true},
		{name: "name not id", in: "general", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseDiscordURL(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %+v want %+v", got, c.want)
			}
			if got.IsThreadURL() != (c.want.ParentID != "") {
				t.Errorf("IsThreadURL = %v", got.IsThreadURL())
			}
		})
	}
}
