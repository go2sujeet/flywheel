package flywheel

import (
	"errors"
	"testing"
)

// appendPassed records the events a task needs to derive status passed.
func appendPassed(t *testing.T, dir, task string) {
	t.Helper()
	if err := AppendEvent(dir, Event{TS: "2026-09-14T10:00:00Z", Task: task, Kind: "planned", Brief: "b.txt"}); err != nil {
		t.Fatalf("append planned: %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-14T10:01:00Z", Task: task, Kind: "inspected", Verdict: "pass", Session: "lead-1"}); err != nil {
		t.Fatalf("append inspected: %v", err)
	}
}

// landedEvents returns the task's landed events, newest last.
func landedEvents(t *testing.T, dir, task string) []Event {
	t.Helper()
	events, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var out []Event
	for _, e := range events {
		if e.Task == task && e.Kind == "landed" {
			out = append(out, e)
		}
	}
	return out
}

func TestLandTaskSuccess(t *testing.T) {
	dir := t.TempDir()
	appendPassed(t, dir, "T1")
	if err := LandTask(dir, "T1", "abc1234", "merged"); err != nil {
		t.Fatalf("LandTask() error = %v", err)
	}
	landed := landedEvents(t, dir, "T1")
	if len(landed) != 1 {
		t.Fatalf("landed events = %d, want 1", len(landed))
	}
	if landed[0].Commit != "abc1234" || landed[0].Note != "merged" {
		t.Errorf("landed event = %+v, want commit abc1234 note merged", landed[0])
	}
	all, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	status := ""
	for _, ts := range Derive(all).Tasks {
		if ts.ID == "T1" {
			status = ts.Status
		}
	}
	if status != "landed" {
		t.Errorf("derived status = %q, want landed", status)
	}
}

func TestLandTaskRefusedWithoutPass(t *testing.T) {
	dir := t.TempDir()
	if err := AppendEvent(dir, Event{TS: "2026-09-14T10:00:00Z", Task: "T1", Kind: "planned", Brief: "b.txt"}); err != nil {
		t.Fatalf("append planned: %v", err)
	}
	err := LandTask(dir, "T1", "abc1234", "")
	if !IsRuleRefusal(err) {
		t.Fatalf("LandTask() error = %v, want a rule refusal", err)
	}
	var r *RuleRefusal
	if !errors.As(err, &r) || r.Rule != "T5" {
		t.Errorf("refusal = %v, want rule T5", err)
	}
	if len(landedEvents(t, dir, "T1")) != 0 {
		t.Error("landed event appended despite the refusal")
	}
}

func TestLandTaskBadCommit(t *testing.T) {
	dir := t.TempDir()
	appendPassed(t, dir, "T1")
	for _, commit := range []string{"", "abc12", "zzzzzzz", "abcdef1234567890abcdef1234567890abcdef12345678901"} {
		if err := LandTask(dir, "T1", commit, ""); err == nil {
			t.Errorf("LandTask(commit %q) accepted, want an error", commit)
		}
	}
	if len(landedEvents(t, dir, "T1")) != 0 {
		t.Error("landed event appended despite the bad commit")
	}
}

func TestLandTaskSameCommitNoOp(t *testing.T) {
	dir := t.TempDir()
	appendPassed(t, dir, "T1")
	if err := LandTask(dir, "T1", "abc1234", ""); err != nil {
		t.Fatalf("first LandTask() error = %v", err)
	}
	err := LandTask(dir, "T1", "abc1234", "again")
	if !errors.Is(err, ErrAlreadyLanded) {
		t.Fatalf("second LandTask() error = %v, want ErrAlreadyLanded", err)
	}
	if len(landedEvents(t, dir, "T1")) != 1 {
		t.Errorf("landed events = %d, want 1 (no-op must not append)", len(landedEvents(t, dir, "T1")))
	}
}

func TestLandTaskDifferentCommitRefused(t *testing.T) {
	dir := t.TempDir()
	appendPassed(t, dir, "T1")
	if err := LandTask(dir, "T1", "abc1234", ""); err != nil {
		t.Fatalf("first LandTask() error = %v", err)
	}
	err := LandTask(dir, "T1", "def5678", "")
	if !IsRuleRefusal(err) {
		t.Fatalf("second LandTask() error = %v, want a rule refusal", err)
	}
	var r *RuleRefusal
	if !errors.As(err, &r) || r.Rule != "T5" {
		t.Errorf("refusal = %v, want rule T5", err)
	}
	if len(landedEvents(t, dir, "T1")) != 1 {
		t.Errorf("landed events = %d, want 1", len(landedEvents(t, dir, "T1")))
	}
}
