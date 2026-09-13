# Worker Brief — how to run the flywheel loop safely

This is the operating manual for the orchestrator (Codex or Claude Code). The worker is the OpenCode
CLI running the approved model (`$MODEL`). `$MODEL` is the approved worker model, set once in
`skills/flywheel/SKILL.md` → Invariants. Everything below is a rule, not a suggestion.

## 1. Precise, bounded briefs

The worker sees **only** the brief text plus the working tree — no chat history, no shared context.
It also auto-loads the repo's `AGENTS.md` / `CLAUDE.md` into its system prompt, so a brief carries
only what those cannot know. One task per brief. Each brief must state, in plain text:

- **Goal** — the single outcome, stated as a verifiable result.
- **owns:** / **needs:** — the files this task may write and the task ids that must land first
  (rules live in §4).
- **Exact change** — what to modify and the intended approach; leave no ambiguity about scope.
- **Don't-touch list** — every file with uncommitted, in-flight changes the worker could clobber
  (the orchestrator's own work and any other worker's; see §4).
- **Write rule** — every brief includes, verbatim, "One tool call per response; at most 120 lines
  written per tool call." A worker that drafts a whole large file in one response hits the model's
  output cap; the run exits rc 0 with the last `step_finish` reason `length` and nothing written.
  In the field run all three output-cap failures came from briefs without this rule, and none
  happened after it was added.
- **Task-specific tests** — tests only this task can define, beyond what the repo's documented
  gates already cover.
- **Report contract** — what to return: files changed, tests run, exact output of those tests,
  exit status, and anything it left undone or uncertain, plus a "Findings outside `owns:`"
  section: real problems noticed outside the task, reported not fixed. One such finding became
  a new task.
- **Plan check-in** — the brief says "state your plan in one text message before step 20", so
  the orchestrator can check direction without interrupting (a 53-step exploration was otherwise
  unreadable).
- **Moves and renames** — when a task moves or renames a file, grant "files that reference it
  (list them with grep first)" in `owns:`. Moves break every test that reads the file by path;
  workers handled it correctly, but had to flag it instead of being allowed.
- **State-changing routes** — require one failure-injection test per state-changing route (see
  the traps in §6).
- **Docs tasks** — "document what is in the code; flag what is not". A docs worker told to
  document a parallel task caught a code/doc mismatch this way; the docs task becomes a cheap
  second reviewer.

Large scope per brief is fine; large single writes are not.

State a gate command explicitly only when the task needs a **non-default** gate: the worker already
knows the documented gates from `AGENTS.md`/`CLAUDE.md`, so a brief that restates them is dead weight
(this cut one brief from 166 to 81 lines with no loss of quality). Example from a real session: the
repo doc said only `clippy`, but plain `cargo clippy` misses lints in test code — the brief had to say
`cargo clippy --all-targets`.

Keep it bounded: a bug fix, a single feature slice, one migration. If a request is bigger than one
brief, split it and run the pieces as separate, ordered tasks.

## 2. Dispatch: verify, then use the safe quoted file brief

Never trust flag names from memory. Before relying on `opencode run` options, run:

```bash
opencode run --help
```

A **fresh run** must label the task with `--title` (human-readable), auto-approve permissions with
`--auto`, and emit `--format json` so the session id comes back in the output. Dispatch with the
brief quoted into a single argument — quoting the `$(cat ...)` substitution prevents word-splitting
and glob expansion and keeps the brief out of your editing surface:

```bash
mkdir -p .flywheel/runs
opencode run --pure -m "$MODEL" --auto --format json --title "<id>" \
  "$(cat .flywheel/briefs/<id>.txt)" < /dev/null > .flywheel/runs/<id>.r1.jsonl; rc=$?
```

- `-m "$MODEL"` is the **approved default**. Never switch providers or models silently, and never
  assert a metered model is free. Cost is small but real: a one-line probe on the approved model on
  2026-09-12 used 46,324 tokens, 46,310 of them cache reads, and cost $0.0004; the field run used
  32.7 M fresh input tokens vs 289 M cache reads (~90 % of input), 0.7 M output, and 1.5 M reasoning
  across 99 runs. An earlier measurement on a different setup saw no caching at all. Check
  `part.tokens.cache.read` on `step_finish` events in your own runs rather than trust either number.
  If cost is a question, ask the human which models are on their flat-rate plan before dispatching.
- `--auto` is **required for non-interactive dispatch**: without it the worker hangs on a permission
  prompt nobody can answer the first time it tries to write a file. If you prefer not to auto-approve,
  configure `opencode.jsonc` permission settings as the narrower alternative.
