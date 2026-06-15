package disctrack

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupRetainDefault is the default number of backup snapshots to keep.
// Older snapshots beyond this count are pruned by Backup.
const BackupRetainDefault = 10

// BackupResult summarises one backup call.
type BackupResult struct {
	Backup string   `json:"backup"`
	Pruned []string `json:"pruned"`
	Retain int      `json:"retain"`
}

// Backup snapshots messages.db into <BackupDir()>/messages.db.<UTC-stamp>.bak
// and prunes older snapshots to keep at most `retain` (0 → BackupRetainDefault).
// Errors if the source DB doesn't exist yet.
func (r *Resolved) Backup(retain int) (BackupResult, error) {
	if retain <= 0 {
		retain = BackupRetainDefault
	}
	src := r.DBFile()
	if _, err := os.Stat(src); err != nil {
		return BackupResult{}, fmt.Errorf("source db not found at %s: %w", src, err)
	}
	dir := r.BackupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BackupResult{}, err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	dst := filepath.Join(dir, fmt.Sprintf("messages.db.%s.bak", stamp))
	if err := copyFile(src, dst); err != nil {
		return BackupResult{}, err
	}
	pruned, err := pruneBackups(dir, retain)
	if err != nil {
		return BackupResult{Backup: dst, Retain: retain}, err
	}
	return BackupResult{Backup: dst, Pruned: pruned, Retain: retain}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func pruneBackups(dir string, keep int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var backups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "messages.db.") || !strings.HasSuffix(name, ".bak") {
			continue
		}
		backups = append(backups, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))
	var pruned []string
	if len(backups) <= keep {
		return pruned, nil
	}
	for _, name := range backups[keep:] {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			return pruned, err
		}
		pruned = append(pruned, path)
	}
	return pruned, nil
}
