# The factory model

Flywheel is a **factory** that ships code. Its execution system has two planes:

- **Control plane** — dispatch and policy: who may start which work, under what limits.
- **Data plane** — traceability and telemetry for every session: what ran, on what tree, with
  what result.

This file is the shared model every persona skill links to. A *unit* is one dispatched work order
(a brief plus its run). The protocol these roles follow is
[docs/design/autonomous-shipping.md](../../../docs/design/autonomous-shipping.md).

## Roles

A persona is a role, not a model. One agent may hold several; only the worker is restricted to
the OpenCode CLI in phase 1.

| Factory role | Persona | Does | Never | Skill |
| --- | --- | --- | --- | --- |
| Plant manager | lead | owns the goal, sets policy, reviews escalations | implements | `flywheel` (exists) |
| Production planner | planner | turns a spec into work orders: `owns:`/`needs:`/`exclusive:`, gates, acceptance criteria | dispatches | `flywheel-planner` (new) |
| Line supervisor | foreman | runs a line of workers, watches run states, retries by policy, pulls the andon cord | plans or inspects | `flywheel-foreman` (new) |
| Line worker | worker | executes one work order, runs its gates, reports evidence | plans, commits, records gauge readings, inspections or audits | `flywheel-worker` (exists) |
| Machine gauges | supervisor (the CLI, no model) | runs every declared gate in the unit's own worktree, checks `owns:`, records readings bound to the git tree hash | judges intent | none: `flywheel supervise`, planned (#55) |
| QC inspector (internal) | inspector | inspects finished units against their work order; verdicts pass / rework / scrap / escalate | runs gauges as evidence, fixes, inspects its own work | `flywheel-inspector` (new) |
| External auditor | auditor | audits first articles, samples and records; files nonconformances | works the line, discusses a unit with the line before reporting | `flywheel-auditor` (new) |
| Continuous improvement | steward | triages signals and nonconformances into learnings | changes units | `flywheel-steward` (new) |
| Owner | operator | installs, configures, assigns personas, decides who merges | — | `flywheel-operator` (exists) |

## Independence rules

- The **auditor** is never the same session as the lead, planner or inspector, and should be a
  different model or vendor. Its report is only worth something if it cannot be talked out of it.
- The **inspector** never inspects work from its own session; a unit it wrote or planned goes to
  another inspector or to audit.
- A **worker** never records gauge readings, inspections or audits. It reports evidence; it does
  not grade itself or others.

## Factory mechanics

**Work order and traveler.** A work order is the brief: `owns:`/`needs:`/`exclusive:`, goal,
exact change, don't-touch list, gates, acceptance criteria and report contract. The traveler is
the brief plus the event chain attached to it — every run, reading, verdict and audit appended in
order. Nothing about a unit lives in a vendor session; the traveler is the record.

**Lot traceability.** Every gauge reading is bound to the git tree hash it was measured on. A
reading without a tree is not evidence; verdicts and audits cite the same tree the unit ran on.

**Poka-yoke.** Commands refuse illegal steps: dispatching before `needs:` land, overlapping
`owns:`, recording a reading for the wrong tree, an inspector grading its own session's work. The
refusal is the feature — the loop cannot be driven into an illegal state by accident.

**Andon cord.** Anyone who sees a signal (a stall, a provider error, repeated rework, a defect)
pulls the cord: that signal stops the checkpoint, land and handoff until it is triaged. No
session ends with untriaged signals.

**First article inspection.** The first unit of each wave — or of each kind of task — gets full
inspection *and* an audit, even when nothing suggests a problem. It validates the work order
template before it is reused.

**Sampling.** The audit rate rises with nonconformances and falls with a clean record. A clean
line is audited at the base rate; a line with nonconformances is audited at a higher rate until
they are resolved.

**Nonconformance and corrective action.** Audit findings become nonconformances. The steward
triages them into learnings (Observed / Evidence / Ask) with a corrective action; a finding that
produces no learning is not finished.

**Shift handoff.** State is the repo, so a handoff reads the same files: the traveler, the event
log, the learnings. The new head confirms the tree hash and the untriaged signal count before
continuing.

## Enforcement today vs planned

Today the only enforcement is the skill rules and CI. The planned CLI commands (with their issue
numbers) add mechanical enforcement; until they land, each persona runs its part of these by
hand. Once the event log lands (#11), `flywheel log` / `flywheel state` record the traveler.

| Mechanic | Enforced today by | Planned CLI enforcement |
| --- | --- | --- |
| Run states (silent, exploring, stalled, capped) | the skills and the run files | `flywheel status`, `flywheel watch` (#21, #22) |
| Gauges run on the unit's own tree | the skills (gates run by hand) | `flywheel supervise` (#55) |
| Traveler complete; readings bound to the tree | the skills | `flywheel verify` (#54) |
| Poka-yoke: illegal steps refused | the skills and CI | enforcing commands, git and agent hooks, the required `flywheel-audit` check (#56) |
| External audit | the auditor persona, by hand | `flywheel audit` (#61) |
| Traceability of units and sessions | the skills | `flywheel explain`, `flywheel context` (#58), `flywheel trace` (#62) |
| Andon: signals stop checkpoint, land and handoff | the skills | signals and `flywheel feedback` (#37-#39) |
| Seeing the floor | asking the lead | `flywheel factory` dashboard (#63) |