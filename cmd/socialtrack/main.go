// Command socialtrack finds threads worth replying to on social sites and
// keeps a reply log, so replying becomes a daily habit. Reddit first; see
// internal/socialtrack.
package main

import "github.com/panyam/gap-tracker/internal/socialtrackcli"

func main() {
	socialtrackcli.Execute()
}
