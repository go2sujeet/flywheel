package flywheel

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// briefSHA returns the SHA-256 hex of the file at path.
func briefSHA(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// verifyAll runs VerifyTasks for a single task and returns every failing rule
// id as a set.
func verifyAll(t *testing.T, dir, task string) map[string]bool {
	t.Helper()
	res, err := VerifyTasks(dir, VerifyOptions{Dir: dir, Tasks: []string{task}})
	if err != nil {
		t.Fatalf("VerifyTasks() error = %v", err)
	}
	fails := map[string]bool{}
	for _, item := range res.Items {
		if !item.Pass {
			fails[item.Rule] = true
		}
	}
	return fails
}

// buildCleanChain builds a task whose chain passes every rule: planned brief,
// dispatched with a matching brief sha, a finished worker event, then a
// ValidateTask pass and an InspectTask pass.
func buildCleanChain(t *testing.T) string {
	t.Helper()
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T02:00:00Z", Task: "T1", Kind: "dispatched", Attempt: "r1", Session: "w1", SHA256: briefSHA(t, filepath.Join(dir, "brief.txt"))}); err != nil {
		t.Fatalf("append dispatched: %v", err)
	}
	logFinished(t, dir, "T1", "w1")
	if _, err := ValidateTask(dir, "T1", ValidateOptions{Dir: dir}); err != nil {
		t.Fatalf("ValidateTask() error = %v", err)
	}
	if err := InspectTask(dir, "T1", InspectOptions{Dir: dir, Verdict: "pass", Session: "i1"}); err != nil {
		t.Fatalf("InspectTask() error = %v", err)
	}
	return dir
}

func TestVerifyCleanChainPasses(t *testing.T) {
	dir := buildCleanChain(t)
	res, err := VerifyTasks(dir, VerifyOptions{Dir: dir, Tasks: []string{"T1"}})
	if err != nil {
		t.Fatalf("VerifyTasks() error = %v", err)
	}
	if !res.Passed {
		for _, item := range res.Items {
			if !item.Pass {
				t.Errorf("unexpected FAIL %s: %s", item.Rule, item.Reason)
			}
		}
	}
}

func TestVerifyT1TamperedBrief(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T02:00:00Z", Task: "T1", Kind: "dispatched", Attempt: "r1", SHA256: "deadbeef"}); err != nil {
		t.Fatalf("append dispatched: %v", err)
	}
	fails := verifyAll(t, dir, "T1")
	if !fails["T1"] {
		t.Error("verify did not flag a tampered dispatched brief (T1)")
	}
}

func TestVerifyT3InspectedPassWithoutReadings(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	logFinished(t, dir, "T1", "w1")
	if err := AppendEvent(dir, Event{TS: "2026-09-12T03:00:00Z", Task: "T1", Kind: "inspected", Verdict: "pass", Session: "i1", Persona: "inspector", Tree: "deadbeef"}); err != nil {
		t.Fatalf("append inspected: %v", err)
	}
	fails := verifyAll(t, dir, "T1")
	if !fails["T3"] {
		t.Error("verify did not flag an inspected pass without readings (T3)")
	}
}

func TestVerifyT4InspectedFromWorkerSession(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	logFinished(t, dir, "T1", "w1")
	if err := AppendEvent(dir, Event{TS: "2026-09-12T03:00:00Z", Task: "T1", Kind: "inspected", Verdict: "rework", Session: "w1", Persona: "inspector", Tree: "t"}); err != nil {
		t.Fatalf("append inspected: %v", err)
	}
	fails := verifyAll(t, dir, "T1")
	if !fails["T4"] {
		t.Error("verify did not flag an inspected event from a worker session (T4)")
	}
}

func TestVerifyT5LandedWithoutInspectedPass(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T03:00:00Z", Task: "T1", Kind: "landed"}); err != nil {
		t.Fatalf("append landed: %v", err)
	}
	fails := verifyAll(t, dir, "T1")
	if !fails["T5"] {
		t.Error("verify did not flag a landed event without an inspected pass (T5)")
	}
}

func TestVerifyT8BadPersona(t *testing.T) {
	dir, err := initTask(t, []string{"exit 0"})
	if err != nil {
		t.Fatalf("initTask() error = %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T03:00:00Z", Task: "T1", Kind: "inspected", Verdict: "rework", Session: "i1", Persona: "worker", Tree: "t"}); err != nil {
		t.Fatalf("append inspected: %v", err)
	}
	fails := verifyAll(t, dir, "T1")
	if !fails["T8"] {
		t.Error("verify did not flag a bad persona (T8)")
	}
}

// TestVerifyT3FirstPassSurvivesLaterAttempt checks the T3 window: a legitimate
// pass recorded after its own finished event must not be invalidated by a
// later correction attempt's finished event. Each inspection uses the latest
// finished event before it.
func TestVerifyT3FirstPassSurvivesLaterAttempt(t *testing.T) {
	dir := buildCleanChain(t)
	rc := 0
	if err := AppendEvent(dir, Event{TS: "2099-01-01T00:00:00Z", Task: "T1", Kind: "finished", Attempt: "c1", Session: "w2", RC: &rc}); err != nil {
		t.Fatalf("append later finished: %v", err)
	}
	fails := verifyAll(t, dir, "T1")
	if fails["T3"] {
		t.Error("T3 flagged the first inspected pass even though a later finished event exists")
	}
}
