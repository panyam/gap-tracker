// Command gapt is the gap-tracker CLI.
//
// See ~/newstack/gap-tracker/docs/STATE_MACHINE.md for the full flow model
// and developer entry points.
package main

import "github.com/panyam/gap-tracker/internal/cli"

func main() {
	cli.Execute()
}
