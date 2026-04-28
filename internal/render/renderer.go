// Package render turns typed command results into human or machine output.
//
// The Renderer interface sits between command code and output formatting.
// Commands produce typed view structs (StatusReport, TriageRow, IssueView)
// and hand them to a Renderer without caring whether the result ends up
// as a lipgloss-styled table, a plain ASCII dump for LLMs, or a YAML blob
// for scripting. Adding a new output mode is a new Renderer implementation.
//
// Available modes:
//
//	auto    pretty if stdout is a TTY, text otherwise (default)
//	pretty  lipgloss-styled tables with color — interactive use
//	text    plain ASCII, no ANSI codes — LLM-safe, pipe-friendly
//	yaml    structured YAML for scripting
//	json    structured JSON (future)
package render

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Mode is the selected output format.
type Mode string

const (
	ModeAuto   Mode = "auto"
	ModePretty Mode = "pretty"
	ModeText   Mode = "text"
	ModeYAML   Mode = "yaml"
	ModeJSON   Mode = "json"
)

// ParseMode turns a --output string into a Mode, returning an error for
// unknown values so the CLI can surface a helpful message.
func ParseMode(s string) (Mode, error) {
	switch Mode(strings.ToLower(s)) {
	case ModeAuto, ModePretty, ModeText, ModeYAML, ModeJSON:
		return Mode(strings.ToLower(s)), nil
	case "":
		return ModeAuto, nil
	default:
		return "", fmt.Errorf("unknown output mode %q (valid: auto, pretty, text, yaml, json)", s)
	}
}

// Renderer writes typed view structs to an io.Writer. Each method is
// responsible for choosing the right layout for its data type.
//
// The separation is coarse on purpose: one method per view type, not one
// per command, so adding a new command that produces an existing view
// doesn't need a new renderer method.
type Renderer interface {
	// Primary verb views (M2).
	RenderStatus(w io.Writer, s StatusReport) error
	RenderList(w io.Writer, l TriageList) error
	RenderIssue(w io.Writer, v IssueView) error

	// Legacy verb views (M7 — rewired from plain-text-only so every
	// command respects --output uniformly).
	RenderNotesList(w io.Writer, v NotesListView) error
	RenderCrossref(w io.Writer, r CrossrefReport) error
	RenderReconcile(w io.Writer, r ReconcileReport) error

	// Search + query views.
	RenderSearch(w io.Writer, r SearchReport) error
	RenderQuery(w io.Writer, r QueryReport) error

	// Proposal views.
	RenderProposals(w io.Writer, l ProposalList) error

	// M7 drift-check view.
	RenderVerify(w io.Writer, r VerifyReport) error
}

// ForMode returns a Renderer for the given mode, resolving "auto" to
// pretty if isTTY is true, text otherwise. An optional LinkResolver
// enables clickable OSC 8 hyperlinks in pretty mode.
func ForMode(m Mode, isTTY bool, links ...LinkResolver) Renderer {
	if m == ModeAuto {
		if isTTY {
			m = ModePretty
		} else {
			m = ModeText
		}
	}

	var lr LinkResolver
	if len(links) > 0 {
		lr = links[0]
	}

	switch m {
	case ModePretty:
		return &prettyRenderer{links: lr}
	case ModeText:
		return &textRenderer{}
	case ModeYAML:
		return &yamlRenderer{}
	case ModeJSON:
		// JSON is a follow-up; fall back to yaml so --output=json doesn't blow up.
		return &yamlRenderer{}
	default:
		return &textRenderer{}
	}
}

// IsTerminal reports whether f is attached to an interactive terminal.
// Used to resolve ModeAuto. Kept simple (stat mode check) so we don't
// pull in golang.org/x/term for one call.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
