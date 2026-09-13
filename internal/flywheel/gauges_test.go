package flywheel

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// git runs a git command in wd with a test identity and CRLF handling off, so
// trees hash the same on every OS. The commit helpers build the args here.
func git(t *testing.T, wd string, args []string) string {
	t.Helper()
	cmd := exec.Command("git")
	cmd.Dir = wd
	full := []string{"git", "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "core.autocrlf=false"}
	for _, a := range args {
		full = append(full, a)
	}
	cmd.Args = full
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// initRepo makes a throwaway git repo in dir with one committed file a.go.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, []string{"init", "-q"})
	git(t, dir, []string{"config", "core.autocrlf", "false"})
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	git(t, dir, []string{"add", "a.go"})
	git(t, dir, []string{"commit", "-m", "init"})
}

// initTask makes a flywheel dir with a git repo, a committed a.go owned by the
// task, and a planned event pointing at a brief with the given gates.
func initTask(t *testing.T, gates []string) (string, error) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		return "", fmt.Errorf("Init() error = %w", err)
	}
	initRepo(t, dir)
	// Flywheel's own bookkeeping must never enter the unit's tree: ignore it
	// so both treeHash and git write-tree exclude it.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".flywheel/\nflywheel.md\n"), 0o644); err != nil {
		return "", fmt.Errorf("write .gitignore: %w", err)
	}
	brief := "owns: a.go\nneeds: none\n"
	for _, g := range gates {
		brief = brief + "gate: " + g + "\n"
	}
	brief = brief + "\n# TASK: gauges\n"
	if err := os.WriteFile(filepath.Join(dir, "brief.txt"), []byte(brief), 0o644); err != nil {
		return "", fmt.Errorf("write brief: %w", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "planned", Brief: "brief.txt"}); err != nil {
		return "", fmt.Errorf("AppendEvent() error = %w", err)
	}
	git(t, dir, []string{"add", "-A"})
	git(t, dir, []string{"commit", "-m", "brief"})
	return dir, nil
}

func TestValidatePassingGates(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0", "exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	res, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	if res.Tree == "" {
		t.Error("empty tree hash")
	}
	if len(res.Gates) != 2 {
		t.Fatalf("gates = %v, want 2", len(res.Gates))
	}
	if res.Gates[0].RC != 0 || res.Gates[0].HostBlocked {
		t.Errorf("gate 1 rc = %d, want 0 and not host-blocked", res.Gates[0].RC)
	}
	if res.Gates[1].RC != 0 {
		t.Errorf("gate 2 rc = %d, want 0", res.Gates[1].RC)
	}
	if !res.GatesOK || !res.OwnsOK || !res.OK() {
		t.Errorf("gatesOK/ownsOK = %v/%v, want true/true", res.GatesOK, res.OwnsOK)
	}
	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	validated := 0
	for _, e := range evs {
		if e.Kind == "validated" {
			validated++
			if e.Gate == "" || e.Tree == "" || e.Command == "" {
				t.Error("validated event missing gate/tree/command")
			}
			if e.Persona != "supervisor" {
				t.Errorf("validated persona = %q, want supervisor", e.Persona)
			}
			if !strings.HasPrefix(e.Path, ".flywheel/evidence/T1/") {
				t.Errorf("validated path = %q", e.Path)
			}
		}
	}
	if validated != 2 {
		t.Errorf("validated events = %d, want 2", validated)
	}
}

func TestValidateFailingGate(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0", "exit 1"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	res, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	if res.Gates[0].RC != 0 || res.Gates[1].RC != 1 {
		t.Errorf("gates rc = %d/%d, want 0/1", res.Gates[0].RC, res.Gates[1].RC)
	}
	if res.GatesOK || res.OK() {
		t.Errorf("GatesOK = %v, want false", res.GatesOK)
	}
	if !res.OwnsOK {
		t.Error("ownsOK should be true: nothing outside owns")
	}
}

func TestValidateOutOfOwns(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("stray\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	res, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	if res.OwnsOK || res.OK() {
		t.Errorf("OwnsOK = %v, want false", res.OwnsOK)
	}
	if len(res.Outside) != 1 || res.Outside[0] != "b.txt" {
		t.Errorf("outside = %v, want [b.txt]", res.Outside)
	}
	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	for _, e := range evs {
		if e.Kind == "owns_checked" {
			if len(e.Outside) != 1 || e.Outside[0] != "b.txt" {
				t.Errorf("owns_checked outside = %v, want [b.txt]", e.Outside)
			}
			if e.Tree == "" {
				t.Error("owns_checked missing tree")
			}
		}
	}
}

func TestValidateNoGates(t *testing.T) {
	dir, err := initTask(t, []string{})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	if _, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir}); err == nil {
		t.Error("ValidateTask() accepted a brief with no gate: lines")
	} else if !strings.Contains(err.Error(), "gate:") {
		t.Errorf("error = %v, want it to name the gate fix", err)
	}
}

