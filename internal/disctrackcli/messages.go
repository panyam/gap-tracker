package disctrackcli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/panyam/gap-tracker/internal/disctrack"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	messagesSince  string
	messagesOutput string
)

const (
	msgOutputYAML   = "yaml"
	msgOutputJSON   = "json"
	msgOutputPretty = "pretty"
)

func messagesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "messages <channel-name-or-id> [--since N] [-o yaml|json|pretty]",
		Short: "Query synced Discord messages for a channel",
		Long: `Returns synced messages for the channel, ordered by ts asc.

<channel-name-or-id> resolves like every other disctrack command: all-digits
is treated as an ID, otherwise looked up via the resolver's channels dir.

--since accepts the same dual form as ` + "`cursor set`" + `:
  - All-digits (e.g. 60, 1440)  → N minutes ago
  - Anything else (ISO 8601)    → used verbatim

Omit --since to fetch every synced message for the channel.

Output formats (-o / --output):
  yaml    (default) human-readable structured form; consumable by yq
  json    machine-readable; arrays of objects with quoted keys
  pretty  one block per message: ts + author header, indented content,
          inline metadata for edits / replies / urls

When the result is empty, an explanatory stderr line is emitted so callers
know whether the channel is genuinely quiet or just stale.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if messagesOutput != msgOutputYAML &&
				messagesOutput != msgOutputJSON &&
				messagesOutput != msgOutputPretty {
				return fmt.Errorf(
					"unknown -o value: %q (valid: yaml, json, pretty)",
					messagesOutput,
				)
			}
			r, err := resolveTracking()
			if err != nil {
				return err
			}
			spec, err := r.ResolveChannelSpec(args[0])
			if err != nil {
				return err
			}
			db, err := r.OpenDB()
			if err != nil {
				return err
			}
			since := ""
			if messagesSince != "" {
				since = interpretCursorArg(messagesSince)
			}
			rows, err := disctrack.MessagesSince(db, spec.ChannelID, since)
			if err != nil {
				return err
			}
			// Empty result is ambiguous — could be a quiet channel or a
			// stale DB. Emit a stderr hint so the skill (or human) knows
			// the difference. stdout content stays clean.
			if len(rows) == 0 {
				cursor, _ := disctrack.GetDiscordCursor(db, spec.ChannelID, spec.Name)
				label := spec.Name
				if label == "" {
					label = spec.ChannelID
				}
				if !cursor.Found {
					fmt.Fprintf(os.Stderr,
						"warn: %s has no sync history. Run `disctrack sync %s` to bootstrap.\n",
						label, label)
				} else {
					sinceMsg := "any time"
					if since != "" {
						sinceMsg = since
					}
					fmt.Fprintf(os.Stderr,
						"warn: 0 messages since %s. Last synced: %s (cursor: %s). Run `disctrack sync %s` if you expect newer messages.\n",
						sinceMsg, cursor.UpdatedAt, cursor.CursorValue, label)
				}
			}
			return emitMessages(rows, messagesOutput)
		},
	}
	c.Flags().StringVar(&messagesSince, "since", "", "Filter to messages strictly newer than: N (minutes ago) or an ISO8601 timestamp")
	c.Flags().StringVarP(&messagesOutput, "output", "o", msgOutputYAML, "Output format: yaml | json | pretty")
	return c
}

func emitMessages(rows []disctrack.MessageView, format string) error {
	switch format {
	case msgOutputJSON:
		b, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	case msgOutputPretty:
		printMessagesPretty(rows)
	default: // yaml
		b, err := yaml.Marshal(rows)
		if err != nil {
			return err
		}
		fmt.Print(string(b))
	}
	return nil
}

func printMessagesPretty(rows []disctrack.MessageView) {
	for i, m := range rows {
		if i > 0 {
			fmt.Println()
		}
		author := m.Author
		if author == "" {
			author = "(unknown)"
		}
		fmt.Printf("%s  %s\n", m.TS, author)
		if m.Content != "" {
			for _, line := range strings.Split(m.Content, "\n") {
				fmt.Printf("  %s\n", line)
			}
		} else {
			fmt.Println("  (empty)")
		}
		if m.LastEditedAt != "" {
			fmt.Printf("  ~ edited: %s\n", m.LastEditedAt)
		}
		if m.ReplyTo != "" {
			fmt.Printf("  -> reply_to: %s\n", m.ReplyTo)
		}
		for _, u := range m.URLs {
			fmt.Printf("  link: %s\n", u)
		}
	}
}
