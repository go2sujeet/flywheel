# Flywheel runtime: gap analysis (Phase 0, part 2)

Every requirement of the owner's spec (sections 1-27) classified against the code at commit
2e6f6fd (2026-09-13). Classes: ALREADY EXISTS (implemented), PARTIALLY EXISTS (partly
implemented, what is missing in Why), MISSING (absent), NOT NEEDED (out of scope for this
local-first CLI), SHOULD NOT BE IMPLEMENTED (actively wrong). Analysis only; no code changed.

## Sections 1-27

| § | Requirement | Class | Evidence | Why |
| --- | --- | --- | --- | --- |
| 1 | Factory as control plane owning lifecycle | MISSING | none | CLI dispatches and watches single runs; no controller owns worker lifecycle. |
| 2 | Deterministic core: persistence, derivation, gates | PARTIALLY EXISTS | internal/flywheel/state.go:Derive | Log, derivation and gates are deterministic; leases, heartbeats, scheduling, retry, health are missing. |
| 3 | First-class Goal concept with acceptance criteria | MISSING | none | Tasks are the top unit; no goal entity, status or acceptance criteria. |
| 4 | Distinct task, attempt, worker, evidence concepts | PARTIALLY EXISTS | internal/flywheel/run.go:attemptNum | Tasks and rN/cN attempts are distinct; no goal, worker identity or evidence objects. |
| 5 | Factory-to-goal-to-task-to-attempt hierarchy | MISSING | none | Flat task list only; no goals, subgoals or hierarchy. |
| 6 | Desired versus observed state distinction | PARTIALLY EXISTS | internal/flywheel/factory.go:classifyRun | Observed run states derived from file signals; no desired-state model. |
| 7 | Deterministic controller with reconcile loop | MISSING | none | No loop, no reconcile, no policy; dispatch is one-shot CLI. |
| 8 | Event-driven and periodic reconciliation | MISSING | none | Events are logged but nothing reacts; no periodic liveness loop. |
| 9 | Pure reconcile(desired, observed, policy, now) | MISSING | none | No reconcile function exists to test without an LLM. |
| 10 | Formalized, validated task state machine | PARTIALLY EXISTS | internal/flywheel/state.go:Derive | Statuses derive deterministically from the latest event; no legal-transition table, illegal transitions not rejected. |
| 11 | Expiring worker leases | MISSING | none | Workers assumed alive until the run exits; no lease concept. |
| 12 | Periodic worker heartbeats | MISSING | none | Only a start watchdog and file-activity heuristics; no heartbeat events. |
| 13 | Unique attempt identity per execution | PARTIALLY EXISTS | internal/flywheel/run.go:attemptNum | Attempts numbered rN/cN from dispatched events; no worker_id, lease_id or generation. |
| 14 | Stale-result rejection by attempt token | MISSING | none | Late event from an older attempt overwrites newer status: Derive never checks attempt against latest dispatch. |
| 15 | Deterministic scheduler of ready tasks | MISSING | none | No scheduler; needs and max_parallel declared but never enforced. |
| 16 | Config-driven retry policy with budget | MISSING | none | Retries are manual lead decisions; config budget declared but unused. |
| 17 | Durable retry eligibility and timing | MISSING | none | No retry_eligible_at; nothing moves tasks back to ready automatically. |
| 18 | Idempotent repeated reconciliation | PARTIALLY EXISTS | internal/flywheel/state.go:Derive | Derivation and atomic writes are idempotent; nothing stops double dispatch or duplicate workers. |
| 19 | Append-only log with deterministic reducer | ALREADY EXISTS | internal/flywheel/events.go:AppendEvent, internal/flywheel/state.go:Derive | Log is append-only with torn-write repair; Derive rebuilds state deterministically. |
| 20 | Crash recovery from durable log | PARTIALLY EXISTS | internal/flywheel/state.go:Derive | State re-derives after restart; no controller to recover, orphaned children not reclaimed. |
| 21 | Factory inspectable during agent blackout | PARTIALLY EXISTS | internal/flywheel/factory.go:buildAndon | Floor and andon derive from files with no LLM; no explicit blackout state. |
| 22 | Automatic redispatch when capacity returns | MISSING | none | Resume is a manual --resume decision; nothing watches capacity. |
| 23 | Activity tracked separately from progress | PARTIALLY EXISTS | internal/flywheel/factory.go:classifyRun | Ages distinguish silent/long-step/stalled; no last_event_at versus last_progress_at. |
| 24 | Goal progress derived from task states | MISSING | none | No goal entity; per-status counts exist but no goal progress accounting. |
| 25 | Persisted acceptance criteria, evaluated deterministically | MISSING | none | Gates validate per task, not goal criteria; no criteria storage. |
| 26 | Stuck-factory detection from deterministic state | PARTIALLY EXISTS | internal/flywheel/factory.go:buildAndon | Silent/stalled/capped raise an andon; no retry-loop, no-progress or budget-exhausted conditions; display only. |
| 27 | Deterministic factory health model | MISSING | none | Andon flags individual units; no HEALTHY/DEGRADED/STALLED health state. |