- `--pure` runs without external plugins, so the worker always gets OpenCode's default `build` agent
  whatever the user's global config loads. In one run a global plugin replaced the agent and both
  workers looped on reads without editing; `--pure` fixed it. If a project needs a plugin, pin the
  agent with `--agent build` instead and watch for the same loop. Resuming a session first started
  without `--pure` under `--pure` works — it held across ten dispatches in a consumer run, fresh
  and resumed. In another setup the same global plugin did not swap the agent, so the effect depends
  on the plugin and its config; `--pure` removes the variable either way. If a plugin is what
  supplies provider auth, `--pure` drops it: set credentials with `opencode auth login` instead.
- `--title "<id>"` gives the run a human-readable label — and is the kill handle in §3;
  `--format json` is what emits the **actual session id** in the output.
- `< /dev/null` closes stdin and is required on every dispatch and every resume. In a non-TTY shell
  (an agent's shell tool, CI), `opencode run` waits on an open stdin and writes nothing after
  startup, which looks exactly like a stall. Run dispatches from bash (Git Bash on Windows);
  PowerShell 5.1 has no `/dev/null` and no `&&`.
- `rc=$?` captures the **actual exit status** — save it; it is evidence.
- Run files are per attempt: `.flywheel/runs/<id>.r1.jsonl` for the first fresh run (`r2` if you
  ever re-dispatch fresh) and `<id>.c1.jsonl`, `<id>.c2.jsonl`, ... for each correction resume.
  Per-attempt steps, tokens and finish reasons then stay separate, so a correction never pollutes
  the fresh run's stats.
- Read the run file and record the emitted `sessionID` — every JSONL event carries it:

  ```bash
  grep -o '"sessionID":"[^"]*"' .flywheel/runs/<id>.r1.jsonl | head -1
  ```

  That value — and only that value — is what you pass to `--session` later. `--session` accepts an
  existing emitted session id, never an invented `flywheel-<id>` string.

## 3. Run states and failures

Each line of the run file is one event with a top-level `type` and `sessionID`. Verified on a probe:
`step_start`, `text`, `step_finish`. Tool calls add tool events; failures appear as `error` events
(field run). `step_finish` carries `part.reason` (`stop` on a normal finish, `length` when the
output cap was hit), `part.tokens` `{total, input, output, reasoning, cache: {read, write}}`, and
`part.cost`.

| state | how to detect | what to do |
| --- | --- | --- |
| starting | no output yet | wait — a healthy run writes its first event within about 30 s (the probe took 25 s end to end). |
| silent | no output after 60 s | check, in order: was stdin closed? is there a provider error in the opencode log? Only then treat it as stalled. |
| running | events arriving | do nothing; let it run. |
| exploring | distinct files read keeps rising, zero edits, no file read over and over | healthy for large tasks — one run read for 53 steps, about 40 minutes, then made 50 edits steadily. Compare the plan message the brief asked for (see §1) with what it is reading; if off course, stop it by PID and resume with a delta, otherwise leave it. |
| long step | events stop for 5-10 min during a large generation | not a stall; do not kill it. |
| read loop | the same file read again and again, no edits (compare `"tool":"read"` with `"tool":"edit"`/`"tool":"write"` counts in the run file) | stop it by PID, check which agent the opencode log shows for the session (`agent=` on its lines), and re-dispatch with `--pure`. |
| capped | rc 0 and the last reason is `length` | resume the same session, with the write rule as the delta. |
| provider error | an `error` event in the JSONL, or errors only in the opencode log | see §8. |
| done | rc 0 and the last reason is `stop` | review it (§6). |

Detection commands:

```bash
wc -c < .flywheel/runs/<id>.<attempt>.jsonl                               # 0 after 60 s = silent
grep -o '"reason":"[^"]*"' .flywheel/runs/<id>.<attempt>.jsonl | tail -1  # stop | length
grep '"type":"error"' .flywheel/runs/<id>.<attempt>.jsonl                 # provider error in the run
grep '<sessionID>' ~/.local/share/opencode/log/opencode.log | tail -20
```

`<attempt>` is the run being classified (`r1` for the fresh run, `c<n>` for a correction).

The shared log is `~/.local/share/opencode/log/opencode.log`
(`%USERPROFILE%\.local\share\opencode\log\opencode.log` on Windows). It mixes every session on the
machine, so filter by session id. `opencode run --print-logs` also writes logs to stderr, which can
be captured per run with `2> .flywheel/runs/<id>.log`.

Provider failures from the field run (each first looked like silence or no progress):

- per-key limit: "Key limit exceeded", visible only in opencode.log; cost about 3 hours.
- credits: HTTP 402 "can only afford N tokens", as a JSONL error event.
- consent gate: the fallback model refused with "requires explicit opt in" (a China-hosted provider),
  discovered only at dispatch.

**Processes.** Never kill `opencode serve` or any opencode process while a dispatch is in flight —
the server is shared, and killing it takes down healthy work (that mistake looked exactly like a
mysterious race condition; it was self-inflicted). Reap orphans only between batches, when nothing
is in flight. On Unix count with `pgrep -x opencode`, never `pgrep -f`: a bare `-f` matches full
command lines, so any brief mentioning opencode inflates the count — observed: 67 apparent
processes when the true state was one server and zero orphans; `pgrep -x` matches the process name
exactly and is immune. A killed background dispatch exits 144, which is expected and not a worker
failure.

There is no zero-process precondition before dispatching. Parallel workers are normal — 7 at once in
the field run — and on 2026-09-12 a dispatch with stdin closed returned in 25 s while two unrelated
opencode runs were in flight. The zero-byte stalls the old text could not explain are most likely
the open-stdin hang from §2; orphan cleanup is hygiene, not the fix.

Other opencode processes may be the user's own work in other repos. Never stop a process you did not
start.

On Windows, `pgrep`/`pkill` in Git Bash see only MSYS processes, not native `opencode.exe`.
OpenCode Desktop can share the `opencode` process name, so never kill by name (`pkill opencode`,
`Stop-Process -Name opencode`, `taskkill /IM opencode.exe`). Find your dispatch by its unique title
and stop that PID only:

```powershell
Get-CimInstance Win32_Process -Filter "Name like 'opencode%'" |
  Where-Object { $_.CommandLine -like '*--title*<id>*' } |
  Select-Object ProcessId, ExecutablePath, CommandLine
Stop-Process -Id <pid>
```

The CLI binary is `node_modules\opencode-ai\bin\opencode.exe` under the npm global prefix; check
ExecutablePath before stopping anything.

## 4. Concurrency: disjoint file ownership, preserve dirty edits

Parallel workers are allowed only under **disjoint file ownership**: no two concurrently running
workers may touch the same file. Partition the change set up front and state each worker's owned
files explicitly in its brief. If two tasks would overlap, serialize them or split them differently —
do not let two sessions race on one file.

**Ready filter.** Before each dispatch, a task is ready when every `needs:` task has landed, its
`owns:` is disjoint from every in-flight task's `owns:`, and it shares no choke-point file with
in-flight work. Recompute it from the brief headers every time. A scheduler is not needed; the filter
is (the field run had 44 tasks and 5 choke-point files). A read-only `flywheel next` is planned for
this.

**Early dispatch with a follow-up delta.** Docs, audit and verification tasks whose dependencies are
still in flight can dispatch early. Add this addendum to the brief: "Cover only what has landed in
the working tree; list anything still missing under 'planned, not found'." After the dependency
lands, resume the same session with a short delta to cover the rest (typically 10-15 steps). In a
consumer run this paid off four times and saved roughly one full worker cycle (30-60 minutes) per
task.

**Contract dependency.** Write ownership is not the only dependency. Task B may compile against a
function signature that task A is creating, in a file B must not edit — files are disjoint, tasks are
not. Put the **exact signature** in both briefs, and tell B explicitly: if the contract is missing or
the file is mid-edit, wait and retry the build; never write your own copy, never edit A's file.
Registration files (a `lib.rs`, a module index, a route table) are structural choke points because
almost every task wants to add a line — serialize on them or give one task sole ownership.

**Task manifest (a convention, not tooling).** Have each brief declare two lines at the top —
`owns:` (files this task may write) and `needs:` (task ids that must land first). That makes the two
real coordination mistakes mechanically checkable: two concurrent tasks sharing an `owns:` entry, and
dispatching before a `needs:` task is done. Do not build a graph runner — it violates the skill's own
DRY rule, and coordination was not the observed bottleneck; reliability was.

**Exclusive resources.** Tasks that share a build cache or device (for example PlatformIO's
`.pio/`) must not run together even with disjoint `owns:`. Declare an optional `exclusive: <resource>`
header line and treat it like a shared choke point.

**Gate scoping.** Under concurrency, a worker's full-repo gate can fail on another worker's
half-written files. Give workers a scoped gate (the affected tests plus typecheck) and run the full
gate yourself only when nothing is in flight.

**Preserve dirty edits.** The orchestrator's own uncommitted work — and any other worker's
uncommitted work — is not free real estate. A brief's don't-touch list must name every file with
in-flight changes the worker could otherwise clobber. If you can't guarantee disjoint ownership for a
change, don't dispatch it in parallel.

**Check for orphans between batches.** Orphan accumulation is silent and only shows up as unexplained
stalls later, so a long orchestration session should run the orphan check from §3 between batches —
reap orphans only when nothing is in flight, and never clean up while a dispatch is running.

## 5. Process and session handles

Keep the process handle and the session id for the lifetime of the task:

- **Exit status** (`$?` after `opencode run`): nonzero means the run failed to *execute* (bad args,
  missing binary, auth failure, crash). Zero means the run *completed* — nothing more.
- **Session id** (the `sessionID` emitted in `--format json` output on a fresh run): the handle used
  to resume the same worker context on correction. Never lose it; a correction without the emitted
  session id restarts the worker from zero, and you must never substitute an invented
  `flywheel-<id>` string.

Record rc, session id, and model for every run — in the run file plus `flywheel.md`. Keep helper
scripts and state in the repo, never in a per-session scratch directory: a session restart lost the
field run's helpers.

## 6. Review: exit status + diff, and independent validation

The worker executes tests; **you judge the evidence**. Never accept "tests passed" as self-report:

1. Check the exit status. Nonzero → investigate the run failure before anything else.
2. Read `git diff` against the brief: correct change, no scope creep, no clobbered dirty edits,
   nothing on the don't-touch list touched.
3. **Independent validation when needed**: re-run the gate commands yourself on changes that are
   security-, money-, or schema-sensitive, or whenever the worker's own test output looks suspicious.
   The worker runs tests as part of the work; you re-run them as the judge. You may run these
   validation commands yourself — but any resulting implementation change still goes to the worker.

**Recurring traps:**
- **Gate fails in files I don't own**: usually another worker's in-flight edit. Re-run the gate at a
  quiet point before blaming either task.
- **Edits outside `owns:`**: a rename or tooling workaround can force them. Compare
  `git diff --name-only` with the brief's `owns:`, then accept after review or send a correction.
- **Tests that pass as a superuser but fail under the production role**: row-level security and
  permission bugs hide there. Check which role the tests run as.
- **Assumptions the worker never questions** (multi-tenant isolation, token lifecycle, idempotency).
  In the field run, diff review caught a cross-account existence oracle, a token-loss design flaw,
  and a revoke that deleted the row the revoked state depended on. No worker flagged any of them.
- **Debug and test-only surfaces under the production flag**: look for them in every auth diff. A
  test-only token endpoint was still built under the production flag, so anyone could mint a
  session; no worker flagged it.
- **Contract prose that disagrees with its test rows**: cross-check them. Two contract rows
  contradicted each other about the same header.
- **Error paths that fall through**: for every `catch` that sends a response, check that it returns,
  and inject one store failure per state-changing route. A handler sent the error but carried on to
  close a device socket, publish a change event and journal a revoke that never happened; its own 72
  tests passed because none injected a failure.

## 7. Correct, don't implement

When the diff fails review, the orchestrator **sends a correction to the worker** — it does not write
the implementation itself. Resume the worker's session using the **emitted session id** with a delta
brief (only the correction, not a restated task):

```bash
opencode run --pure -m "$MODEL" --auto --format json --session "<emitted-sessionID>" \
  "$(cat .flywheel/briefs/<id>.delta.txt)" < /dev/null > .flywheel/runs/<id>.c<n>.jsonl; rc=$?
```

Each correction resume writes a new attempt file (`c1`, `c2`, ...) with `>` — never `>>` — so
per-attempt steps, tokens and finish reasons stay separate (§2).

`--auto` is required here too (same non-interactive permission prompt), and stdin must be closed per
§2.

Review the result again. Repeat until the diff passes. If you ever find yourself typing the fix, you
have broken the loop — stop and dispatch it instead.

## 8. Blocker protocol: do not take over

If the worker is unavailable — `opencode` CLI missing, model unauthenticated, session cannot be
resumed, the approved worker model is not reachable, or a provider failure (§3) — **report the
blocker and halt**. Do not implement the task yourself to "keep moving". Surfacing the blocker is the
correct outcome; silently taking over violates the orchestrator/worker boundary.

**Recovery.** With the user's explicit OK, resume the same session on a different model by changing
only `-m` (`--session <emitted id> -m <other model>`). The worker keeps its context; this rescued the
field run after credit exhaustion. Never pick the fallback yourself, and never accept a consent gate
(China hosting, training on request data) on the user's behalf.

## 9. Hard rules

- No commits or pushes unless the user asks; standing instructions in the repo's `CLAUDE.md` or
  `AGENTS.md` count as asking. Workers never commit.
- No secrets, keys, tokens, or credentials in a brief or on any command line.
- DRY: drive the `opencode` CLI directly. Do not copy scripts, do not scaffold a framework. The
  `opencode-delegate` skill is an optional integration you may call; it is never something to clone.
- On OpenCode Go, keep "Allow models that train on request data" off when the worker reads a private
  repo.
