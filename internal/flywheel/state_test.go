package flywheel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stateJSON(st State) string {
	b, err := json.Marshal(st)
	if err != nil {
		return ""
	}
	return string(b)
}

func concat(a, b []Event) []Event {
	out := make([]Event, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

func withRC(e Event, rc int) Event {
	p := new(int)
	*p = rc
	e.RC = p
	return e
}

func findTask(st State, id string) (TaskState, bool) {
	for _, ts := range st.Tasks {
		if ts.ID == id {
			return ts, true
		}
	}
	return TaskState{}, false
}

func TestDeriveStatusMapping(t *testing.T) {
	events := []Event{
		{TS: "2026-09-12T00:00:00Z", Task: "T-plan", Kind: "planned"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-disp", Kind: "dispatched", Attempt: "r1"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-run", Kind: "started"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-fin", Kind: "finished"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-pass", Kind: "reviewed", Verdict: "pass"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-corr", Kind: "reviewed", Verdict: "correct"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-rej", Kind: "reviewed", Verdict: "reject"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-block", Kind: "blocked"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-land", Kind: "landed"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-amend", Kind: "planned", Brief: "b1"},
		{TS: "2026-09-12T00:00:01Z", Task: "T-amend", Kind: "amended", Brief: "b2"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-attr", Kind: "dispatched", Session: "s1", Model: "m1", Attempt: "r1", Reason: "first"},
		{TS: "2026-09-12T00:00:01Z", Task: "T-attr", Kind: "finished", Session: "s2", Reason: "ok"},
		{TS: "2026-09-12T00:00:00Z", Task: "T-att2", Kind: "dispatched", Attempt: "r1"},
		{TS: "2026-09-12T00:00:01Z", Task: "T-att2", Kind: "dispatched", Attempt: "c1"},
	}
	events[11] = withRC(events[11], 3)

	st := Derive(events)
	for id, want := range map[string]string{
		"T-plan":  "planned",
		"T-disp":  "dispatched",
		"T-run":   "running",
		"T-fin":   "finished",
		"T-pass":  "passed",
		"T-corr":  "needs-correction",
		"T-rej":   "rejected",
		"T-block": "blocked",
		"T-land":  "landed",
	} {
		ts, ok := findTask(st, id)
		if !ok {
			t.Errorf("task %s missing from derived state", id)
			continue
		}
		if ts.Status != want {
			t.Errorf("%s status = %q, want %q", id, ts.Status, want)
		}
	}

	amend, ok := findTask(st, "T-amend")
	if !ok {
		t.Fatal("T-amend missing from derived state")
	}
	if amend.Status != "planned" {
		t.Errorf("amended task status = %q, want planned (amended does not change status)", amend.Status)
	}
	if amend.Brief != "b2" {
		t.Errorf("T-amend brief = %q, want b2 (from latest planned/amended)", amend.Brief)
	}

	attr, ok := findTask(st, "T-attr")
	if !ok {
		t.Fatal("T-attr missing from derived state")
	}
	if attr.Status != "finished" || attr.Session != "s2" || attr.Model != "m1" ||
		attr.Attempt != "r1" || attr.Reason != "ok" || attr.Attempts != 1 {
		t.Errorf("T-attr fields wrong: %v", attr)
	}
	if attr.RC == nil || *attr.RC != 3 {
		t.Errorf("T-attr rc = %v, want 3", attr.RC)
	}
	if attr.UpdatedAt != "2026-09-12T00:00:01Z" {
		t.Errorf("T-attr updated = %q, want latest TS", attr.UpdatedAt)
	}

	att2, ok := findTask(st, "T-att2")
	if !ok {
		t.Fatal("T-att2 missing from derived state")
	}
	if att2.Attempts != 2 {
		t.Errorf("T-att2 attempts = %d, want 2 (two dispatched events)", att2.Attempts)
	}
	if att2.Attempt != "c1" {
		t.Errorf("T-att2 attempt = %q, want c1 (last non-empty)", att2.Attempt)
	}
}

func TestDeriveOrderIndependent(t *testing.T) {
	a := []Event{
		{TS: "2026-09-12T00:00:00Z", Task: "A", Kind: "planned"},
		{TS: "2026-09-12T00:00:00Z", Task: "B", Kind: "started"},
		{TS: "2026-09-12T00:00:00Z", Task: "C", Kind: "finished"},
	}
	b := []Event{
		{TS: "2026-09-12T00:00:00Z", Task: "B", Kind: "finished"},
		{TS: "2026-09-12T00:00:00Z", Task: "C", Kind: "reviewed", Verdict: "pass"},
	}
	ab := stateJSON(Derive(concat(a, b)))
	ba := stateJSON(Derive(concat(b, a)))
	if ab != ba {
		t.Errorf("Derive(a+b) != Derive(b+a) with a TS tie:\n%s\nvs\n%s", ab, ba)
	}
}

func TestDeriveOrderingSameSecond(t *testing.T) {
	// dispatched, started and finished for T1 share one second-precision TS;
	// appended in reverse order they must still derive finished.
	events := []Event{
		{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "finished"},
		{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "started"},
		{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "dispatched", Attempt: "r1"},
	}
	st := Derive(events)
	ts, ok := findTask(st, "T1")
	if !ok {
		t.Fatal("T1 missing from derived state")
	}
	if ts.Status != "finished" {
		t.Errorf("T1 status = %q, want finished (started must not win over finished)", ts.Status)
	}
	if ts.Attempts != 1 {
		t.Errorf("T1 attempts = %d, want 1", ts.Attempts)
	}
}

func TestDeriveOrderingNanoVsSecond(t *testing.T) {
	// A nanosecond-precision TS in the same second sorts after the
	// second-precision one even though '.' sorts before 'Z' as text.
	a := []Event{
		{TS: "2026-09-12T00:00:00.500000000Z", Task: "T1", Kind: "dispatched", Attempt: "r1"},
		{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "finished"},
	}
	st := Derive(a)
	ts, ok := findTask(st, "T1")
	if !ok {
		t.Fatal("T1 missing from derived state")
	}
	if ts.Status != "dispatched" {
		t.Errorf("T1 status = %q, want dispatched (nanosecond event is later)", ts.Status)
	}

	// And derivation is still order independent.
	b := []Event{
		{TS: "2026-09-12T00:00:00.250000000Z", Task: "T2", Kind: "started"},
		{TS: "2026-09-12T00:00:00.100000000Z", Task: "T2", Kind: "planned"},
	}
	ab := stateJSON(Derive(concat(a, b)))
	ba := stateJSON(Derive(concat(b, a)))
	if ab != ba {
		t.Errorf("Derive(a+b) != Derive(b+a) with nano/second mix:\n%s\nvs\n%s", ab, ba)
	}
}

func TestWriteStateReplacesOnlyMarkedBlock(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "flywheel.md")
	md := "before\n<!-- flywheel:status:start -->\nOLD TABLE\n<!-- flywheel:status:end -->\nafter\n"
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatalf("write flywheel.md: %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "planned", Model: "deepseek"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T00:00:01Z", Task: "T1", Kind: "started"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	st, err := WriteState(dir)
	if err != nil {
		t.Fatalf("WriteState() error = %v", err)
	}
	if st.Version != 2 {
		t.Errorf("state version = %d, want 2", st.Version)
	}

	b, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read flywheel.md: %v", err)
	}
	content := string(b)
	if !strings.HasPrefix(content, "before\n<!-- flywheel:status:start -->\n") {
		t.Errorf("text before the marked block changed:\n%q", content)
	}
	if !strings.HasSuffix(content, "\n<!-- flywheel:status:end -->\nafter\n") {
		t.Errorf("text after the marked block changed:\n%q", content)
	}
	if strings.Contains(content, "OLD TABLE") {
		t.Error("old table body was not replaced")
	}
	if !strings.Contains(content, "| T1 | running | 0 |") {
		t.Errorf("status block missing T1 row:\n%q", content)
	}
	if !strings.Contains(content, "deepseek") {
		t.Errorf("status block missing model:\n%q", content)
	}

	// Same input gives byte-identical output on the second run.
	if _, err := WriteState(dir); err != nil {
		t.Fatalf("WriteState() second run error = %v", err)
	}
	b2, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("re-read flywheel.md: %v", err)
	}
	if string(b2) != string(b) {
		t.Error("WriteState() output changed between runs")
	}
}

func TestWriteStateAppendsBlockWhenMarkersMissing(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "flywheel.md")
	md := "custom content\nsecond line\n"
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatalf("write flywheel.md: %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "planned"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	if _, err := WriteState(dir); err != nil {
		t.Fatalf("WriteState() error = %v", err)
	}
	b, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read flywheel.md: %v", err)
	}
	content := string(b)
	if !strings.HasPrefix(content, "custom content\nsecond line\n") {
		t.Errorf("existing markdown text changed:\n%q", content)
	}
	if !strings.Contains(content, statusStartMarker) || !strings.Contains(content, statusEndMarker) {
		t.Error("status markers were not appended")
	}
	if !strings.Contains(content, "| T1 | planned | 0 |") {
		t.Errorf("status block missing T1 row:\n%q", content)
	}
}

func TestWriteStateCreatesMarkdownWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := AppendEvent(dir, Event{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "landed"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	if _, err := WriteState(dir); err != nil {
		t.Fatalf("WriteState() error = %v", err)
	}
	mdPath := filepath.Join(dir, "flywheel.md")
	b, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read flywheel.md: %v", err)
	}
	content := string(b)
	if !strings.HasPrefix(content, statusStartMarker) {
		t.Errorf("flywheel.md does not start with the status block:\n%q", content)
	}
	if !strings.Contains(content, "| T1 | landed | 0 |") {
		t.Errorf("status block missing T1 row:\n%q", content)
	}
}