func TestValidateTreeHashMatchesWriteTree(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	idxBefore, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatalf("read .git/index: %v", err)
	}
	res, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	want := git(t, dir, []string{"write-tree"})
	if res.Tree != want {
		t.Errorf("tree = %q, want %q (git write-tree)", res.Tree, want)
	}
	idxAfter, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatalf("read .git/index: %v", err)
	}
	if len(idxBefore) != len(idxAfter) {
		t.Error("real .git/index changed during validation")
	}
	for i := range idxBefore {
		if idxBefore[i] != idxAfter[i] {
			t.Error("real .git/index changed during validation")
			break
		}
	}
}

func TestValidateEvidenceLogWritten(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	if _, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir}); err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	log := filepath.Join(dir, ".flywheel", "evidence", "T1", "r1", "gate-1.log")
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read %s: %v", log, err)
	}
	_ = b
}

// TestValidateGateExitThree checks that a failing gate's nonzero exit code is
// recorded on the ExitError pointer target: a gate `exit 3` must record rc=3
// and still let ValidateTask return without error.
func TestValidateGateExitThree(t *testing.T) {
	dir, err := initTask(t, []string{"exit 3"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	res, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	if len(res.Gates) != 1 {
		t.Fatalf("gates = %v, want 1", len(res.Gates))
	}
	if res.Gates[0].RC != 3 {
		t.Errorf("gate rc = %d, want 3", res.Gates[0].RC)
	}
	if res.GatesOK {
		t.Error("GatesOK = true, want false for a failing gate")
	}
}

// TestValidateWorkdirEmptyOutOfOwns checks that with Workdir empty the gates
// and owns check run in the task's tree (Dir): an out-of-owns file committed
// under the repo must be reported.
func TestValidateWorkdirEmptyOutOfOwns(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("stray\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	res, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	if len(res.Outside) != 1 || res.Outside[0] != "b.txt" {
		t.Errorf("outside = %v, want [b.txt]", res.Outside)
	}
}

// TestTreeHashMatchesWriteTreeIndex checks the throwaway temp index: treeHash
// must equal git write-tree and must not touch the real .git/index.
func TestTreeHashMatchesWriteTreeIndex(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	idxBefore, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatalf("read .git/index: %v", err)
	}
	tree, err := treeHash(dir)
	if err != nil {
		t.Fatalf("treeHash() error = %v", err)
	}
	want := git(t, dir, []string{"write-tree"})
	if tree != want {
		t.Errorf("tree = %q, want %q (git write-tree)", tree, want)
	}
	idxAfter, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatalf("read .git/index: %v", err)
	}
	if len(idxBefore) != len(idxAfter) {
		t.Error("real .git/index changed during treeHash")
	}
	for i := range idxBefore {
		if idxBefore[i] != idxAfter[i] {
			t.Error("real .git/index changed during treeHash")
			break
		}
	}
}

// gitAuto runs git with the repo's own config (no -c autocrlf override), so a
// repo-level core.autocrlf=true exercises git's CRLF warnings on stderr.
func gitAuto(t *testing.T, wd string, args []string) string {
	t.Helper()
	cmd := exec.Command("git")
	cmd.Dir = wd
	full := append([]string{"git"}, args...)
	cmd.Args = full
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// TestValidateAutocrlfWarningNotOutside checks that git's "LF will be replaced
// by CRLF" warning, printed to stderr when core.autocrlf=true touches an LF
// file inside owns, is never parsed as an outside path.
func TestValidateAutocrlfWarningNotOutside(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	gitAuto(t, dir, []string{"init", "-q"})
	gitAuto(t, dir, []string{"config", "core.autocrlf", "true"})
	gitAuto(t, dir, []string{"config", "user.name", "test"})
	gitAuto(t, dir, []string{"config", "user.email", "test@example.com"})
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	gitAuto(t, dir, []string{"add", "a.go"})
	gitAuto(t, dir, []string{"commit", "-q", "-m", "init"})
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".flywheel/\nflywheel.md\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	brief := "owns: a.go\nneeds: none\ngate: exit 0\n\n# TASK: gauges\n"
	if err := os.WriteFile(filepath.Join(dir, "brief.txt"), []byte(brief), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "planned", Brief: "brief.txt"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	gitAuto(t, dir, []string{"add", "-A"})
	gitAuto(t, dir, []string{"commit", "-q", "-m", "brief"})
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n// change\n"), 0o644); err != nil {
		t.Fatalf("rewrite a.go: %v", err)
	}
	res, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir})
	if err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	if len(res.Outside) != 0 {
		t.Errorf("outside = %v, want nothing (stderr CRLF warning must not be a path)", res.Outside)
	}
}
