package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/panyam/gap-tracker/internal/app"
)

// loadAppFromArgs pops args[0] as the project name, loads the app, and
// returns the remaining positional args.
//
// Every verb that needs a project context uses this instead of reading
// a persistent flag — matches the Python CLI's `gapt <verb> <project>
// [args]` shape and means users never have to type `-p mcpkit`.
//
// Returns a helpful error if args is empty or the project config
// isn't found.
func loadAppFromArgs(args []string) (*app.App, []string, error) {
	if len(args) == 0 {
		return nil, nil, errf("missing <project> — first positional arg (e.g. 'gapt <verb> mcpkit ...')")
	}
	proj := args[0]
	a, err := app.Load(rootDir, proj, outputMode)
	if err != nil {
		return nil, nil, err
	}
	return a, args[1:], nil
}

// canonicalBucketOrder is the iteration order used everywhere a grouped
// view is rendered. Mirrors BUCKET_ORDER in the Python source.
var canonicalBucketOrder = []string{
	"real-gap", "already-covered", "defer-spec-flux",
	"defer-scope", "not-applicable", "done",
}

// itoa is strconv.Itoa with a shorter name — used often in format helpers.
func itoa(n int) string { return strconv.Itoa(n) }

// jsonDecode is a thin wrapper so callers don't have to import encoding/json.
func jsonDecode(raw string, into any) error {
	return json.Unmarshal([]byte(raw), into)
}

// writeTextTable is a minimal column-aligned table writer for legacy
// verbs that render plain output without going through the Renderer
// interface. render/text.go has a richer implementation but it's
// internal to that package; we duplicate the tiny amount of code here
// rather than exporting it.
func writeTextTable(w io.Writer, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = runeCount(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if i >= len(widths) {
				break
			}
			if n := runeCount(c); n > widths[i] {
				widths[i] = n
			}
		}
	}

	writeRow := func(cells []string) {
		for i, c := range cells {
			if i >= len(widths) {
				break
			}
			fmt.Fprint(w, c)
			if i < len(widths)-1 {
				fmt.Fprint(w, strings.Repeat(" ", widths[i]-runeCount(c)+2))
			}
		}
		fmt.Fprintln(w)
	}

	writeRow(headers)
	sep := make([]string, len(headers))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	writeRow(sep)
	for _, r := range rows {
		writeRow(r)
	}
}

// runeCount counts unicode code points in s, so column widths aren't
// skewed by multi-byte characters.
func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
