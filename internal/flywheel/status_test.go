package flywheel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func statusNow(t *testing.T) time.Time {
	now, err := time.Parse(time.RFC3339Nano, "2026-09-14T01:00:00Z")
	if err != nil {
		t.Fatalf("parse test now: %v", err)
	}
	return now
}

// statusFixture builds an event log with one task per derived status, one
// stale attempt, one andon unit, and a staffed event as the latest of all.
func statusFixture(t *testing.T) string {
	dir := t.TempDir()
	events := []Event{
		{TS: "2026-09-14T00:00:00Z", Task: "t-planned", Kind: "planned", Brief: "b.txt"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-run", Kind: "planned", Brief: "b.txt"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-run", Kind: "dispatched", Attempt: "r1"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-run", Kind: "started"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-stale", Kind: "planned", Brief: "b.txt"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-stale", Kind: "dispatched", Attempt: "r1"},
		{TS: "2026-09-14T00:01:00Z", Task: "t-stale", Kind: "dispatched", Attempt: "r2"},
		{TS: "2026-09-14T00:02:00Z", Task: "t-stale", Kind: "finished", Attempt: "r1"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-silent", Kind: "planned", Brief: "b.txt"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-silent", Kind: "dispatched", Attempt: "r1"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-pass", Kind: "planned", Brief: "b.txt"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-pass", Kind: "dispatched", Attempt: "r1"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-pass", Kind: "started"},
		{TS: "2026-09-14T00:03:00Z", Task: "t-pass", Kind: "finished", Attempt: "r1"},
		{TS: "2026-09-14T00:05:00Z", Task: "t-pass", Kind: "inspected", Attempt: "r1", Verdict: "pass"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-land", Kind: "planned", Brief: "b.txt"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-land", Kind: "dispatched", Attempt: "r1"},
		{TS: "2026-09-14T00:00:00Z", Task: "t-land", Kind: "started"},
		{TS: "2026-09-14T00:04:00Z", Task: "t-land", Kind: "finished", Attempt: "r1"},
		{TS: "2026-09-14T00:06:00Z", Task: "t-land", Kind: "landed", Commit: "abc"},
		{TS: "2026-09-14T00:07:00Z", Kind: "staffed", Persona: "lead", Session: "s1"},
	}
	for _, e := range events {
		if err := AppendEvent(dir, e); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}
	// A live run file each for t-run and t-stale keeps their units off the
	// andon; t-silent has no run file, so its unit is silent.
	runLine := `{"type":"step_finish","sessionID":"s1","part":{"type":"step_finish","reason":"stop"}}` + "\n"
	for _, name := range []string{"t-run.r1.jsonl", "t-stale.r2.jsonl"} {
		run := filepath.Join(dir, ".flywheel", "runs", name)
		if err := os.MkdirAll(filepath.Dir(run), 0o755); err != nil {
			t.Fatalf("mkdir runs: %v", err)
		}
		if err := os.WriteFile(run, []byte(runLine), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestStatusFixture(t *testing.T) {
	dir := statusFixture(t)
	now := statusNow(t)
	rep, err := Status(dir, now)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if rep.Factory != filepath.Base(dir) {
		t.Errorf("factory = %q, want %q", rep.Factory, filepath.Base(dir))
	}
	if rep.Tasks.Total != 6 {
		t.Errorf("tasks total = %d, want 6", rep.Tasks.Total)
	}
	if rep.Tasks.Planned != 1 || rep.Tasks.Dispatched != 2 || rep.Tasks.Running != 1 {
		t.Errorf("planned/dispatched/running = %d/%d/%d, want 1/2/1", rep.Tasks.Planned, rep.Tasks.Dispatched, rep.Tasks.Running)
	}
	if rep.Tasks.Finished != 0 || rep.Tasks.Passed != 1 || rep.Tasks.NeedsCorrection != 0 {
		t.Errorf("finished/passed/needs-correction = %d/%d/%d, want 0/1/0", rep.Tasks.Finished, rep.Tasks.Passed, rep.Tasks.NeedsCorrection)
	}
	if rep.Tasks.Rejected != 0 || rep.Tasks.Blocked != 0 || rep.Tasks.Landed != 1 {
		t.Errorf("rejected/blocked/landed = %d/%d/%d, want 0/0/1", rep.Tasks.Rejected, rep.Tasks.Blocked, rep.Tasks.Landed)
	}
	if rep.Attempts.Live != 3 {
		t.Errorf("attempts live = %d, want 3", rep.Attempts.Live)
	}
	if rep.Attempts.Stale != 1 {
		t.Errorf("attempts stale = %d, want 1", rep.Attempts.Stale)
	}
	if rep.LastEventAt == nil || rep.LastEventAt.TS != "2026-09-14T00:07:00Z" || rep.LastEventAt.Age != 3180 {
		t.Errorf("last_event_at = %+v, want 00:07:00Z age 3180", rep.LastEventAt)
	}
	if rep.LastProgressAt == nil || rep.LastProgressAt.TS != "2026-09-14T00:06:00Z" || rep.LastProgressAt.Age != 3240 {
		t.Errorf("last_progress_at = %+v, want 00:06:00Z age 3240", rep.LastProgressAt)
	}
	if rep.Andon != 1 {
		t.Errorf("andon = %d, want 1 (only the silent unit)", rep.Andon)
	}
}

func TestStatusJSONRoundTrip(t *testing.T) {
	dir := statusFixture(t)
	rep, err := Status(dir, statusNow(t))
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got StatusReport
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Factory != rep.Factory || got.Tasks != rep.Tasks || got.Attempts != rep.Attempts || got.Andon != rep.Andon {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, rep)
	}
	if got.LastEventAt == nil || *got.LastEventAt != *rep.LastEventAt {
		t.Errorf("last_event_at round-trip = %+v, want %+v", got.LastEventAt, rep.LastEventAt)
	}
	if got.LastProgressAt == nil || *got.LastProgressAt != *rep.LastProgressAt {
		t.Errorf("last_progress_at round-trip = %+v, want %+v", got.LastProgressAt, rep.LastProgressAt)
	}
}

func TestStatusEmptyFactory(t *testing.T) {
	rep, err := Status(t.TempDir(), statusNow(t))
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if rep.Tasks.Total != 0 {
		t.Errorf("tasks total = %d, want 0", rep.Tasks.Total)
	}
	if rep.LastEventAt != nil {
		t.Errorf("last_event_at = %+v, want none", rep.LastEventAt)
	}
	if rep.LastProgressAt != nil {
		t.Errorf("last_progress_at = %+v, want none", rep.LastProgressAt)
	}
	if rep.Attempts.Live != 0 || rep.Attempts.Stale != 0 {
		t.Errorf("attempts = %+v, want 0/0", rep.Attempts)
	}
	if rep.Andon != 0 {
		t.Errorf("andon = %d, want 0", rep.Andon)
	}
}
