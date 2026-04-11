// Package triage is the business layer for YAML export/import.
//
// It's separate from internal/store (which owns SQL) and internal/cli
// (which owns flag parsing) so the YAML shape can be tested without
// touching either. The shape is intentionally parse-compatible with the
// Python cmd_triage_export output — same top-level source labels, same
// integer issue keys, same entry fields — so Python and Go snapshots
// can be consumed interchangeably during the port.
package triage

import (
	"fmt"
	"io"
	"time"

	"gopkg.in/yaml.v3"
)

// snapshotHeader mirrors Python's TRIAGE_SNAPSHOT_HEADER. We write this
// prelude as a raw string before the YAML document so comments aren't
// lost to YAML marshaling (yaml.v3 doesn't preserve top-of-file comments
// when you marshal a plain map/struct).
const snapshotHeader = `# GENERATED — DO NOT EDIT
#
# Snapshot of the triage table in gaps.db for project: %s
# Exported: %s
#
# This file is a point-in-time export. Edits here are NOT authoritative —
# run ` + "`gapt triage %s import <file>`" + ` to bulk-load a
# YAML back into the DB (rare reverse path).
#
# Buckets:
#   real-gap         — failing repro test exists; GH issue filed; track until fixed.
#   already-covered  — passing verification test exists; no new issue needed.
#   defer-spec-flux  — upstream spec still moving; re-review on daily sync.
#   defer-scope      — host-side / out-of-scope for our libraries; unlikely to implement.
#   not-applicable   — truly unrelated (other languages, end-user bugs, example fixes).
#   done             — previously a real-gap, now fixed and closed.

`

// Entry is the YAML shape of one triage row inside a snapshot.
//
// Field order in the Go struct IS the field order in the emitted YAML
// (yaml.v3 respects struct declaration order). We put bucket and
// rationale first because they're always present, then target and test
// last because they're optional — matches Python's output.
type Entry struct {
	Bucket    string `yaml:"bucket"`
	Rationale string `yaml:"rationale"`
	Target    string `yaml:"target,omitempty"`
	Test      string `yaml:"test,omitempty"`
}

// Snapshot is the top-level YAML document shape: source label →
// issue number → entry. yaml.v3 sorts map keys alphabetically for
// strings and numerically for integers, so the output is deterministic
// without us having to preserve insertion order.
type Snapshot map[string]map[int]Entry

// WriteSnapshot marshals a Snapshot to the given writer, preceded by
// the GENERATED header. project is embedded in the header so you can
// tell which project a snapshot came from.
func WriteSnapshot(w io.Writer, project string, snap Snapshot) error {
	exportedAt := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	if _, err := fmt.Fprintf(w, snapshotHeader, project, exportedAt, project); err != nil {
		return err
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(snap); err != nil {
		return err
	}
	return enc.Close()
}

// ReadSnapshot parses a YAML snapshot back into memory. Unknown fields
// in Entry are ignored — if we add new columns later, old snapshots
// still import cleanly.
func ReadSnapshot(r io.Reader) (Snapshot, error) {
	var snap Snapshot
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&snap); err != nil {
		return nil, err
	}
	return snap, nil
}
