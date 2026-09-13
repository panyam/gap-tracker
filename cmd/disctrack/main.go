// Command disctrack is the Discord-channel tracker CLI.
//
// First cut is resolver-only: looks up names in a project's
// .discord-tracking.yaml, parses Discord URLs, prints config. The Playwright
// scraper (today: ~/projects/mcpcontrib/discord/snapshot.py, invoked by
// mcpcontribd) stays in place; disctrack just feeds it channel_ids.
//
// See gap-tracker/plugins/gaps/reference/discord-tracking-playbook.md for the layout + config schema.
package main

import "github.com/panyam/gap-tracker/internal/disctrackcli"

func main() {
	disctrackcli.Execute()
}
