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

func TestInitRefusesExistingStateWithoutMarkdown(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".flywheel", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir .flywheel: %v", err)
	}
	junk := []byte(`{"version":99,"status":"corrupt","tasks":[1]}`)
	if err := os.WriteFile(statePath, junk, 0o644); err != nil {
		t.Fatalf("write state.json: %v", err)
	}

	if _, err := Init(dir, false); err == nil {
		t.Fatal("Init() without --force: got nil error, want refusal when state.json exists")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Init() error = %v, want 'already exists'", err)
	}

	// Refusal preserved exact bytes and created nothing.
	b, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	if string(b) != string(junk) {
		t.Errorf("state.json changed after refusal: got %q, want %q", b, junk)
	}
	if _, err := os.Stat(filepath.Join(dir, "flywheel.md")); !os.IsNotExist(err) {
		t.Errorf("flywheel.md exists after refusal, want absent")
	}

	// Force resets the existing state file and adds the markdown.
	if _, err := Init(dir, true); err != nil {
		t.Fatalf("Init() --force error = %v", err)
	}
	b, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state.json after force: %v", err)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatalf("state.json not valid JSON after force: %v", err)
	}
	if len(state) != 3 {
		t.Errorf("state.json not reset by force: %v", string(b))
	}
}

func TestInitRefusesExistingMarkdownWithoutState(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "flywheel.md")
	custom := []byte("keep me")
	if err := os.WriteFile(mdPath, custom, 0o644); err != nil {
		t.Fatalf("write flywheel.md: %v", err)
	}

	if _, err := Init(dir, false); err == nil {
		t.Fatal("Init() without --force: got nil error, want refusal when flywheel.md exists")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Init() error = %v, want 'already exists'", err)
	}

	b, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read flywheel.md: %v", err)
	}
	if string(b) != string(custom) {
		t.Errorf("flywheel.md changed after refusal: got %q, want %q", b, custom)
	}
}

func TestInitInvalidStateDestinationLeavesNoMarkdown(t *testing.T) {
	dir := t.TempDir()
	// Block .flywheel/state.json by making .flywheel a regular file.
	dotFlywheel := filepath.Join(dir, ".flywheel")
	if err := os.WriteFile(dotFlywheel, []byte("obstruction"), 0o644); err != nil {
		t.Fatalf("write obstruction: %v", err)
	}

	if _, err := Init(dir, false); err == nil {
		t.Fatal("Init() with invalid state destination: got nil error, want failure")
	}

	// No markdown was left behind to block a retry.
	if _, err := os.Stat(filepath.Join(dir, "flywheel.md")); !os.IsNotExist(err) {
		t.Errorf("flywheel.md exists after failed Init, want absent")
	}

	// Remove the obstruction; the same call now succeeds.
	if err := os.Remove(dotFlywheel); err != nil {
		t.Fatalf("remove obstruction: %v", err)
	}
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() after obstruction removed error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".flywheel", "state.json")); err != nil {
		t.Errorf("state.json missing after retry: %v", err)
	}
}

func TestInitFailedForceRestoresPreexistingBytes(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	mdPath := filepath.Join(dir, "flywheel.md")
	statePath := filepath.Join(dir, ".flywheel", "state.json")

	// Simulate a preexisting markdown a failed force update must restore.
	custom := []byte("custom markdown that must survive a failed force update")
	if err := os.WriteFile(mdPath, custom, 0o644); err != nil {
		t.Fatalf("write custom flywheel.md: %v", err)
	}
	origState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}

	// Make state.json unwritable so the force update fails after the
	// markdown was already overwritten.
	if err := os.Chmod(statePath, 0o444); err != nil {
		t.Fatalf("chmod state.json: %v", err)
	}
	if _, err := Init(dir, true); err == nil {
		t.Skip("read-only state.json did not block the write (privileged user); skipping restore assertion")
	}

	// The preexisting markdown bytes were restored, state.json untouched.
	b, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read flywheel.md: %v", err)
	}
	if string(b) != string(custom) {
		t.Errorf("flywheel.md not restored after failed force: got %q, want %q", b, custom)
	}
	b, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	if string(b) != string(origState) {
		t.Errorf("state.json changed by failed force: got %q, want %q", b, origState)
	}

	// Unblock and verify a retry succeeds and resets both files.
	if err := os.Chmod(statePath, 0o644); err != nil {
		t.Fatalf("chmod state.json back: %v", err)
	}
	if _, err := Init(dir, true); err != nil {
		t.Fatalf("Init() retry after unblock error = %v", err)
	}
	b, err = os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read flywheel.md: %v", err)
	}
	if string(b) != markdownTemplate {
		t.Errorf("flywheel.md not reset by successful force: got %q", b)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("state.json missing after successful force: %v", err)
	}
}

func TestInitForceResetsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "flywheel.md")
	statePath := filepath.Join(dir, ".flywheel", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir .flywheel: %v", err)
	}
	if err := os.WriteFile(mdPath, []byte("junk markdown"), 0o644); err != nil {
		t.Fatalf("write flywheel.md: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("junk state"), 0o644); err != nil {
		t.Fatalf("write state.json: %v", err)
	}

	if _, err := Init(dir, true); err != nil {
		t.Fatalf("Init() --force error = %v", err)
	}

	b, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read flywheel.md: %v", err)
	}
	if string(b) != markdownTemplate {
		t.Errorf("flywheel.md not reset: got %q", b)
	}
	b, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatalf("state.json not valid JSON after force: %v", err)
	}
	if len(state) != 3 {
		t.Errorf("state.json not reset by force: %v", string(b))
	}
}

func TestInitForceRejectsDirectoryDestination(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "flywheel.md")
	if err := os.Mkdir(mdPath, 0o755); err != nil {
		t.Fatalf("mkdir flywheel.md: %v", err)
	}

	if _, err := Init(dir, true); err == nil {
		t.Fatal("Init() --force with directory at flywheel.md: got nil error, want refusal")
	}

	// The directory is untouched (not deleted).
	info, err := os.Stat(mdPath)
	if err != nil {
		t.Fatalf("stat flywheel.md: %v", err)
	}
	if !info.IsDir() {
		t.Error("flywheel.md directory was removed by refused Init")
	}
}

func TestInitForceRejectsSymlinkDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(target, []byte("do not touch"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "flywheel.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	if _, err := Init(dir, true); err == nil {
		t.Fatal("Init() --force with symlink at flywheel.md: got nil error, want refusal")
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read symlink target: %v", err)
	}
	if string(b) != "do not touch" {
		t.Errorf("symlink target modified: got %q", b)
	}
}

func TestInitForcePreservesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing unrelated content, including a brief inside .flywheel/briefs.
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("unrelated"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	briefs := filepath.Join(dir, ".flywheel", "briefs")
	if err := os.MkdirAll(briefs, 0o755); err != nil {
		t.Fatalf("mkdir briefs: %v", err)
	}
	brief := filepath.Join(briefs, "keep.txt")
	if err := os.WriteFile(brief, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}

	if _, err := Init(dir, true); err != nil {
		t.Fatalf("Init() --force error = %v", err)
	}

	for p, want := range map[string]string{notes: "unrelated", brief: "keep"} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		if string(b) != want {
			t.Errorf("%s changed: got %q, want %q", p, b, want)
		}
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
