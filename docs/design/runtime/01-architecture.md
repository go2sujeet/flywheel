# Flywheel runtime: architecture report (Phase 0)

Flywheel's runtime today is a single Go CLI (module `flywheel`, standard library only) that
drives a factory loop of AI coding workers. A lead records work as events in an append-only
JSONL event log; materialized views (state.json, flywheel.md) are derived from it; work orders
carry owns/needs/gate; `flywheel run` dispatches an OpenCode worker process and watches it;
`flywheel factory` renders run states and raises an andon. Analysis only, as of commit 2e6f6fd
(2026-09-13); no code was changed.

## Subsystems

### Event log

The event log is `.flywheel/events.jsonl`, one JSON object per line, and is the source of truth:
state.json and flywheel.md are derived from it. `AppendEvent`
validates the event, stamps TS when empty, and appends one JSON line with a single O_APPEND
write, prefixing a newline when the file ends in a torn (crash) byte
(internal/flywheel/events.go:AppendEvent, internal/flywheel/events.go:needsNewlinePrefix).
`ReadEvents` parses the file; `ParseEvents` skips blank lines, errors on unresolved git conflict
markers and malformed lines (naming the line), and supports strict mode rejecting unknown fields
(internal/flywheel/events.go:ReadEvents, internal/flywheel/events.go:ParseEvents). `Validate`
enforces the task pattern `^[A-Za-z0-9._-]+$`, attempt pattern `^[rc][0-9]+$`, the kind set, and
kind-specific verdict rules (internal/flywheel/events.go:Validate). Event kinds: planned,
dispatched, started, worker_plan, finished, report, reviewed, blocked, landed, amended,
validated, owns_checked, inspected, staffed.

- State owned: `.flywheel/events.jsonl`; Event fields ts, task, kind, session, model, attempt,
  rc, reason, verdict, brief, needs, owns, commit, note, adapter, path, sha256, tokens, cost,
  steps, tree, gate, command, duration_ms, outside, persona (internal/flywheel/events.go:Validate).
- Guarantees: append-only log with one write per event; torn-write repair on append; every
  event validated before append; parse failures name the offending line; conflict markers never
  silently merged.

| Q | Question | Answer |
| --- | --- | --- |
| Q6 | Deterministic? | Yes, appends and parses are deterministic; only the TS stamp reads the clock. |
| Q7 | Needs an agent alive? | No, log and parser run in the CLI itself. |
| Q8 | A worker dies? | Yes, events already appended survive; the CLI records the outcome. |
| Q9 | The orchestrating process dies? | Yes, events persist; a torn final byte is repaired on next append. |
| Q10 | All workers die? | Yes, log integrity is independent of worker liveness. |
| Q11 | All LLM providers unavailable? | Yes, logging requires no LLM. |
| Q12 | State reconstructable deterministically? | Partial, the log is the source of truth but state derivation is elsewhere. |
| Q13 | Stalled work detected automatically? | No, detection is not in this subsystem. |
| Q14 | Retried automatically? | No, retries are a higher-level decision. |
| Q15 | Retries survive a restart? | Partial, events survive; retry policy lives in the run subsystem. |
| Q16 | Duplicate work possible? | Yes, nothing deduplicates events at append time. |
| Q17 | Stale worker results mutate newer state? | No, the log is append-only; mutation happens in derived state. |
| Q18 | Progress measurable without conversations? | Yes, kinds and fields (rc, tokens, duration_ms) carry progress. |

- Gaps: no sequence number or id on events; reads re-parse the whole file every time; no
  compaction or rotation; no schema versioning on the log.
### Materialized state

`State` is the derived snapshot written to `.flywheel/state.json` (version 2, updated_at, tasks,
counts). `Derive` recomputes it from the event log: events are sorted by parsed TS, then Task,
then a per-kind rank (planned < amended < dispatched < started < worker_plan < finished < report
< validated < owns_checked < inspected < reviewed < blocked < landed), then canonical JSON, so
the result is independent of the order events were concatenated in
(internal/flywheel/state.go:Derive). The latest status-bearing
event per task decides status (planned, dispatched, running, finished, passed, needs-correction,
rejected, blocked, landed); amended only updates brief/needs/owns
(internal/flywheel/state.go:Derive). `WriteState` reads the log, derives, writes state.json via
temp file plus rename (`atomicWrite`), and updates the marked status table in flywheel.md between
`<!-- flywheel:status:start -->` markers; same input gives byte-identical output
(internal/flywheel/state.go:WriteState, internal/flywheel/state.go:atomicWrite).

- State owned: `.flywheel/state.json`, the marked status block in flywheel.md; TaskState fields
  id, status, session, model, attempt, rc, verdict, reason, brief, needs, owns, attempts,
  updated_at; State counts per status (internal/flywheel/state.go:updateStatusBlock).
