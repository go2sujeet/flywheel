---
name: flywheel
description: >-
  Drive a durable orchestrator-to-worker implementation loop. Any orchestrator
  (Codex, Claude Code, OpenCode main agent, or a subagent supervisor) plans,
  briefs, dispatches, and validates; any worker (OpenCode CLI model, harness
  subagent, or cross-agent combination) does code exploration, implementation,
  tests, and heavy work. The worker model is pinned per task (default
  `opencode-go/deepseek-v4-pro`, overridable with user approval) and every
  attempt is persisted to SQLite. Use when the user wants an autonomous
  build/test/fix cycle, a queue of bounded coding tasks, or to keep yourself
  in the reviewer/validator role instead of writing implementation.
  When the worker is unavailable, report the blocker and do not take over
  implementation yourself.
license: MIT
metadata:
  version: 0.3.0
---

# Flywheel

You are the **orchestrator** (a planner/thinker/validator) — Codex, Claude Code,
an OpenCode main agent, or a supervisor subagent. The **worker** is whatever agent
does code exploration, implementation, tests, and heavy work: an OpenCode CLI run
on any model, a same-harness subagent, or a cross-agent combination. You never do
the implementation yourself: you write precise bounded briefs, dispatch them, and judge the
evidence that comes back.

Durable state lives in `.flywheel/flywheel.db` (SQLite, gitignored), seeded from
`schema.sql`. It is how main and worker agents remember, communicate, coordinate,
verify, and iterate across sessions — query it on session start, persist every
attempt, claim files before dispatch, and log every verdict. Cookbook:
[references/state.md](references/state.md).

The five-step loop: **Plan → Brief → Dispatch → Review → Correct-or-land**. Steps 1, 4, and 5 are
your judgment; 2 and 3 are mechanical. Full detail on every step is in
[references/worker-brief.md](references/worker-brief.md) — read it before first dispatch.
State happens on every step — read [references/state.md](references/state.md) alongside it.

## Invariants (hold these or don't run)

- **Worker model pinned per task, never silently switched.** Default
  `opencode-go/deepseek-v4-pro`; the user may pin any `<provider>/<model>` per task
  at brief time (recorded in `tasks.worker_model` + `runs.model`). Never switch models
  mid-task without user approval, never assert a metered model is free — ask which
  models are on the user's flat-rate plan before dispatching. A mid-task model change
  is a **fresh run** (new attempt row, no `--session` resume across models); same-model
  corrections resume the emitted session handle. If no approved worker is
  available, stop and ask — don't guess.
- **Orchestrator never implements.** You send corrections to the worker; you do not write the fix.
- **Worker unavailable → report blocker, do not take over.** If the dispatch transport is
  missing (no `opencode` CLI, no subagent harness), the model is unauthenticated, or the
  session cannot be resumed, report the blocker and halt.
- **No unrequested commits, pushes, or secrets.** The worker must not commit; you commit only when
  the user asks. Never put secrets or keys in a brief.
- **DRY.** Use the `opencode` and `sqlite3` CLIs directly. Do not copy scripts or scaffold a framework. The
  `opencode-delegate` skill is an optional integration, never a dependency to clone. `schema.sql`
  is the only bundled state artifact; the `.db` itself is created locally and never committed.

## The loop (compact)

### 0. Remember (every session)
Init once (`sqlite3 .flywheel/flywheel.db < <skill-dir>/schema.sql`; existing 0.2.x DBs:
see [references/state.md](references/state.md) migration), then query
`tasks` (incl. `worker_model`) / `runs` (incl. `model`, `transport`) / `messages`
before planning. The resume handle is whatever `runs.session_id` holds for that
transport — never an invented string.

### 1. Plan & brief
Decompose the request into bounded, single-purpose tasks. Check `file_claims` for overlap and claim
owned files before briefing; write each brief to a file (safe quoting,
no secrets): goal, current state, exact change, explicit don't-touch list, the repo's **real** gate
commands (discover from AGENTS.md/CLAUDE.md/Makefile — never assume), pinned worker model +
transport, and a report contract. For
concurrency, assign **disjoint file ownership** — no two workers may touch the same file. Persist
the task + brief row + `messages` brief entry. Template
and rules: [references/worker-brief.md](references/worker-brief.md), state queries:
[references/state.md](references/state.md).

### 2. Dispatch (safe quoted file brief)
Two transports, same brief + same state rows. **A) Cross-CLI** (orchestrator outside
OpenCode → OpenCode worker): verify first (`opencode run --help`), then a **fresh run**
labelled with `--title` and emitted as JSON so you can capture the session id:

```bash
opencode run -m <pinned-worker-model> --title "flywheel-task" --format json \
  "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
```

**B) Same-harness subagent** (main agent → Task/subagent worker): pass the brief file path
plus task id in the subagent prompt; the subagent reads
`.flywheel/briefs/<id>.txt`, works, and reports back via the harness channel (stdout
report canonical, SQLite write-back best-effort). Persist the harness-emitted run/subagent
id as `runs.session_id` with `transport='subagent'`.

Capture the exit status (`rc` above, or subagent success/failure) **and the emitted
session handle** — both are evidence. Persist `runs(model=<pinned>, transport=...)`
(plus a `messages` report row, worker or orchestrator-written).
`--session` (or subagent resume) accepts only that emitted handle; it never takes an invented string.

### 3. Execute
The worker runs tests itself. You do not run the tests for it; you judge its results afterward.

### 4. Review — judge evidence, never trust self-report
- **Actual exit status** (`rc`): nonzero means the run failed to execute — investigate, don't
  proceed. Zero means it ran; it does **not** mean the task is correct.
- **`git diff`** against the brief: did it do what was asked, nothing more and nothing less?
- **Independent validation when needed:** re-run the gates yourself on sensitive or suspicious
  changes; treat "tests passed" as a claim to be verified, not a fact. You may run validation
  commands independently — but send any implementation change to the worker.
- **Log the verdict:** every review writes `reviews` + `gate_runs` rows and flips
  `tasks.status` (`pass`/`correct`/`blocked`); `pass` also releases `file_claims`.

### 5. Correct or land
- Needs changes → log a `deltas` row, insert the next `runs` attempt (attempt+1, same emitted
  session handle **and same model**), then send a **correction** to the worker by resuming the
  **emitted session handle** with a
  delta brief (never implement it yourself, never invent the session handle). Cross-CLI:

```bash
opencode run -m <pinned-worker-model> --session "<emitted-session-handle>" \
  "$(cat .flywheel/briefs/<id>.delta.txt)"
```

  Same-harness: resume the subagent/thread with the delta brief path. Changing models
  mid-task needs user approval and is a fresh run (new attempt, no session resume).

- Correct and gate-passing → surface the result; commit **only** if the user asked you to.

## References

- [references/state.md](references/state.md) — SQLite memory: init/remember, coordinate claims,
  dispatch attempts, worker write-back, verify logging, iterate deltas, recovery.
- [schema.sql](schema.sql) — source of truth for `.flywheel/flywheel.db`; seed only, never commit the DB.
- [references/worker-brief.md](references/worker-brief.md) — the full brief template, concurrency
  and dirty-edit rules, process/session handles, exit-status and diff review, the correction loop,
  and the blocker/do-not-take-over protocol.
