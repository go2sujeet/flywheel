package flywheel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testNow is the fixed clock the fixture refresh tests are drawn at.
var testNow time.Time

func fixtureNow(t *testing.T) time.Time {
	now, err := time.Parse(time.RFC3339Nano, "2026-09-12T01:00:00Z")
	if err != nil {
		t.Fatalf("parse test now: %v", err)
	}
	return now
}

func unitBy(units []Unit, task string) (Unit, bool) {
	for _, u := range units {
		if u.Task == task {
			return u, true
		}
	}
	return Unit{}, false
}

// andonHas reports whether the andon lists the given task.
func andonHas(a []Andon, task string) bool {
	for _, x := range a {
		if x.Task == task {
			return true
		}
	}
	return false
}

func TestStageOf(t *testing.T) {
	if stageOf("planned") != "planned" {
		t.Error("stageOf planned != planned")
	}
	if stageOf("dispatched") != "building" || stageOf("running") != "building" {
		t.Error("stageOf in-flight != building")
	}
	if stageOf("finished") != "finished" || stageOf("rejected") != "finished" {
		t.Error("stageOf finished/rejected != finished")
	}
	if stageOf("blocked") != "blocked" {
		t.Error("stageOf blocked != blocked")
	}
	if stageOf("landed") != "landed" {
		t.Error("stageOf landed != landed")
	}
}

func TestClassifyRun(t *testing.T) {
	if classifyRun(true, 1, 0, 0, false, "stop", 100, 0) != "done" {
		t.Error("done unit not classified done")
	}
	if classifyRun(false, 1, 0, 0, true, "stop", 100, 0) != "provider-error" {
		t.Error("provider-error not classified provider-error")
	}
	if classifyRun(false, 1, 0, 0, false, "length", 100, 0) != "capped" {
		t.Error("capped not classified capped")
	}
	if classifyRun(false, 1, 0, 0, false, "stop", 0, 120) != "silent" {
		t.Error("empty old run not classified silent")
	}
	if classifyRun(false, 1, 0, 0, false, "stop", 100, 700) != "stalled" {
		t.Error("no-growth 700s not classified stalled")
	}
	if classifyRun(false, 1, 0, 0, false, "stop", 100, 400) != "long-step" {
		t.Error("no-growth 400s not classified long-step")
	}
	if classifyRun(false, 10, 3, 0, false, "stop", 100, 0) != "exploring" {
		t.Error("10 steps, 3 reads, 0 edits not classified exploring")
	}
	if classifyRun(false, 10, 3, 1, false, "stop", 100, 0) != "running" {
		t.Error("exploring with an edit not classified running")
	}
	if classifyRun(false, 1, 0, 0, false, "stop", 100, 0) != "running" {
		t.Error("healthy run not classified running")
	}
	// A capped or provider-error run still records a finished event, so the
	// signal wins over done: the unit must reach the andon.
	if classifyRun(true, 5, 1, 0, false, "length", 200, 0) != "capped" {
		t.Error("finished capped run not classified capped")
	}
	if classifyRun(true, 5, 1, 0, true, "stop", 200, 0) != "provider-error" {
		t.Error("finished provider-error run not classified provider-error")
	}
	if classifyRun(true, 5, 1, 1, false, "stop", 200, 0) != "done" {
		t.Error("clean finished run not classified done")
	}
}

