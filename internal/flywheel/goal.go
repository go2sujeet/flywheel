package flywheel

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// GoalSpec is one goal: its identity, title, acceptance gates, required tasks
// and status. A goal event carries one GoalSpec; the latest event per id wins.
type GoalSpec struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Acceptance []string `json:"acceptance,omitempty"`
	Required   []string `json:"required,omitempty"`
	Status     string   `json:"status"`
}

// goalStatuses is the set of goal statuses understood by Validate and Goals.
var goalStatuses = map[string]bool{
	"active":    true,
	"met":       true,
	"failed":    true,
	"abandoned": true,
}

// GoalView is one goal's derived state: its latest spec plus the progress of
// its required tasks.
type GoalView struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Acceptance []string `json:"acceptance,omitempty"`
	Required   []string `json:"required,omitempty"`
	Status     string   `json:"status"`
	CreatedAt  string   `json:"created_at"`
	Total      int      `json:"total"`
	Accepted   int      `json:"accepted"`
	InFlight   int      `json:"in_flight"`
	NotStarted int      `json:"not_started"`
	Blocked    int      `json:"blocked"`
	Progress   string   `json:"progress"`
}

// Goals folds the event log into one GoalView per goal id, sorted by id. It
// is pure and deterministic. Per id the latest goal event wins for title,
// acceptance, required and status; created_at is the first goal event's ts.
// Progress counts the goal's required tasks — its own `required` list plus
// every task whose planned event links the goal id — against the task statuses
// Derive derives: accepted (passed, landed), in_flight (dispatched, running,
// finished, needs-correction), not_started (planned or unknown), blocked
// (blocked, rejected). Progress is "k/n required tasks accepted", never a
// percentage.
func Goals(events []Event) []GoalView {
	spec := map[string]GoalSpec{}
	created := map[string]string{}
	var order []string
	for _, e := range sortedGoalEvents(events) {
		id := e.Goal.ID
		if _, ok := created[id]; !ok {
			created[id] = e.TS
			order = append(order, id)
		}
		spec[id] = *e.Goal
	}

	st := Derive(events)
	status := map[string]string{}
	for _, ts := range st.Tasks {
		status[ts.ID] = ts.Status
	}

	var out []GoalView
	for _, id := range order {
		s := spec[id]
		required := map[string]bool{}
		for _, t := range s.Required {
			required[t] = true
		}
		for _, e := range events {
			if e.Kind == "planned" && e.GoalID == id {
				required[e.Task] = true
			}
		}
		g := GoalView{
			ID: id, Title: s.Title, Acceptance: s.Acceptance,
			Required: sortedKeys(required), Status: s.Status, CreatedAt: created[id],
		}
		g.Total = len(required)
		for t := range required {
			switch status[t] {
			case "passed", "landed":
				g.Accepted++
			case "dispatched", "running", "finished", "needs-correction":
				g.InFlight++
			case "blocked", "rejected":
				g.Blocked++
			default:
				g.NotStarted++
			}
		}
		g.Progress = fmt.Sprintf("%d/%d required tasks accepted", g.Accepted, g.Total)
		out = append(out, g)
	}
	slices.SortFunc(out, func(a, b GoalView) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// sortedGoalEvents returns the goal events in ts order (canonical JSON breaks
// ties), so the fold's "latest wins" is independent of file order.
func sortedGoalEvents(events []Event) []Event {
	var gs []Event
	for _, e := range events {
		if e.Kind == "goal" && e.Goal != nil {
			gs = append(gs, e)
		}
	}
	slices.SortStableFunc(gs, func(a, b Event) int {
		at, aerr := time.Parse(time.RFC3339Nano, a.TS)
		bt, berr := time.Parse(time.RFC3339Nano, b.TS)
		if aerr == nil && berr == nil {
			if at.Before(bt) {
				return -1
			}
			if at.After(bt) {
				return 1
			}
		}
		return strings.Compare(canonical(a), canonical(b))
	})
	return gs
}

// sortedKeys returns the sorted keys of a string set.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
