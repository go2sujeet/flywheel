# Worker Brief — how to run the flywheel loop safely

This is the operating manual for the orchestrator (Codex or Claude Code). The worker is the OpenCode
CLI running `opencode-go/deepseek-v4-pro`. Everything below is a rule, not a suggestion.

## 1. Precise, bounded briefs

The worker sees **only** the brief text plus the working tree — no chat history, no shared context.
It also auto-loads the repo's `AGENTS.md` / `CLAUDE.md` into its system prompt, so a brief carries
only what those cannot know. One task per brief. Each brief must state, in plain text:

- **Goal** — the single outcome, stated as a verifiable result.
- **Exact change** — what to modify and the intended approach; leave no ambiguity about scope.
- **Don't-touch list** — every file with uncommitted, in-flight changes the worker could clobber
  (the orchestrator's own work and any other worker's; see §3).
- **Task-specific tests** — tests only this task can define, beyond what the repo's documented
  gates already cover.
- **Report contract** — what to return: files changed, tests run, exact output of those tests,
  exit status, and anything it left undone or uncertain.

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
opencode run -m opencode-go/deepseek-v4-pro --auto --title "flywheel-task" --format json \
  "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
```

- `-m opencode-go/deepseek-v4-pro` is the **approved default**. Never silently switch providers or
  models. Never claim a model is free: measured at ~60k input tokens (~$0.04) of harness overhead per
  dispatch regardless of task size, with no prompt caching observed (cache read/write both zero), so a
  custom agent definition would relocate tokens rather than save them. Prefer fewer, larger,
  well-bounded briefs. If cost is a question, ask the human which models are on their flat-rate plan
  before dispatching.
- `--auto` is **required for non-interactive dispatch**: without it the worker hangs on a permission
  prompt nobody can answer the first time it tries to write a file. If you prefer not to auto-approve,
  configure `opencode.jsonc` permission settings as the narrower alternative.
- `--title "flywheel-task"` gives the run a human-readable label — and is the kill handle below;
  `--format json` is what emits the **actual session id** in the output.
- `rc=$?` captures the **actual exit status** — save it; it is evidence.
- Read the JSON output and record the emitted `sessionID`. That value — and only that value — is
  what you pass to `--session` later. `--session` accepts an existing emitted session id, never an
  invented `flywheel-<id>` string.

**Detecting a stalled run.** A healthy run writes JSONL within roughly 30 seconds; zero bytes after
that is a stall, not slowness — check with `wc -c` on the output file, not by waiting. Distinguish
the two hangs: zero bytes means a stall; events present but stopping after tool calls means a
permission block (see the `--auto` note above). Recovery is a **precondition, not a remedy** — the
environment must be verified clean before every dispatch that follows another one. The precondition
is the whole thing:

```bash
pgrep -f "opencode run" | wc -l    # MUST be 0 — never clean up mid-flight
pkill -f "opencode serve"
pgrep -x opencode | wc -l          # MUST be 0 before dispatching
```

1. **Never kill `opencode serve` while any dispatch is running.** It is shared, and killing it takes
   down healthy work. This mistake looks exactly like a mysterious race condition — it was
   self-inflicted, not a simultaneous-dispatch bug. The `pgrep -f "opencode run"` count MUST be 0
   before any cleanup: only reap when no dispatch is in flight.
2. **Verify with `pgrep -x opencode`, never a `-f` pattern match.** A bare `pgrep -f opencode` (or
   `-fl`) matches full command lines, and a dispatch passes its brief as an argument, so any brief
   mentioning opencode inflates the count — observed: 67 apparent processes when the true state was
   one server and zero orphans. `pgrep -x opencode` matches the process *name* exactly and is immune
   to this.
3. **A verified-clean environment strongly improves the odds, but it is not a cure.** Treat it as a
   precondition for a dispatch rather than a remedy applied
   only after a stall.** Dispatching from a state verified as zero opencode processes produced
   output within a second, three times in a row; dispatching without verifying stalled, repeatedly,
   with the same brief. Honest caveat: this is not a complete explanation. A dispatch has also stalled AFTER a verified-clean check, and has succeeded with a stray process present. Long, multi-step briefs stall far more than short ones. Cleaning up first clearly helps and costs nothing; it is not a guarantee, and the underlying cause is not fully understood. Check both counts before every dispatch that follows another one.

Run the cleanup-and-verify as its **own step** and read the numbers before dispatching. An
orchestrator that runs the cleanup and the dispatch in a single shell command cannot see the
verification output once the dispatch is backgrounded, so it cannot confirm the precondition held —
this alone caused two stalls that looked inexplicable. A killed background dispatch exits 144, which
is expected and not a worker failure.

## 3. Concurrency: disjoint file ownership, preserve dirty edits

Parallel workers are allowed only under **disjoint file ownership**: no two concurrently running
workers may touch the same file. Partition the change set up front and state each worker's owned
files explicitly in its brief. If two tasks would overlap, serialize them or split them differently —
do not let two sessions race on one file.

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

**Preserve dirty edits.** The orchestrator's own uncommitted work — and any other worker's
uncommitted work — is not free real estate. A brief's don't-touch list must name every file with
in-flight changes the worker could otherwise clobber. If you can't guarantee disjoint ownership for a
change, don't dispatch it in parallel.

**Check for orphans between batches.** Orphan accumulation is silent and only shows up as unexplained
stalls later, so a long orchestration session should run the cleanup-and-verify step from §2 between
batches — verify zero opencode processes, and never clean up while a dispatch is in flight — before
the next dispatch.

## 4. Process and session handles

Keep the process handle and the session id for the lifetime of the task:

- **Exit status** (`$?` after `opencode run`): nonzero means the run failed to *execute* (bad args,
  missing binary, auth failure, crash). Zero means the run *completed* — nothing more.
- **Session id** (the `sessionID` emitted in `--format json` output on a fresh run): the handle used
  to resume the same worker context on correction. Never lose it; a correction without the emitted
  session id restarts the worker from zero, and you must never substitute an invented
  `flywheel-<id>` string.

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
the implementation itself. Resume the worker's session using the **emitted session id** with a delta
brief (only the correction, not a restated task):

```bash
opencode run -m opencode-go/deepseek-v4-pro --auto --session "<emitted-sessionID>" \
  "$(cat .flywheel/briefs/<id>.delta.txt)"
```

`--auto` is required here too (same non-interactive permission prompt). Without `--format json`, a
resume emits human-formatted output, not JSONL — pass `--format json` on the resume to parse it, or
read the output as text.

Review the result again. Repeat until the diff passes. If you ever find yourself typing the fix, you
have broken the loop — stop and dispatch it instead.

## 7. Blocker protocol: do not take over

If the worker is unavailable — `opencode` CLI missing, model unauthenticated, session cannot be
resumed, or the approved worker model is not reachable — **report the blocker and halt**. Do not
implement the task yourself to "keep moving". Surfacing the blocker is the correct outcome; silently
taking over violates the orchestrator/worker boundary.

## 8. Hard rules

- No automatic commits or pushes, ever. Committing is the user's call and the user's instruction.
- No secrets, keys, tokens, or credentials in a brief or on any command line.
- DRY: drive the `opencode` CLI directly. Do not copy scripts, do not scaffold a framework. The
  `opencode-delegate` skill is an optional integration you may call; it is never something to clone.