func TestLiveRun(t *testing.T) {
	for _, s := range []string{"running", "exploring", "long-step", "silent", "stalled"} {
		if !liveRun(s) {
			t.Errorf("liveRun(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"capped", "provider-error", "done", "landed", "waiting"} {
		if liveRun(s) {
			t.Errorf("liveRun(%q) = true, want false", s)
		}
	}
}

func TestFixtureRefresh(t *testing.T) {
	stalled := filepath.Join("testdata", "factory", ".flywheel", "runs", "stalled.r1.jsonl")
	mt, err := time.Parse(time.RFC3339Nano, "2026-09-12T00:30:00Z")
	if err != nil {
		t.Fatalf("parse stalled mtime: %v", err)
	}
	if err := os.Chtimes(stalled, mt, mt); err != nil {
		t.Fatalf("set stalled mtime: %v", err)
	}

	var w Watcher = NewWatcher()
	fl, err := w.Refresh("testdata/factory", fixtureNow(t))
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fl.Lines) != 1 || fl.Lines[0].Name != "default" {
		t.Errorf("lines = %v, want default worker", fl.Lines)
	}
	if fl.Lines[0].Busy != 3 {
		t.Errorf("default line busy = %d, want 3 live in-flight (capped and provider-error are dead)", fl.Lines[0].Busy)
	}
	if fl.Staffing.Lead != "not registered" {
		t.Errorf("staffing lead = %q, want not registered", fl.Staffing.Lead)
	}

	want := map[string]string{
		"done":           "done",
		"running":        "running",
		"exploring":      "exploring",
		"stalled":        "stalled",
		"capped":         "capped",
		"provider-error": "provider-error",
		"planned":        "waiting",
	}
	for task, state := range want {
		u, ok := unitBy(fl.Units, task)
		if !ok {
			t.Errorf("unit %s missing", task)
			continue
		}
		if u.RunState != state {
			t.Errorf("unit %s run state = %q, want %q", task, u.RunState, state)
		}
	}
	if u, ok := unitBy(fl.Units, "done"); ok && u.Stage != "landed" {
		t.Errorf("done stage = %q, want landed", u.Stage)
	}
	if u, ok := unitBy(fl.Units, "planned"); ok && u.Stage != "planned" {
		t.Errorf("planned stage = %q, want planned", u.Stage)
	}

	if len(fl.Andon) != 3 {
		t.Errorf("andon = %v, want exactly the 3 bad units", fl.Andon)
	}
	for _, a := range fl.Andon {
		if a.State != "stalled" && a.State != "capped" && a.State != "provider-error" {
			t.Errorf("andon holds %s %s, want a bad unit", a.Task, a.State)
		}
	}
	// The finished capped and provider-error units must reach the andon, while
	// the clean finished (done) unit must not.
	if !andonHas(fl.Andon, "capped") {
		t.Errorf("andon missing capped: %v", fl.Andon)
	}
	if !andonHas(fl.Andon, "provider-error") {
		t.Errorf("andon missing provider-error: %v", fl.Andon)
	}
	if andonHas(fl.Andon, "done") {
		t.Errorf("clean done unit wrongly on andon: %v", fl.Andon)
	}

	if fl.Output.LandedToday != 1 {
		t.Errorf("landed today = %d, want 1", fl.Output.LandedToday)
	}
	if fl.Output.Finished != 1 {
		t.Errorf("finished = %d, want 1", fl.Output.Finished)
	}
	if fl.Output.HasReviews {
		t.Errorf("has reviews = true, want false (no reviewed events)")
	}
	if fl.Output.Tokens != 260 {
		t.Errorf("tokens = %d, want 260", fl.Output.Tokens)
	}
	if fl.Output.Cost != 0.004 {
		t.Errorf("cost = %g, want 0.004", fl.Output.Cost)
	}
}

func TestIncrementalReadsOnlyAppended(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join("testdata", "factory", ".flywheel")
	copyFile(filepath.Join(src, "events.jsonl"), filepath.Join(tmp, ".flywheel", "events.jsonl"), t)
	copyFile(filepath.Join(src, "runs", "running.r1.jsonl"), filepath.Join(tmp, ".flywheel", "runs", "running.r1.jsonl"), t)

	now := fixtureNow(t)
	var w Watcher = NewWatcher()
	fl1, err := w.Refresh(tmp, now)
	if err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	evBytes := w.EventsBytes
	runBytes := w.RunBytes

	eventsPath := filepath.Join(tmp, ".flywheel", "events.jsonl")
	runPath := filepath.Join(tmp, ".flywheel", "runs", "running.r1.jsonl")
	evSize1, _ := os.Stat(eventsPath)
	runSize1, _ := os.Stat(runPath)

	// Append one finished event and one run line.
	toks := new(Tokens)
	*toks = Tokens{Input: 30, Output: 10, Reasoning: 2}
	if err := AppendEvent(tmp, Event{TS: "2026-09-12T00:20:00Z", Task: "extra", Kind: "finished", Attempt: "r1", Steps: 1, Tokens: toks, Cost: 0.001}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	appendLine(runPath, `{"type":"step_finish","sessionID":"ses_running_0000000001","part":{"type":"step_finish","reason":"stop","tokens":{"input":5,"output":1,"reasoning":0,"cache":{"read":10,"write":0}},"cost":0.0001}}`, t)

	evSize2, _ := os.Stat(eventsPath)
	runSize2, _ := os.Stat(runPath)

	fl2, err := w.Refresh(tmp, now)
	if err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}
	if w.EventsBytes-evBytes != evSize2.Size()-evSize1.Size() {
		t.Errorf("events bytes reread = %d, want %d", w.EventsBytes-evBytes, evSize2.Size()-evSize1.Size())
	}
	if w.RunBytes-runBytes != runSize2.Size()-runSize1.Size() {
		t.Errorf("run bytes reread = %d, want %d", w.RunBytes-runBytes, runSize2.Size()-runSize1.Size())
	}
	if fl2.Output.Finished != fl1.Output.Finished+1 {
		t.Errorf("finished count after append = %d, want %d", fl2.Output.Finished, fl1.Output.Finished+1)
	}
	if u, ok := unitBy(fl2.Units, "extra"); !ok || u.RunState != "done" {
		t.Errorf("extra unit run state = %v, want done", u.RunState)
	}
}

func TestPartialRunLineWaitsForNewline(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join("testdata", "factory", ".flywheel")
	copyFile(filepath.Join(src, "events.jsonl"), filepath.Join(tmp, ".flywheel", "events.jsonl"), t)
	runPath := filepath.Join(tmp, ".flywheel", "runs", "running.r1.jsonl")
	copyFile(filepath.Join(src, "runs", "running.r1.jsonl"), runPath, t)

	now := fixtureNow(t)
	var w Watcher = NewWatcher()
	if _, err := w.Refresh(tmp, now); err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	rel := ".flywheel/runs/running.r1.jsonl"
	wantOff := w.runOff[rel]
	wantSteps := w.runSteps[rel]
	size1 := fileSize(t, runPath)

	line := `{"type":"step_finish","sessionID":"ses_running_0000000001","part":{"type":"step_finish","reason":"stop","tokens":{"input":5,"output":1,"reasoning":0,"cache":{"read":10,"write":0}},"cost":0.0001}}`
	half := len(line) / 2
	appendBytes(runPath, []byte(line[:half]), t)

	fl, err := w.Refresh(tmp, now)
	if err != nil {
		t.Fatalf("partial Refresh() error = %v", err)
	}
	if w.runSteps[rel] != wantSteps {
		t.Errorf("steps after partial line = %d, want %d unchanged", w.runSteps[rel], wantSteps)
	}
	if w.runOff[rel] != wantOff {
		t.Errorf("run offset after partial line = %d, want %d unchanged", w.runOff[rel], wantOff)
	}
	if w.RunBytes != size1 {
		t.Errorf("RunBytes after partial line = %d, want %d", w.RunBytes, size1)
	}

	appendBytes(runPath, []byte(line[half:]+"\n"), t)
	fl, err = w.Refresh(tmp, now)
	if err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}
	size2 := fileSize(t, runPath)
	if w.runSteps[rel] != wantSteps+1 {
		t.Errorf("steps after completed line = %d, want %d", w.runSteps[rel], wantSteps+1)
	}
	if w.RunBytes != size2 {
		t.Errorf("RunBytes after completed line = %d, want %d (file size)", w.RunBytes, size2)
	}
	_ = fl
}

