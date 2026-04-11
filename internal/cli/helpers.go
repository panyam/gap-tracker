package cli

import (
	"encoding/json"
	"strconv"
)

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
