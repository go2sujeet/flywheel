# Flywheel runtime: design (Phase 0, part 3)

Part three of the Phase 0 runtime design: A restates how the runtime works today (per
docs/design/runtime/01-architecture.md), C proposes the target architecture, D lists the exact
data model changes, following the owner's spec sections 3-19 (goals, attempt identity, leases,
retry, event-sourced recovery), 36-37 (concurrency, single controller) and 49-50 (no
overengineering; implementation plan). The lead's decisions are binding: the event log stays the
only source of truth and everything extends it additively — no database, no queue, no daemon
framework, standard library only; every cited code name was confirmed against the tree.

## A. Existing architecture

- **Persistence**: `.flywheel/events.jsonl`, an append-only JSONL source of truth; `AppendEvent` validates, stamps ts and appends with torn-byte repair, `ParseEvents` reads it back naming bad lines, `Validate` enforces task/attempt patterns, the kind set and verdict rules (internal/flywheel/events.go:AppendEvent, internal/flywheel/events.go:ParseEvents, internal/flywheel/events.go:Validate).
- **State**: `Derive` replays the log (sorted, canonical tiebreak) into state.json and the flywheel.md status block, written atomically (internal/flywheel/state.go:Derive, internal/flywheel/state.go:atomicWrite); it never checks attempt ids, so a late event from an older attempt wins.
- **Work orders**: brief headers (owns/needs/gate) parsed by `ParseBriefHeader`; gates are shell lines run by `runGate` under `ValidateTask` (internal/flywheel/brief.go:ParseBriefHeader, internal/flywheel/gauges.go:runGate, internal/flywheel/gauges.go:ValidateTask).
- **Config and init**: `LoadConfig` reads .flywheel/config.json with validation; `InitSeeded` scaffolds events.jsonl, state.json, config.json and flywheel.md (internal/flywheel/config.go:LoadConfig, internal/flywheel/init.go:InitSeeded).
- **Dispatch**: `Run` requires a planned event, appends dispatched, spawns opencode under an embedded deny policy, streams `.flywheel/runs/<task>.<attempt>.jsonl` and records started/worker_plan/report/finished; `attemptNum` numbers rN/cN attempts, `ExitCode` maps outcomes (internal/flywheel/run.go:Run, internal/flywheel/run.go:attemptNum, internal/flywheel/run.go:ExitCode).
- **Validation**: `ValidateTask` runs each gate, hashes the tree with a throwaway index (`treeHash`) and checks owns with evidence logs (internal/flywheel/gauges.go:treeHash, internal/flywheel/gauges.go:changedPaths).
- **Recovery**: replay-only — `Derive` rebuilds state after restarts; a torn final log byte is repaired on the next append; no leases, no liveness watch, no orphan reclamation.
- **Retry**: none in code — redispatch and resume are manual lead decisions; no retry policy, no eligible_at, no scheduler (max_parallel is declared, never enforced).
- **Status**: `classifyRun` infers silent/long-step/stalled from run-file age and growth and `buildAndon` lists them display-only; `InspectTask` gates verdicts and `VerifyTasks` replays rules T1/T3/T4/T5/T8 (internal/flywheel/factory.go:classifyRun, internal/flywheel/factory.go:buildAndon, internal/flywheel/inspect.go:InspectTask, internal/flywheel/verify.go:VerifyTasks).

## C. Proposed architecture

```text
CLI  flywheel next | controller | run | validate | inspect | factory (cmd/flywheel/*_cmd.go)
 │
 ▼
Factory runtime (internal/flywheel/)
 ├── Event log .............. .flywheel/events.jsonl ............ events.go (existing)
 ├── Goal store ............. goal events in the log ............ events.go (goals are events)
 ├── State reducer .......... state.json + flywheel.md ........... state.go (existing)
 ├── Reconcile .............. reconcile.go ...................... NEW: pure decision
 │    ├── Lease manager ...... .flywheel/leases/<task>.<attempt>.json ... leases.go (NEW)
 │    ├── Retry policy ....... config `retry` block .............. retry.go (NEW)
 │    ├── Dependency resolver  planned events + brief needs: .... resolver.go (NEW)
 │    └── Scheduler .......... worker max_parallel ............... controller.go (NEW)
 ├── Worker runtime .......... internal/flywheel/run.go:Run ...... existing
 ├── Validator ............... internal/flywheel/gauges.go:ValidateTask ... existing
 └── Status .................. internal/flywheel/factory.go:Refresh ..... existing (+ status.go NEW)
```

