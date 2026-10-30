package socialtrackcli

import "testing"

func TestShortIDDropsSourceAndKind(t *testing.T) {
	for in, want := range map[string]string{
		"reddit:t3_1xbv8ds": "1xbv8ds",
		"t3_1xbv8ds":        "1xbv8ds",
		"1xbv8ds":           "1xbv8ds",
	} {
		if got := shortID(in); got != want {
			t.Errorf("shortID(%q) = %q, want %q", in, got, want)
		}
	}
}
