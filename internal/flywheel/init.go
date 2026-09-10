package flywheel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const markdownTemplate = `## Status

status: initialized

## Main session

Placeholder: main session notes go here.

## Workers

Placeholder: worker state and briefs go here.

## Roles

Placeholder: role assignments go here.

## Task log

Placeholder: completed tasks are logged here.
`

// Field order is the JSON key order; the spec pins it to version, status, tasks.
type stateFile struct {
	Version int   `json:"version"`
	Status  string `json:"status"`
	Tasks   []any  `json:"tasks"`
}

// Init scaffolds flywheel state files into dir. It returns the absolute path
// of the initialized directory. When force is false, Init refuses to touch a
// directory that already contains flywheel.md.
func Init(dir string, force bool) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", dir, err)
	}

	mdPath := filepath.Join(abs, "flywheel.md")
	if !force {
		if _, err := os.Stat(mdPath); err == nil {
			return "", fmt.Errorf("%s already exists (use --force to overwrite)", mdPath)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("check %s: %w", mdPath, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(abs, ".flywheel", "briefs"), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Join(abs, ".flywheel", "briefs"), err)
	}
	if err := os.WriteFile(mdPath, []byte(markdownTemplate), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", mdPath, err)
	}

	state := stateFile{
		Version: 1,
		Status:  "initialized",
		Tasks:   []any{},
	}
	stateJSON, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode state: %w", err)
	}
	statePath := filepath.Join(abs, ".flywheel", "state.json")
	if err := os.WriteFile(statePath, append(stateJSON, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", statePath, err)
	}

	return abs, nil
}
