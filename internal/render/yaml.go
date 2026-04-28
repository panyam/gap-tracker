package render

import (
	"io"

	"gopkg.in/yaml.v3"
)

// yamlRenderer dumps view structs as YAML. Used for scripting (pipe to yq)
// and for machine consumers that want structured output. Not the LLM feed —
// prefer textRenderer for prompting, yamlRenderer for tooling.
type yamlRenderer struct{}

func (yamlRenderer) RenderStatus(w io.Writer, s StatusReport) error {
	return yaml.NewEncoder(w).Encode(s)
}

func (yamlRenderer) RenderList(w io.Writer, l TriageList) error {
	return yaml.NewEncoder(w).Encode(l)
}

func (yamlRenderer) RenderIssue(w io.Writer, v IssueView) error {
	return yaml.NewEncoder(w).Encode(v)
}

func (yamlRenderer) RenderNotesList(w io.Writer, v NotesListView) error {
	return yaml.NewEncoder(w).Encode(v)
}

func (yamlRenderer) RenderCrossref(w io.Writer, r CrossrefReport) error {
	return yaml.NewEncoder(w).Encode(r)
}

func (yamlRenderer) RenderReconcile(w io.Writer, r ReconcileReport) error {
	return yaml.NewEncoder(w).Encode(r)
}

func (yamlRenderer) RenderSearch(w io.Writer, r SearchReport) error {
	return yaml.NewEncoder(w).Encode(r)
}

func (yamlRenderer) RenderQuery(w io.Writer, r QueryReport) error {
	return yaml.NewEncoder(w).Encode(r)
}

func (yamlRenderer) RenderProposals(w io.Writer, l ProposalList) error {
	return yaml.NewEncoder(w).Encode(l)
}

func (yamlRenderer) RenderVerify(w io.Writer, r VerifyReport) error {
	return yaml.NewEncoder(w).Encode(r)
}
