package flywheel

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAppendReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	e := Event{
		TS:   "2026-09-12T00:00:00Z",
		Task: "T1",
		Kind: "planned",
		Owns: []string{"a.go"},
		Note: `back\slash "quoted" & <tag>`,
	}
	if err := AppendEvent(dir, e); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("ReadEvents() = %d events, want 1", len(evs))
	}
	got := evs[0]
	if got.TS != e.TS || got.Task != e.Task || got.Kind != e.Kind {
		t.Errorf("round trip mismatch: got %v", got)
	}
	if got.Note != e.Note {
		t.Errorf("note = %q, want %q", got.Note, e.Note)
	}
	if len(got.Owns) != 1 || got.Owns[0] != "a.go" {
		t.Errorf("owns = %v, want [a.go]", got.Owns)
	}

	// HTML escaping is off: raw bytes carry < and & literally.
	b, err := os.ReadFile(filepath.Join(dir, ".flywheel", "events.jsonl"))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "<") || !strings.Contains(content, "&") {
		t.Errorf("note was HTML-escaped in the log: %q", content)
	}
	if strings.Contains(content, `\u003c`) || strings.Contains(content, `\u0026`) {
		t.Errorf("note was HTML-escaped in the log: %q", content)
	}
}

func TestAppendSetsTimestampWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := AppendEvent(dir, Event{TS: "", Task: "T1", Kind: "planned"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("ReadEvents() = %d events, want 1", len(evs))
	}
	if evs[0].TS == "" {
		t.Error("AppendEvent() did not stamp TS")
	}
}

func TestValidateRejectsBadTask(t *testing.T) {
	if err := Validate(Event{Task: "T1!", Kind: "planned"}); err == nil {
		t.Error("Validate() accepted task id with '!'")
	} else if !strings.Contains(err.Error(), "task") {
		t.Errorf("Validate() error = %v, want task message", err)
	}
	if err := Validate(Event{Task: "", Kind: "planned"}); err == nil {
		t.Error("Validate() accepted empty task id")
	}
}

func TestValidateRejectsUnknownKind(t *testing.T) {
	if err := Validate(Event{Task: "T1", Kind: "frobnicated"}); err == nil {
		t.Error("Validate() accepted unknown kind")
	} else if !strings.Contains(err.Error(), "kind") {
		t.Errorf("Validate() error = %v, want kind message", err)
	}
}

func TestValidateRejectsBadAttempt(t *testing.T) {
	if err := Validate(Event{Task: "T1", Kind: "dispatched", Attempt: "x1"}); err == nil {
		t.Error("Validate() accepted attempt x1")
	}
	if err := Validate(Event{Task: "T1", Kind: "dispatched", Attempt: "r"}); err == nil {
		t.Error("Validate() accepted attempt r (no digits)")
	}
	if err := Validate(Event{Task: "T1", Kind: "dispatched", Attempt: "c2"}); err != nil {
		t.Errorf("Validate() rejected valid attempt c2: %v", err)
	}
}

func TestValidateRejectsReviewedWithoutVerdict(t *testing.T) {
	if err := Validate(Event{Task: "T1", Kind: "reviewed"}); err == nil {
		t.Error("Validate() accepted reviewed event without verdict")
	} else if !strings.Contains(err.Error(), "verdict") {
		t.Errorf("Validate() error = %v, want verdict message", err)
	}
	if err := Validate(Event{Task: "T1", Kind: "reviewed", Verdict: "pass"}); err != nil {
		t.Errorf("Validate() rejected reviewed/pass: %v", err)
	}
}

func TestAppendTornLastLineGetsNewlinePrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".flywheel", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir .flywheel: %v", err)
	}
	// A torn write: a full record with no trailing newline.
	torn := `{"ts":"2026-09-12T00:00:00Z","task":"T0","kind":"planned"}`
	if err := os.WriteFile(path, []byte(torn), 0o644); err != nil {
		t.Fatalf("write torn line: %v", err)
	}

	if err := AppendEvent(dir, Event{TS: "2026-09-12T00:00:01Z", Task: "T1", Kind: "planned"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("ReadEvents() = %d events, want 2", len(evs))
	}
	if evs[0].Task != "T0" || evs[1].Task != "T1" {
		t.Errorf("events spliced by torn write: %v", evs)
	}
}

func TestReadEventsConflictMarkerNamesLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".flywheel", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir .flywheel: %v", err)
	}
	if err := os.WriteFile(path, []byte("<<<<<<< HEAD\n"), 0o644); err != nil {
		t.Fatalf("write conflict marker: %v", err)
	}

	_, err := ReadEvents(dir)
	if err == nil {
		t.Fatal("ReadEvents() accepted a conflict marker")
	}
	if !strings.Contains(err.Error(), "unresolved merge conflict") {
		t.Errorf("ReadEvents() error = %v, want 'unresolved merge conflict'", err)
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("ReadEvents() error = %v, want line number", err)
	}
}

func TestParseStrictRejectsUnknownFieldLenientAccepts(t *testing.T) {
	line := []byte(`{"ts":"2026-09-12T00:00:00Z","task":"T1","kind":"planned","bogus":1}`)

	if _, err := ParseEvents(bytes.NewReader(line), true); err == nil {
		t.Error("strict ParseEvents() accepted unknown field")
	} else if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("strict ParseEvents() error = %v, want unknown field bogus", err)
	}

	evs, err := ParseEvents(bytes.NewReader(line), false)
	if err != nil {
		t.Fatalf("lenient ParseEvents() error = %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("lenient ParseEvents() = %d events, want 1", len(evs))
	}
	if evs[0].Task != "T1" || evs[0].Kind != "planned" {
		t.Errorf("lenient parse mismatch: %v", evs[0])
	}
}

func TestAppendConcurrent(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make([]error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = AppendEvent(dir, Event{TS: "2026-09-12T00:00:00Z", Task: fmt.Sprintf("T%d", i), Kind: "planned"})
		}(i)
	}
	wg.Wait()

	for i := 0; i < 50; i++ {
		if errs[i] != nil {
			t.Errorf("goroutine %d AppendEvent() error = %v", i, errs[i])
		}
	}

	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	if len(evs) != 50 {
		t.Fatalf("ReadEvents() = %d events, want 50", len(evs))
	}
	seen := map[string]bool{}
	for _, e := range evs {
		seen[e.Task] = true
		if err := Validate(e); err != nil {
			t.Errorf("event %v failed validation: %v", e, err)
		}
	}
	if len(seen) != 50 {
		t.Errorf("distinct tasks = %d, want 50", len(seen))
	}
}
