package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Marguelgtz/Stint/internal/config"
	sessionstate "github.com/Marguelgtz/Stint/internal/session"
)

// sessionArchiveDir is the per-session archive location under the state
// directory: <state>/archive/sessions/<instanceId>.<timestamp>.json.
func sessionArchiveDir(paths config.Paths) string {
	return filepath.Join(paths.StateDir, "archive", "sessions")
}

// ArchiveSession copies the session's final session.json into the per-session
// archive before the state is cleared, so that tracking of a paid instance —
// and any incident involving it (orphaned boxes, unverified destroys,
// overwritten state) can be proven from the operator machine alone.
//
// The copy is a faithful record of the file that is about to be deleted: it
// is read from disk (not re-serialized, which would stamp a new updatedAt)
// and the archive write is append-only history. A failure to write the
// archive never blocks teardown; it is reported on stderr so the operator
// notices a gap in the record.
func ArchiveSession(paths config.Paths, state sessionstate.State, at time.Time) {
	if state.InstanceID <= 0 {
		return
	}
	data, err := os.ReadFile(sessionstate.Path(paths))
	if err != nil {
		fmt.Fprintf(os.Stderr, "stint: session archive: %v\n", err)
		return
	}
	dir := sessionArchiveDir(paths)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "stint: session archive: create %s: %v\n", dir, err)
		return
	}
	name := fmt.Sprintf("%d.%s.json", state.InstanceID, at.UTC().Format("2006-01-02T15-04-05Z"))
	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "stint: session archive: %v\n", err)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		fmt.Fprintf(os.Stderr, "stint: session archive: %v\n", err)
		return
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		fmt.Fprintf(os.Stderr, "stint: session archive: %v\n", err)
		return
	}
	if err := tmp.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "stint: session archive: %v\n", err)
		return
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		fmt.Fprintf(os.Stderr, "stint: session archive: %v\n", err)
		return
	}
	fmt.Printf("Session archive    %s\n", filepath.Join("archive", "sessions", name))
}