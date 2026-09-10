package flywheel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesScaffold(t *testing.T) {
	dir := t.TempDir()

	got, err := Init(dir, false)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if got != dir {
		t.Fatalf("Init() = %q, want %q", got, dir)
	}

	for _, f := range []string{"flywheel.md", ".flywheel/state.json", ".flywheel/briefs"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("Init() did not create %s: %v", f, err)
		}
	}
}

func TestInitWritesMarkdownHeadings(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "flywheel.md"))
	if err != nil {
		t.Fatalf("read flywheel.md: %v", err)
	}
	content := string(b)

	for _, h := range []string{"## Status", "## Main session", "## Workers", "## Roles", "## Task log"} {
		if !strings.Contains(content, h) {
			t.Errorf("flywheel.md missing heading %q", h)
		}
	}
	if !strings.Contains(content, "status: initialized") {
		t.Error("flywheel.md missing 'status: initialized'")
	}
}

func TestInitStateJSONContract(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, ".flywheel", "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}

	var state map[string]json.RawMessage
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatalf("state.json is not valid JSON: %v", err)
	}

	for _, k := range []string{"version", "status", "tasks"} {
		if _, ok := state[k]; !ok {
			t.Errorf("state.json missing key %q (have %v)", k, keys(state))
		}
	}
	if len(state) != 3 {
		t.Errorf("state.json has %d keys, want exactly 3 (version, status, tasks)", len(state))
	}

	var version float64
	_ = json.Unmarshal(state["version"], &version)
	if version != 1 {
		t.Errorf("state.json version = %v, want 1", version)
	}

	var status string
	_ = json.Unmarshal(state["status"], &status)
	if status != "initialized" {
		t.Errorf("state.json status = %q, want initialized", status)
	}

	var tasks []any
	_ = json.Unmarshal(state["tasks"], &tasks)
	if tasks == nil || len(tasks) != 0 {
		t.Errorf("state.json tasks = %#v, want empty array", tasks)
	}
}

func TestInitRefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if _, err := Init(dir, false); err == nil {
		t.Fatal("Init() second call without --force: got nil error, want refusal")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Init() second call error = %v, want 'already exists'", err)
	}

	// Nothing changed: flywheel.md still present, state.json still valid.
	if _, err := os.Stat(filepath.Join(dir, "flywheel.md")); err != nil {
		t.Errorf("flywheel.md missing after refused overwrite: %v", err)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if _, err := Init(dir, true); err != nil {
		t.Fatalf("Init() --force error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "flywheel.md")); err != nil {
		t.Errorf("flywheel.md missing after --force: %v", err)
	}
}

func TestInitCreatesMissingParentDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() into nested dirs error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".flywheel", "state.json")); err != nil {
		t.Errorf("state.json missing after nested Init: %v", err)
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
