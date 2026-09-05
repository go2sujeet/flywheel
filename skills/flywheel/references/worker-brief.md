# Worker Brief — how to run the flywheel loop safely

This is the operating manual for the orchestrator (Codex, Claude Code, an OpenCode main
agent, or a supervisor subagent). The worker is whatever agent does the implementation:
an OpenCode CLI run on any pinned model, a same-harness subagent, or a cross-agent
combination. Everything below is a rule, not a suggestion.

## 1. Precise, bounded briefs

The worker sees **only** the brief text plus the working tree — no chat history, no shared context.
If it isn't in the brief, the worker cannot know it. One task per brief. Each brief must state, in
plain text:

- **Goal** — the single outcome, stated as a verifiable result.
- **Current state** — where the code is now, so the worker doesn't have to re-derive it.
- **Exact change** — what to modify and the intended approach; leave no ambiguity about scope.
- **Don't-touch list** — files, modules, or behaviors explicitly off-limits (includes the
  orchestrator's own in-flight work; see §3).
- **Gate commands** — the repo's actual test/lint/build commands, discovered from
  `AGENTS.md` / `CLAUDE.md` / `Makefile` / `package.json`. Never assume a command exists.
- **Worker model + transport** — the task's pinned `<provider>/<model>` and which transport
  carries it (`opencode-cli` or `subagent`), so any agent reading only the brief file knows
  the combination (mirrors `tasks.worker_model`; corrections stay same-model, §6).
- **Report contract** — what to return: files changed, tests run, exact output of those tests,
  exit status, and anything it left undone or uncertain. End every brief with the
  SQLite write-back (task id filled in; stdout report stays required as fallback):

  ```text
  Report back: (1) print files changed, gate commands run with exact output and exit
  status, and anything undone/uncertain to stdout; (2) best-effort, append the same
  to SQLite: sqlite3 .flywheel/flywheel.db "PRAGMA busy_timeout=5000; INSERT INTO
  messages(task_id,from_role,to_role,kind,body) VALUES ('<id>','worker','orchestrator',
  'report','<files; tests; exit; undone>'); INSERT INTO gate_runs(task_id,run_id,command,
  exit_code,output,ran_by) VALUES ('<id>',(SELECT MAX(id) FROM runs WHERE
  task_id='<id>'),'<gate-cmd>',<exit>,'<output>','worker');" — if sqlite3 or the DB
  is unavailable, stdout alone suffices and I will persist it.
  ```

Keep it bounded: a bug fix, a single feature slice, one migration. If a request is bigger than one
brief, split it and run the pieces as separate, ordered tasks.

## 2. Dispatch: pin the model, verify transport, then use the safe quoted file brief

Pin the worker model per task at brief time (default `opencode-go/deepseek-v4-pro`;
any `<provider>/<model>` with user approval; record in `tasks.worker_model`). Never
switch mid-task without approval; never claim a model is free — ask which models are on
the user's flat-rate plan first. A mid-task model change is a fresh run, never a session resume.

**Transport A — cross-CLI** (`opencode run`). Never trust flag names from memory:

```bash
opencode run --help
```

A **fresh run** must label the task with `--title` (human-readable) and `--format json` so the
session id comes back in the output. Dispatch with the brief quoted into a single argument — quoting
the `$(cat ...)` substitution prevents word-splitting and glob expansion and keeps the brief out of
your editing surface:

```bash
opencode run -m <pinned-worker-model> --title "flywheel-task" --format json \
  "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
```

- `-m <pinned-worker-model>` is the task's pinned model (default `opencode-go/deepseek-v4-pro`).
- `--title "flywheel-task"` gives the run a human-readable label; `--format json` is what emits the
  **actual session id** in the output.
- `rc=$?` captures the **actual exit status** — save it; it is evidence.
- Read the JSON output and record the emitted `sessionID` plus the model used. That value — and only
  that value — is what you pass to `--session` later. `--session` accepts an existing emitted session
  handle on the **same model**, never an invented `flywheel-<id>` string.

**Transport B — same-harness subagent** (main agent → Task/subagent). Pass the brief file path +
task id + pinned model in the subagent prompt; the subagent reads the brief file and the DB,
works only its owned files, and returns the report contract (stdout canonical, SQLite append
best-effort). Persist the harness-emitted run/subagent id as `runs.session_id` with
`transport='subagent'`. Resume via the harness resume mechanism with the delta brief path —
same rule: emitted handle only, same model only.

## 3. Concurrency: disjoint file ownership, preserve dirty edits

Parallel workers are allowed only under **disjoint file ownership**: no two concurrently running
workers may touch the same file. Partition the change set up front and state each worker's owned
files explicitly in its brief. If two tasks would overlap, serialize them or split them differently —
do not let two sessions race on one file.