- Event log: the only source of truth; every change is a new event kind or field, appended additively, so old factories replay unchanged (events.go).
- Goal store: no new store — goals are `goal` events carrying id, title, acceptance and status; a `planned` event may carry a goal (events.go).
- State reducer: `Derive` re-derives State and is changed to compare each result-bearing event's attempt with the latest dispatched one, moving non-current results to the task's stale list (state.go:Derive).
- Reconcile: pure `Reconcile(State, Observed, Policy, now) -> []Action`, no LLM, no I/O; expires a dispatch_intent with no dispatched after a timeout and marks a dispatched attempt with no live lease lost (reconcile.go, new).
- Lease manager: writes and atomically renews `.flywheel/leases/<task>.<attempt>.json` every renew interval while the worker lives and removes it after appending finished; renewal is the heartbeat, i.e. liveness (leases.go, new).
- Retry policy: classifies finish reasons (lost/process crash -> retry, provider unavailable -> wait, dependency blocked -> no budget, budget exhausted -> terminal) and emits `retry_scheduled` with eligible_at from the config retry block (retry.go, new).
- Dependency resolver: a task is ready only when every brief `needs:` task has landed; a plan is the task graph of planned events plus needs (resolver.go, new).
- Scheduler: picks ready tasks within the worker's max_parallel, one live attempt per task, at most one dispatch per task (controller.go, new).
- Worker runtime: `Run` spawns opencode, streams the run file, echoes the intent id on dispatched and removes the lease after finished (run.go:Run).
- Validator: `ValidateTask` runs gates and owns on the exact tree; a validated pass is the first meaningful-progress signal (gauges.go:ValidateTask).
- Status: health is derived, never stored — HEALTHY, DEGRADED, WAITING, STALLED, BLOCKED, FAILED, COMPLETED, each with the rule and evidence that produced it (status.go, new).
- Liveness vs progress: lease renewal is liveness; new completed steps in the run file are activity; meaningful progress is a validated pass, an inspected pass, landed or a met goal criterion. Every time decision takes now explicitly.

## D. Data model changes

| Kind | Name | Where | Change |
| --- | --- | --- | --- |
| event | goal | .flywheel/events.jsonl | new kind: create/update a Goal carrying id, title, acceptance (gate commands plus required task ids) and status |
| event | dispatch_intent | .flywheel/events.jsonl | new kind: controller appends the intent id before spawning; the run echoes it on dispatched |
| event | retry_scheduled | .flywheel/events.jsonl | new kind: every retry decision carries eligible_at, so a restart never resets retry history |
| field | goal | Event | optional Goal object on goal events and on planned events |
| field | intent | Event | dispatched events carry the intent id they echo (crash-safe dispatch) |
| field | eligible_at | Event | retry_scheduled timestamp; the task is ready again once now >= eligible_at |
| field | stale | TaskState | list of stale attempts whose late results Derive ignores (attempt identity) |
| type | Goal | goal.go (new) | id, title, acceptance, status; owned by the factory, not a session |
| type | Lease | leases.go (new) | pid, host, started_at, renewed_at, expires_at, run file |
| type | Observed | reconcile.go (new) | leases + run-file ages + controller lock |
| type | Policy | reconcile.go (new) | retry budget, backoff, timeouts, taken from config |
| type | Action | reconcile.go (new) | MarkLost, ScheduleRetry, MakeReady, Dispatch, Block, Fail, MarkGoalMet, SetHealth, each with a reason string |
| type | Health | status.go (new) | HEALTHY, DEGRADED, WAITING, STALLED, BLOCKED, FAILED, COMPLETED, with the rule and evidence |
| file | .flywheel/leases/<task>.<attempt>.json | lease manager | written atomically each renew interval; removed after appending finished |
| file | .flywheel/controller.lock | controller | pid, generation, expires_at; renewed each tick; an expired lock is taken over with generation+1 |
| config | retry | .flywheel/config.json | new block: max_attempts, initial_delay, backoff, max_delay |
| func | Reconcile | reconcile.go (new) | pure (State, Observed, Policy, now) -> []Action; every time decision takes now |
| func | NextActions | next_cmd.go (new) | prints Reconcile's actions without executing them; read-only, built first |
| func | Derive | state.go | change: ignore finished/report/validated/owns_checked whose attempt is not the latest dispatched; count them as stale |