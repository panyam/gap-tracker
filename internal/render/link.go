package render

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LinkResolver maps issue references to clickable URLs.
// Pretty renderer uses this to emit OSC 8 terminal hyperlinks.
type LinkResolver interface {
	// RefURL returns a GitHub URL for an issue ref, or "" if unresolvable.
	// Accepts formats: "label:123" (source ref), "repo#123" or "#123" (target ref).
	RefURL(ref string) string
}

// ConfigLinkResolver resolves refs using the project config's repo mappings.
type ConfigLinkResolver struct {
	TargetRepo string            // "panyam/mcpkit"
	LabelToRepo map[string]string // "ext-apps" → "modelcontextprotocol/ext-apps"
}

func (r *ConfigLinkResolver) RefURL(ref string) string {
	if ref == "" || ref == "-" {
		return ""
	}

	// "label:123" format (source ref)
	if i := strings.Index(ref, ":"); i > 0 {
		label := ref[:i]
		num := ref[i+1:]
		if _, err := strconv.Atoi(num); err == nil {
			if repo, ok := r.LabelToRepo[label]; ok {
				return fmt.Sprintf("https://github.com/%s/issues/%s", repo, num)
			}
		}
		return ""
	}

	// "label#123" or "#123" format.
	// If the left side matches a known source label, resolve to that source repo.
	// Otherwise (empty prefix, project alias, or unknown), resolve to TargetRepo.
	if i := strings.LastIndex(ref, "#"); i >= 0 {
		num := ref[i+1:]
		if _, err := strconv.Atoi(num); err == nil {
			prefix := ref[:i]
			if repo, ok := r.LabelToRepo[prefix]; ok {
				return fmt.Sprintf("https://github.com/%s/issues/%s", repo, num)
			}
			return fmt.Sprintf("https://github.com/%s/issues/%s", r.TargetRepo, num)
		}
	}

	return ""
}

// osc8 wraps text in an OSC 8 terminal hyperlink escape sequence.
// If url is empty, returns text unchanged.
func osc8(url, text string) string {
	if url == "" {
		return text
	}
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

// refPattern matches issue refs in rendered output: "label:123", "label#123", "#123".
var refPattern = regexp.MustCompile(`(?:[\w-]+[:#]\d+|#\d+)`)

// linkifyRendered post-processes already-rendered text, replacing plain issue
// refs with OSC 8 hyperlinks. Safe to call on lipgloss output because it
// doesn't change visible character count — only adds invisible escapes.
func linkifyRendered(s string, lr LinkResolver) string {
	if lr == nil {
		return s
	}
	return refPattern.ReplaceAllStringFunc(s, func(match string) string {
		url := lr.RefURL(match)
		if url == "" {
			return match
		}
		return osc8(url, match)
	})
}
