# Worker Brief — how to run the flywheel loop safely

This is the operating manual for the orchestrator (Codex or Claude Code). The worker is the OpenCode
CLI running `opencode-go/deepseek-v4-pro`. Everything below is a rule, not a suggestion.

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
- **Report contract** — what to return: files changed, tests run, exact output of those tests,
  exit status, and anything it left undone or uncertain.

Keep it bounded: a bug fix, a single feature slice, one migration. If a request is bigger than one
brief, split it and run the pieces as separate, ordered tasks.

## 2. Dispatch: verify, then use the safe quoted file brief

Never trust flag names from memory. Before relying on `opencode run` options, run:

```bash
opencode run --help
```

A **fresh run** must label the task with `--title` (human-readable) and `--format json` so the
session id comes back in the output. Dispatch with the brief quoted into a single argument — quoting
the `$(cat ...)` substitution prevents word-splitting and glob expansion and keeps the brief out of
your editing surface:

```bash
opencode run -m opencode-go/deepseek-v4-pro --title "flywheel-task" --format json \
  "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
```

- `-m opencode-go/deepseek-v4-pro` is the **approved default**. Never silently switch providers or
  models. Never claim a model is free; if cost is a question, ask the human which models are on their
  flat-rate plan before dispatching.
- `--title "flywheel-task"` gives the run a human-readable label; `--format json` is what emits the
  **actual session id** in the output.
- `rc=$?` captures the **actual exit status** — save it; it is evidence.
- Read the JSON output and record the emitted `sessionID`. That value — and only that value — is
  what you pass to `--session` later. `--session` accepts an existing emitted session id, never an
  invented `flywheel-<id>` string.

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
opencode run -m opencode-go/deepseek-v4-pro --session "<emitted-sessionID>" \
  "$(cat .flywheel/briefs/<id>.delta.txt)"
```

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