- Guarantees: derived deterministically from the log (sorted replay, canonical tiebreak); atomic
  writes so a crash never leaves a half-written state.json; no mutation of the log itself.

| Q | Question | Answer |
| --- | --- | --- |
| Q6 | Deterministic? | Yes, Derive sorts deterministically; same log gives byte-identical output. |
| Q7 | Needs an agent alive? | No, derivation runs in the CLI. |
| Q8 | A worker dies? | Yes, state reflects whatever events were appended. |
| Q9 | The orchestrating process dies? | Yes, state.json is atomically written; stale files are overwritten. |
| Q10 | All workers die? | Yes, derivation needs no workers. |
| Q11 | All LLM providers unavailable? | Yes, state derivation is pure computation. |
| Q12 | State reconstructable deterministically? | Yes, Derive fully reconstructs State from the log. |
| Q13 | Stalled work detected automatically? | No, statuses are event-driven; nothing watches wall-clock time. |
| Q14 | Retried automatically? | No, derivation only reflects retries someone else scheduled. |
| Q15 | Retries survive a restart? | Yes, retries are events in the log and replay identically. |
| Q16 | Duplicate work possible? | Partial, attempts counter increments per dispatched; no dedup check. |
| Q17 | Stale worker results mutate newer state? | No, sort is by timestamp, so later events win. |
| Q18 | Progress measurable without conversations? | Yes, attempts, status and counts expose progress. |

- Gaps: no stall detection (no liveness timestamps consulted); state.json is a full rewrite, not
  incremental; no history retained beyond the last status per task; two processes writing
  state.json race (last rename wins).

### Work orders

A work order is a brief file whose header carries the keys owns, needs, gate, exclusive, review.
`ParseBriefHeader` reads the key: value block at the top of the brief (until the first blank line
followed by a '#' heading, or the first 40 lines), splits comma-separated owns values with
indented continuation lines, strips trailing parenthesised annotations like "(new)", and keeps
gate lines one command per line in order; the header also carries the SHA-256 of the whole brief
file (internal/flywheel/brief.go:ParseBriefHeader, internal/flywheel/brief.go:stripAnnotation).
Each `gate:` line is a shell
command run against the worktree by `runGate` (bash -c, cmd /C on Windows, or sh -c), reporting
exit code, elapsed time and output (internal/flywheel/gauges.go:runGate). Gates are the contract
`flywheel validate` enforces: `ValidateTask` runs each gate, checks that changed paths stay
inside owns, and records a validated event with the tree hash (internal/flywheel/gauges.go:ValidateTask).

- State owned: the brief file (arbitrary path per event); planned/amended events carry brief,
  needs, owns; validated events carry gate and tree (internal/flywheel/events.go:Validate).
- Guarantees: the brief header has a stable parse (40-line cap, annotation stripping); gate
  order is preserved; a full-brief SHA-256 is available to detect edits; no gate runs outside
  `flywheel validate`'s shell runner.

| Q | Question | Answer |
| --- | --- | --- |
| Q6 | Deterministic? | Yes, header parsing is order-stable and SHA-256 is fixed. |
| Q7 | Needs an agent alive? | No, parsing and gate running are CLI-side. |
| Q8 | A worker dies? | Yes, the brief file is inert between runs. |
| Q9 | The orchestrating process dies? | Yes, the brief and its SHA-256 survive on disk. |
| Q10 | All workers die? | Yes, orders are data, not processes. |
| Q11 | All LLM providers unavailable? | Yes, gate checks are local shell commands. |
| Q12 | State reconstructable deterministically? | Partial, the brief is hashed but not re-read into state. |
| Q13 | Stalled work detected automatically? | No, nothing watches order age. |
| Q14 | Retried automatically? | No, redispatch is a lead decision via log. |
| Q15 | Retries survive a restart? | Yes, the brief file persists; only the SHA-256 pins it. |
| Q16 | Duplicate work possible? | Yes, nothing prevents dispatching the same brief twice. |
| Q17 | Stale worker results mutate newer state? | Partial, gates run against the worktree as found. |
| Q18 | Progress measurable without conversations? | Partial, gate pass/fail is visible; order age is not surfaced. |

- Gaps: no ordering dependency (needs) is enforced at dispatch time, only declared; the brief
  text is never stored in the event log, only its path and hash; no timeout or budget on gate
  commands; no change tracking from amended to new owns.

### Config and init

