package disctrack

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

// eventTargetDone is the control record snapshot.py emits after each target
// in batch mode (--target). Message records carry no "event" key.
const eventTargetDone = "target_done"

// SnapshotBatchScraper drives one snapshot.py process over several targets:
// one browser launch, one profile lock, a navigation per target. It is the
// BatchScraper the CLI uses for the thread phase.
type SnapshotBatchScraper struct {
	env      scrapeEnv
	MaxIters int
	Headless bool
}

// NewSnapshotBatchScraper resolves the script, guild and profile the same
// way Sync does. profileOverride and scriptOverride are the --profile-dir and
// --script flags.
func (r *Resolved) NewSnapshotBatchScraper(profileOverride, scriptOverride string, maxIters int, headless bool) (*SnapshotBatchScraper, error) {
	env, err := r.resolveScrapeEnv(profileOverride, scriptOverride)
	if err != nil {
		return nil, err
	}
	return &SnapshotBatchScraper{env: env, MaxIters: maxIters, Headless: headless}, nil
}

// batchArgs builds the snapshot.py arguments for a batch. Each target is
// --target <channel_id>[=<since>].
func batchArgs(env scrapeEnv, targets []ScrapeTarget, maxIters int, headless bool) []string {
	args := []string{
		"--server-id", env.GuildID,
		"--profile-dir", env.ProfileDir,
	}
	if maxIters > 0 {
		args = append(args, "--max-iters", strconv.Itoa(maxIters))
	}
	if headless {
		args = append(args, "--headless")
	}
	for _, t := range targets {
		v := t.ChannelID
		if t.Since != "" {
			v += "=" + t.Since
		}
		args = append(args, "--target", v)
	}
	return args
}

// Scrape runs the batch. A process that fails to start is a batch error;
// a process that dies part way leaves the unfinished targets failed and the
// finished ones intact.
func (s *SnapshotBatchScraper) Scrape(targets []ScrapeTarget) (map[string]*TargetScrape, error) {
	if len(targets) == 0 {
		return map[string]*TargetScrape{}, nil
	}
	cmd := uvCommand(s.env.ScriptPath, batchArgs(s.env, targets, s.MaxIters, s.Headless)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start snapshot.py: %w", err)
	}
	results, parseErr := parseBatchStream(stdout, targets)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return results, parseErr
	}
	if waitErr != nil {
		for _, t := range targets {
			if r := results[t.ChannelID]; r != nil && r.Err != nil {
				r.Err = fmt.Errorf("%w (snapshot.py exited: %v)", r.Err, waitErr)
			}
		}
	}
	return results, nil
}

// batchEvent is a control record in the batch stream.
type batchEvent struct {
	Event     string `json:"event"`
	ChannelID string `json:"channel_id"`
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
}

// parseBatchStream splits snapshot.py's batch output into per-target
// results. Message records are routed by channel_id. A target_done event
// closes a target; a target with no such event by EOF is marked failed, as
// is one whose event reports ok: false. Records for channels that are not
// targets are dropped as parse errors on no one's account.
func parseBatchStream(r io.Reader, targets []ScrapeTarget) (map[string]*TargetScrape, error) {
	out := make(map[string]*TargetScrape, len(targets))
	done := make(map[string]bool, len(targets))
	for _, t := range targets {
		out[t.ChannelID] = &TargetScrape{}
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev batchEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			fmt.Fprintln(os.Stderr, "warn: invalid JSON line:", err)
			continue
		}
		if ev.Event != "" {
			if ev.Event != eventTargetDone {
				continue
			}
			res := out[ev.ChannelID]
			if res == nil {
				continue
			}
			done[ev.ChannelID] = true
			if !ev.OK {
				msg := ev.Error
				if msg == "" {
					msg = "snapshot.py reported failure"
				}
				res.Err = errors.New(msg)
			}
			continue
		}
		var m IncomingMessage
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		res := out[m.ChannelID]
		if res == nil {
			fmt.Fprintf(os.Stderr, "warn: record for untargeted channel %s dropped\n", m.ChannelID)
			continue
		}
		if m.MessageID == "" || m.TS == "" {
			res.ParseErrors++
			continue
		}
		res.Messages = append(res.Messages, m)
	}
	for id, res := range out {
		if !done[id] && res.Err == nil {
			res.Err = errors.New("snapshot.py ended before finishing this thread")
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("scan: %w", err)
	}
	return out, nil
}