func TestPartialEventLineWaitsForNewline(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join("testdata", "factory", ".flywheel")
	eventsPath := filepath.Join(tmp, ".flywheel", "events.jsonl")
	copyFile(filepath.Join(src, "events.jsonl"), eventsPath, t)
	copyFile(filepath.Join(src, "runs", "done.r1.jsonl"), filepath.Join(tmp, ".flywheel", "runs", "done.r1.jsonl"), t)

	now := fixtureNow(t)
	var w Watcher = NewWatcher()
	if _, err := w.Refresh(tmp, now); err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	evs := append([]Event{}, w.events...)
	size1 := fileSize(t, eventsPath)

	line := `{"ts":"2026-09-12T00:40:00Z","task":"extra","kind":"dispatched","attempt":"r1","adapter":"opencode","model":"m","path":".flywheel/runs/done.r1.jsonl"}`
	half := len(line) / 2
	appendBytes(eventsPath, []byte(line[:half]), t)

	if _, err := w.Refresh(tmp, now); err != nil {
		t.Fatalf("partial Refresh() error = %v", err)
	}
	if len(w.events) != len(evs) {
		t.Errorf("events after partial line = %d, want %d unchanged", len(w.events), len(evs))
	}
	if w.EventsBytes != size1 {
		t.Errorf("EventsBytes after partial line = %d, want %d", w.EventsBytes, size1)
	}

	appendBytes(eventsPath, []byte(line[half:]+"\n"), t)
	fl, err := w.Refresh(tmp, now)
	if err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}
	size2 := fileSize(t, eventsPath)
	if len(w.events) != len(evs)+1 {
		t.Errorf("events after completed line = %d, want %d", len(w.events), len(evs)+1)
	}
	if w.EventsBytes != size2 {
		t.Errorf("EventsBytes after completed line = %d, want %d (file size)", w.EventsBytes, size2)
	}
	if u, ok := unitBy(fl.Units, "extra"); !ok || u.Stage != "building" {
		t.Errorf("extra unit stage = %v, want building (derived from completed event)", u.Stage)
	}
}

func fileSize(t *testing.T, path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}

func appendBytes(path string, b []byte, t *testing.T) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := f.Write(b); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

func copyFile(src, dst string, t *testing.T) {
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func appendLine(path, line string, t *testing.T) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := f.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}