Configuration lives in `.flywheel/config.json`: version, a workers list (adapter "opencode" or
"sim", model, variant, max_parallel, approved/fallback models), limits (per_host, budget),
feedback (upstream, submit). `LoadConfig` reads it with unknown fields rejected and full
validation, falling back to `DefaultConfig` when the file is missing
(internal/flywheel/config.go:LoadConfig, internal/flywheel/config.go:DefaultConfig). `Validate`
reports every problem at once (worker name pattern, duplicate names, adapter/model rules,
fallbacks must differ, per_host >= 0, submit in {ask, never}); `Set` mutates keys like model or
limits.per_host, and `WriteConfig` writes atomically via temp file plus rename
(internal/flywheel/config.go:Validate, internal/flywheel/config.go:WriteConfig). `InitSeeded`
scaffolds the runtime: flywheel.md (status markers), state.json (from `Derive` of no events),
events.jsonl, config.json and .flywheel/.gitignore; it preflights destinations, stages payloads,
creates files with O_EXCL so racing creators are detected, and rolls back its own footprint on
failure; events.jsonl and config.json are never overwritten, even with --force
(internal/flywheel/init.go:InitSeeded, internal/flywheel/init.go:publishFile).

- State owned: `.flywheel/config.json`, `.flywheel/.gitignore`; init creates flywheel.md,
  events.jsonl, state.json; `IgnoredStateFiles` reports which of those git would ignore
  (internal/flywheel/init.go:IgnoredStateFiles).
- Guarantees: missing config behaves as the built-in default; config writes are validated and
  atomic; init never truncates existing events.jsonl or config.json; failed init restores
  preexisting bytes.

| Q | Question | Answer |
| --- | --- | --- |
| Q6 | Deterministic? | Yes, defaults and scaffolding output are fixed byte-for-byte. |
| Q7 | Needs an agent alive? | No, config and init are pure CLI operations. |
| Q8 | A worker dies? | Yes, config.json is inert state. |
| Q9 | The orchestrating process dies? | Yes, writes are atomic; init rollback is best-effort per call. |
| Q10 | All workers die? | Yes, scaffolding needs nothing live. |
| Q11 | All LLM providers unavailable? | Yes, init and config never call providers. |
| Q12 | State reconstructable deterministically? | Partial, defaults regenerate config; events cannot be recreated. |
| Q13 | Stalled work detected automatically? | No, no liveness logic here. |
| Q14 | Retried automatically? | No. |
| Q15 | Retries survive a restart? | Yes, config and initialized files persist on disk. |
| Q16 | Duplicate work possible? | Partial, init's O_EXCL prevents duplicate scaffolding. |
| Q17 | Stale worker results mutate newer state? | No, init writes once and never overwrites the log. |
| Q18 | Progress measurable without conversations? | No, config and init carry no progress data. |

- Gaps: no per-run budget enforcement in code (budget is declared but unused); fallbacks
  require manual editing of config.json; no schema migration path beyond version 1; init is
  documented as not crash-atomic across files.

### Dispatch (`flywheel run`) and the worker process

`Run` is the whole dispatch: it loads config, resolves the worker and model, requires a planned
event carrying the brief path (T1), numbers the attempt (r<n+1> fresh, c<m+1> resume), writes an
embedded OpenCode permission policy when missing (git writes denied, OPENCODE_CONFIG pointed at
it), appends a dispatched event pointing at `.flywheel/runs/<task>.<attempt>.jsonl` with the
prompt SHA-256, starts the worker, streams stdout into the run file while parsing, and records
started, worker_plan, report and finished events (internal/flywheel/run.go:Run,
internal/flywheel/run.go:workerPolicySHA). A watchdog kills the child if no stdout line arrives
within the start timeout (default 60s) and the run finishes as reason "silent"
(internal/flywheel/run.go:Run). Every path after dispatched records a finished event: an error
records reason "error" or "start-failed" with the stderr diagnostic; a worker that exits before
any completed step is start-failed (internal/flywheel/run.go:firstStderrLine,
internal/flywheel/run.go:clipNote).
The adapter builds the command (`opencode run --pure -m <model> --auto --format json` with the
brief attached via --file, session passed only on resume) and parses the JSONL stream into
start/text/tool/step/error observations (internal/flywheel/adapter.go:Command,
internal/flywheel/adapter.go:Parse). Exit codes: 0 worker rc 0 with reason stop, 4 nonzero or
capped/error, 3 silent start timeout (internal/flywheel/run.go:ExitCode).

- State owned: `.flywheel/runs/<task>.<attempt>.jsonl` (raw stream), `.err`, `.plan.md`,
  `.report.md`; dispatched/started/worker_plan/report/finished events; attempts are the
  rN/cN counter in the log (internal/flywheel/run.go:attemptNum).
- Guarantees: fresh runs never reuse a session (resume only continues via --session); dispatch
  without a planned event is refused; the worker permission policy forbids git writes; run
  files are hashed and the hash recorded in finished; every outcome after dispatched is
  recorded as an event.

