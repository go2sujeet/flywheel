package flywheel

import (
	"fmt"
	"slices"
)

// CostRow is one per-task or per-model cost line: the id, the summed Tokens
// struct and the summed cost of its finished events.
type CostRow struct {
	ID     string  `json:"id,omitempty"`
	Tokens Tokens  `json:"tokens"`
	Cost   float64 `json:"cost"`
}

// CostReport is the summed cost view over the log's finished events: one row
// per task and per model with ids sorted, plus the grand total.
type CostReport struct {
	Tasks  []CostRow `json:"tasks"`
	Models []CostRow `json:"models"`
	Total  CostRow   `json:"total"`
}

// Count returns the five-component token sum of the row.
func (r CostRow) Count() int {
	return r.Tokens.Input + r.Tokens.Output + r.Tokens.Reasoning +
		r.Tokens.CacheRead + r.Tokens.CacheWrite
}

// Cost reads the event log and sums the Tokens and cost of every finished
// event, grouped per task and per model. A finished task's model is the model
// on its latest dispatched event; a finished task without one lands under
// "unknown". An empty log yields zeroed rows, not an error.
func Cost(dir string) (CostReport, error) {
	events, err := ReadEvents(dir)
	if err != nil {
		return CostReport{}, fmt.Errorf("cost %s: %w", dir, err)
	}
	model := map[string]string{}
	byTask := map[string]*CostRow{}
	byModel := map[string]*CostRow{}
	var total CostRow
	for _, e := range events {
		if e.Kind == "dispatched" && e.Model != "" {
			model[e.Task] = e.Model
		}
		if e.Kind != "finished" {
			continue
		}
		t := e.Tokens
		if t == nil {
			t = &Tokens{}
		}
		m := model[e.Task]
		if m == "" {
			m = "unknown"
		}
		if byTask[e.Task] == nil {
			byTask[e.Task] = &CostRow{ID: e.Task}
		}
		if byModel[m] == nil {
			byModel[m] = &CostRow{ID: m}
		}
		addCostRow(byTask[e.Task], *t, e.Cost)
		addCostRow(byModel[m], *t, e.Cost)
		addCostRow(&total, *t, e.Cost)
	}
	return CostReport{
		Tasks:  sortedCostRows(byTask),
		Models: sortedCostRows(byModel),
		Total:  total,
	}, nil
}

// addCostRow sums t and cost into r.
func addCostRow(r *CostRow, t Tokens, cost float64) {
	r.Tokens.Input += t.Input
	r.Tokens.Output += t.Output
	r.Tokens.Reasoning += t.Reasoning
	r.Tokens.CacheRead += t.CacheRead
	r.Tokens.CacheWrite += t.CacheWrite
	r.Cost += cost
}

// sortedCostRows returns the map's rows sorted by id.
func sortedCostRows(m map[string]*CostRow) []CostRow {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	rows := make([]CostRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, *m[id])
	}
	return rows
}
