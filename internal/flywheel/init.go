package flywheel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const markdownTemplate = `## Status

status: initialized

<!-- flywheel:status:start -->
<!-- flywheel:status:end -->

## Main session

Placeholder: main session notes go here.

## Workers

Placeholder: worker state and briefs go here.

## Roles

Placeholder: role assignments go here.

## Task log

Placeholder: completed tasks are logged here.
`

// Init scaffolds flywheel state files into dir. It returns the absolute path
// of the initialized directory.
//
// When force is false, Init refuses to touch a directory that already
// contains either flywheel.md or .flywheel/state.json, and preserves the
// directory byte-for-byte. When force is true, preexisting regular files at
// those paths are reset, but directories and symlinks are rejected as file
// destinations either way. Non-force writes create files exclusively so a
// racing creator is detected instead of silently truncated.
//
// Init is not a crash-atomic multi-file transaction. Payloads are staged
// before any write, and a returned error rolls back only what this call
// itself created or overwrote (preexisting bytes are restored, newly created
// files are removed, directories created by this call are removed when
// empty). Unrelated files are never touched. A crash mid-write can still
// leave partial state; retries may need --force.
func Init(dir string, force bool) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", dir, err)
	}

	mdPath := filepath.Join(abs, "flywheel.md")
	dotFlywheel := filepath.Join(abs, ".flywheel")
	briefsDir := filepath.Join(dotFlywheel, "briefs")
	statePath := filepath.Join(dotFlywheel, "state.json")
	eventsPath := filepath.Join(dotFlywheel, "events.jsonl")
	gitignorePath := filepath.Join(dotFlywheel, ".gitignore")

	// Preflight both file destinations before touching anything so a refusal
	// preserves the directory exactly as it was.
	for _, p := range []string{mdPath, statePath} {
		if err := preflightDestination(p, force); err != nil {
			return "", err
		}
	}

	// Stage the payloads before publishing anything.
	mdBytes := []byte(markdownTemplate)
	stateJSON, err := json.MarshalIndent(Derive([]Event{}), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode state: %w", err)
	}
	stateBytes := append(stateJSON, '\n')

	// Snapshot what existed before this call so a failure can restore
	// preexisting bytes and remove only what this call created.
	mdExisted, mdPrev := snapshotFile(mdPath)
	stateExisted, statePrev := snapshotFile(statePath)
	briefsExisted := dirExisted(briefsDir)
	dotFlywheelExisted := dirExisted(dotFlywheel)
	createdMD := false
	createdState := false
	createdEvents := false
	createdGitignore := false

	// rollback undoes this call's own footprint after an error: restore
	// preexisting regular-file bytes, remove files this call created, and
	// remove directories this call created (only if empty, never recursive).
	rollback := func() {
		if mdExisted {
			_ = os.WriteFile(mdPath, mdPrev, 0o644)
		} else if createdMD {
			_ = os.Remove(mdPath)
		}
		if stateExisted {
			_ = os.WriteFile(statePath, statePrev, 0o644)
		} else if createdState {
			_ = os.Remove(statePath)
		}
		if createdEvents {
			_ = os.Remove(eventsPath)
		}
		if createdGitignore {
			_ = os.Remove(gitignorePath)
		}
		if !briefsExisted {
			_ = os.Remove(briefsDir)
		}
		if !dotFlywheelExisted {
			_ = os.Remove(dotFlywheel)
		}
	}

	if err := os.MkdirAll(briefsDir, 0o755); err != nil {
		rollback()
		return "", fmt.Errorf("create %s: %w", briefsDir, err)
	}

	createdMD, err = publishFile(mdPath, mdBytes, force)
	if err != nil {
		rollback()
		return "", fmt.Errorf("write %s: %w", mdPath, err)
	}

	createdState, err = publishFile(statePath, stateBytes, force)
	if err != nil {
		rollback()
		return "", fmt.Errorf("write %s: %w", statePath, err)
	}

	// The event log is the source of truth: create it and .gitignore only if
	// missing. Neither is ever overwritten or truncated, even with --force.
	createdEvents, err = createIfMissing(eventsPath, []byte{})
	if err != nil {
		rollback()
		return "", fmt.Errorf("write %s: %w", eventsPath, err)
	}
	createdGitignore, err = createIfMissing(gitignorePath, []byte("runs/\n"))
	if err != nil {
		rollback()
		return "", fmt.Errorf("write %s: %w", gitignorePath, err)
	}

	return abs, nil
}

// preflightDestination checks that path is a usable regular-file destination
// before Init writes anything. Under force, directories and symlinks are
// rejected (writes must not go through them); without force, any existing
// entry is refused so the directory is preserved byte-for-byte.
func preflightDestination(path string, force bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("check %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to write through it", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file; refusing to overwrite it", path)
	}
	if !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
	return nil
}

// publishFile writes b to path. With force the file is truncated and
// rewritten; otherwise it must not already exist and is created exclusively
// (O_EXCL) so a racing creator fails the write instead of being truncated.
// It reports whether this call created the file, so a rollback can remove it
// without touching a file created by someone else.
func publishFile(path string, b []byte, force bool) (created bool, err error) {
	if force {
		return false, os.WriteFile(path, b, 0o644)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false, err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(path)
		return true, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return true, err
	}
	return true, nil
}

// createIfMissing writes b to path only when path does not exist, using
// O_EXCL so a racing creator wins and the file is never overwritten or
// truncated. It reports whether this call created the file.
func createIfMissing(path string, b []byte) (created bool, err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, err
	}
	if len(b) > 0 {
		if _, err := f.Write(b); err != nil {
			f.Close()
			os.Remove(path)
			return true, err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return true, err
	}
	return true, nil
}

// snapshotFile returns whether p existed as a regular file before Init ran
// and, if so, its exact bytes, so a failed force update can restore them.
func snapshotFile(p string) (existed bool, prev []byte) {
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return false, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return true, nil // exists but unreadable; restore as best effort
	}
	return true, b
}

// dirExisted reports whether p existed before Init ran; used only to decide
// whether a rollback may attempt to remove it (removal is best-effort and
// only succeeds when the directory is empty).
func dirExisted(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