| Q | Question | Answer |
| --- | --- | --- |
| Q6 | Deterministic? | Partial, attempt numbering and events are deterministic; LLM output is not. |
| Q7 | Needs an agent alive? | Yes, dispatch starts a worker process (opencode) to do the work. |
| Q8 | A worker dies? | Yes, exit code, reason and stderr note are recorded as finished. |
| Q9 | The orchestrating process dies? | Partial, events before death persist; the child may be orphaned. |
| Q10 | All workers die? | Yes, each run is independent; finished records the failure. |
| Q11 | All LLM providers unavailable? | Yes, start-failed or silent records it; resume can retry later. |
| Q12 | State reconstructable deterministically? | Partial, events replay; run files and session output are opaque. |
| Q13 | Stalled work detected automatically? | Partial, only the start timeout fires; mid-run stalls are not detected. |
| Q14 | Retried automatically? | No, retries are manual (resume via delta). |
| Q15 | Retries survive a restart? | Yes, resume reads the last session from the log and re-runs. |
| Q16 | Duplicate work possible? | Yes, nothing stops two dispatches of the same task. |
| Q17 | Stale worker results mutate newer state? | Partial, finished events stamp the log; run files are written in place. |
| Q18 | Progress measurable without conversations? | Yes, steps, tokens, cost and run-file growth are recorded. |

- Gaps: no mid-run stall detection (watchdog only covers the start); no concurrency control or
  max_parallel enforcement in Run; no retry policy (resume is manual); no cap on run duration or
  cost; session output is only parsed, never summarized.

### Run states and andon (`flywheel factory`)

`Watcher.Refresh` draws the live floor from the config, the event log and the run files,
reading only bytes appended since the last refresh (offsets remembered per file; a truncated
file is re-read from zero) and building a Floor with lines, staffing, units, andon and output
(internal/flywheel/factory.go:Refresh, internal/flywheel/factory.go:readEvents). Each unit's run
state is classified from its run file's signals — error, finish reason "length", done event,
size, age since last growth, step count, files read, edits — into running, exploring, long-step
(>5min), stalled (>10min), silent (no output after 60s), capped, provider-error or done
(internal/flywheel/factory.go:classifyRun). The andon lists units in silent, stalled, capped or
provider-error, oldest first, plus per-line busy counts and the output summary (first-pass rate
from first verdicts, rework ratio, tokens, cost, landed today) (internal/flywheel/factory.go:buildAndon,
internal/flywheel/factory.go:buildOutput). The CLI render (`flywheel factory` with `--once` or
watch mode) calls Refresh and prints the floor; rendering never calls a model and never runs a
git write (internal/flywheel/factory.go:NewWatcher).

- State owned: only reads — events.jsonl, config.json, run files; keeps in-memory offsets,
  per-run step/file/edit/error/reason tallies (internal/flywheel/factory.go:NewWatcher).
- Guarantees: incremental reads (only appended bytes, only complete lines); the floor is fully
  derivable from log plus run files; andon thresholds are fixed (60s silent, 300s long-step,
  600s stalled); live units (running, exploring, long-step, silent, stalled) occupy busy slots,
  capped and provider-error do not (internal/flywheel/factory.go:liveRun).

| Q | Question | Answer |
| --- | --- | --- |
| Q6 | Deterministic? | Yes, classification is pure; only now (clock) is injected. |
| Q7 | Needs an agent alive? | No, the floor only reads files. |
| Q8 | A worker dies? | Yes, death is visible as done, capped or provider-error. |
| Q9 | The orchestrating process dies? | Yes, the floor is rebuilt from files on the next refresh. |
| Q10 | All workers die? | Yes, the floor shows every unit's final state. |
| Q11 | All LLM providers unavailable? | Yes, rendering needs no provider. |
| Q12 | State reconstructable deterministically? | Partial, events and run files replay; the floor is derived, not stored. |
| Q13 | Stalled work detected automatically? | Yes, silent/long-step/stalled states raise an andon from ages. |
| Q14 | Retried automatically? | No, the andon only signals; a human or lead must act. |
| Q15 | Retries survive a restart? | Yes, run files and offsets re-read after truncation. |
| Q16 | Duplicate work possible? | Yes, nothing dedupes; the floor just displays attempts. |
| Q17 | Stale worker results mutate newer state? | No, the floor reads the latest attempt only. |
| Q18 | Progress measurable without conversations? | Yes, steps, ages, sizes and token aggregates are all file-derived. |

- Gaps: andon is display-only — no alerting, no automatic re-dispatch; watch mode and andon
  persistence are not in this package; stall classification trusts mtime, which a touched file
  can spoof; per-unit cost is not surfaced on units, only aggregated.
