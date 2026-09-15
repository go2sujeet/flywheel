package flywheel

import (
	"fmt"
	"path/filepath"
	"time"
)

// StatusReport is the deterministic factory summary: one number per group,
// derived only from the event log (via Derive) and the factory floor's run
// states and andon.
type StatusReport struct {
	Factory        string         `json:"factory"`
	Tasks          StatusTasks    `json:"tasks"`
	Attempts       StatusAttempts `json:"attempts"`
	LastEventAt    *LastEvent     `json:"last_event_at,omitempty"`
	LastProgressAt *LastEvent     `json:"last_progress_at,omitempty"`
	Andon          int            `json:"andon"`
}

// StatusTasks counts the tasks in each derived status.
type StatusTasks struct {
	Total           int `json:"total"`
	Planned         int `json:"planned"`
	Dispatched      int `json:"dispatched"`
	Running         int `json:"running"`
	Finished        int `json:"finished"`
	Passed          int `json:"passed"`
	NeedsCorrection int `json:"needs-correction"`
	Rejected        int `json:"rejected"`
	Blocked         int `json:"blocked"`
	Landed          int `json:"landed"`
}

// StatusAttempts summarises the attempts: live counts the tasks whose current
// attempt is dispatched or running; stale totals the stale entries across
// tasks.
type StatusAttempts struct {
	Live  int `json:"live"`
	Stale int `json:"stale"`
}

// LastEvent is one timestamp plus its age in whole seconds from now.
type LastEvent struct {
	TS  string `json:"ts"`
	Age int    `json:"age"`
}

// Status is a pure read of the factory: the event log via Derive plus the
// floor's run states and andon. It writes nothing and calls no model.
func Status(dir string, now time.Time) (StatusReport, error) {
	var w Watcher = NewWatcher()
	fl, err := w.Refresh(dir, now)
	if err != nil {
		return StatusReport{}, fmt.Errorf("status %s: %w", dir, err)
	}
	st := Derive(w.events)
	var rep StatusReport
	rep.Factory = filepath.Base(dir)
	for _, ts := range st.Tasks {
		rep.Tasks.Total++
		switch ts.Status {
		case "planned":
			rep.Tasks.Planned++
		case "dispatched":
			rep.Tasks.Dispatched++
		case "running":
			rep.Tasks.Running++
		case "finished":
			rep.Tasks.Finished++
		case "passed":
			rep.Tasks.Passed++
		case "needs-correction":
			rep.Tasks.NeedsCorrection++
		case "rejected":
			rep.Tasks.Rejected++
		case "blocked":
			rep.Tasks.Blocked++
		case "landed":
			rep.Tasks.Landed++
		}
		if ts.Attempt == "" {
			continue
		}
		if ts.Status == "dispatched" || ts.Status == "running" {
			rep.Attempts.Live++
		}
		rep.Attempts.Stale += len(ts.Stale)
	}
	rep.LastEventAt = latestEvent(w.events, now, func(e Event) bool { return true })
	rep.LastProgressAt = latestEvent(w.events, now, func(e Event) bool {
		return e.Kind == "landed" || e.Kind == "inspected" && e.Verdict == "pass"
	})
	rep.Andon = len(fl.Andon)
	return rep, nil
}

// latestEvent returns the newest event matching match as LastEvent (ts plus its
// age in seconds from now), or nil when no event matches.
func latestEvent(events []Event, now time.Time, match func(Event) bool) *LastEvent {
	have := false
	var best time.Time
	ts := ""
	for _, e := range events {
		if !match(e) {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, e.TS)
		if err != nil {
			continue
		}
		if !have || t.After(best) {
			best = t
			ts = e.TS
			have = true
		}
	}
	if !have {
		return nil
	}
	p := new(LastEvent)
	*p = LastEvent{TS: ts, Age: ageOfTime(best, now)}
	return p
}