**Preserve dirty edits.** The orchestrator's own uncommitted work — and any other worker's
uncommitted work — is not free real estate. A brief's don't-touch list must name every file with
in-flight changes the worker could otherwise clobber. If you can't guarantee disjoint ownership for a
change, don't dispatch it in parallel.

## 4. Process and session handles

Keep the process handle, the session handle, and the model for the lifetime of the task:

- **Exit status** (`$?` after `opencode run`, or subagent success/failure): nonzero/failure means the
  run failed to *execute* (bad args, missing binary/harness, auth failure, crash). Zero/success means
  the run *completed* — nothing more.
- **Session handle** (the `sessionID` emitted in `--format json` output, or the harness-emitted
  subagent/run id): the handle used to resume the same worker context on correction, **same model
  only**. Never lose it; a correction without the emitted handle restarts the worker from zero, and
  you must never substitute an invented `flywheel-<id>` string. A model change always starts a fresh
  run instead.
- **Model + transport** (`runs.model`, `runs.transport`): persist both per attempt so any agent
  resuming later knows exactly which combination produced which result.

## 5. Review: exit status + diff, and independent validation

The worker executes tests; **you judge the evidence**. Never accept "tests passed" as self-report:

1. Check the exit status. Nonzero → investigate the run failure before anything else.
2. Read `git diff` against the brief: correct change, no scope creep, no clobbered dirty edits,
   nothing on the don't-touch list touched.
3. **Independent validation when needed**: re-run the gate commands yourself on changes that are
   security-, money-, or schema-sensitive, or whenever the worker's own test output looks suspicious.
   The worker runs tests as part of the work; you re-run them as the judge. You may run these
   validation commands yourself — but any resulting implementation change still goes to the worker.

## 6. Correct, don't implement

When the diff fails review, the orchestrator **sends a correction to the worker** — it does not write
the implementation itself. Resume the worker's session using the **emitted session handle** with a
delta brief (only the correction, not a restated task), same model. Cross-CLI:

```bash
opencode run -m <pinned-worker-model> --session "<emitted-session-handle>" \
  "$(cat .flywheel/briefs/<id>.delta.txt)"
```

Same-harness: resume the subagent/thread with the delta brief path. User-approved model change =
fresh run (new attempt row, new handle), never a cross-model resume.

Review the result again. Repeat until the diff passes. If you ever find yourself typing the fix, you
have broken the loop — stop and dispatch it instead.

## 7. Blocker protocol: do not take over

If the worker is unavailable — dispatch transport missing (`opencode` CLI absent, no subagent
harness), pinned model unauthenticated or unreachable, session cannot be
resumed — **report the blocker and halt**. Name the transport + model that failed. Do not
implement the task yourself to "keep moving", and do not silently swap models to route
around it. Surfacing the blocker is the correct outcome; silently
taking over or silently switching violates the orchestrator/worker boundary.

## 8. Hard rules

- No automatic commits or pushes, ever. Committing is the user's call and the user's instruction.
- No secrets, keys, tokens, or credentials in a brief, in SQLite rows, or on any command line.
- DRY: drive the `opencode` and `sqlite3` CLIs directly. Do not copy scripts, do not scaffold a framework. The
  `opencode-delegate` skill is an optional integration you may call; it is never something to clone.
- State: every brief/dispatch/review/verdict persists to `.flywheel/flywheel.db`
  (seeded from `schema.sql`); brief files stay the fallback if `sqlite3` is missing.

## 9. State persistence (orchestrator owns it, worker appends best-effort)

Full cookbook: [state.md](state.md). Minimum per task:

1. Before briefing: pin `tasks.worker_model`, query `file_claims` for overlap, insert `tasks` + claims + `messages` brief row.
2. On dispatch: insert `briefs` + `runs(attempt=1, model=<pinned>, transport=...)`, then update `runs` with `rc` and the
   verbatim emitted session handle. Prefix writes with `PRAGMA busy_timeout=5000;`.
3. Review: insert `reviews` + any orchestrator `gate_runs`, flip `tasks.status`.
4. Correction: insert `deltas`, insert `runs(attempt=prev+1, session_handle=<emitted>, same model)>` — model change needs approval + fresh run.
5. Land (`pass`): set `tasks.status='landed'`, `DELETE FROM file_claims WHERE task_id=...`.

Worker report-back: the brief's report contract instructs the worker to append its
own `messages` report + `gate_runs` rows via the `sqlite3` CLI (DB is in its tree).
Stdout report stays canonical — if the worker can't write (no sqlite3, read-only
checkout), it prints the contract and the orchestrator inserts it. Never let a
missing DB block the loop; note the gap and continue via brief files.
