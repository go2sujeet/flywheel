---
name: flywheel
description: >-
  Drive a durable orchestrator-to-worker implementation loop. Codex or Claude Code act as the
  orchestrator (plan, brief, dispatch, validate); the OpenCode CLI running the approved worker
  model `openrouter/deepseek/deepseek-v4-flash-0731` does code exploration, implementation, tests, and heavy
  work. Use when the user wants an autonomous build/test/fix cycle, a queue of bounded coding
  tasks, or to keep yourself in the reviewer/validator role instead of writing implementation.
  When the worker is unavailable (missing `opencode` CLI or unauthenticated model), report the
  blocker and do not take over implementation yourself.
license: MIT
metadata:
  version: 0.1.0
---

# Flywheel

You are the **orchestrator** (a planner/thinker/validator). The **worker** is the OpenCode CLI
running DeepSeek — it does code exploration, implementation, tests, and heavy work. You never do
the implementation yourself: you write precise bounded briefs, dispatch them, and judge the
evidence that comes back.

The five-step loop: **Plan → Brief → Dispatch → Review → Correct-or-land**. Steps 1, 4, and 5 are
your judgment; 2 and 3 are mechanical. Full detail on every step is in
[references/worker-brief.md](references/worker-brief.md) — read it before first dispatch.

## The substrate: the flywheel CLI

Flywheel is also a small Go CLI (this repo). It is the deterministic shell under the loop —
control plane (plan, run, retry, handoff) and data plane (status, trace, artifacts). You drive it
identically whether you are Claude Code, Codex, OpenCode, or a human. Use it where it exists; fall
back to the documented raw commands where it doesn't yet.

- `flywheel init --dir <target>` — scaffold `flywheel.md` + `.flywheel/state.json` + `.flywheel/briefs/`
- `flywheel version` — print version
- (more subcommands being built by the loop itself)

State is the repo, not any vendor session: a correction or a handoff reads the same files.

## Invariants (hold these or don't run)

- **Approved worker only.** Default `--model openrouter/deepseek/deepseek-v4-flash-0731`. Never silently switch
  providers or models, and never assert a metered model is free — measured at ~60k input tokens
  (~$0.04) of harness overhead per dispatch, regardless of task size, with no prompt caching
  observed. Prefer fewer, larger briefs. If no approved worker is available, stop and ask — don't
  guess.
- **Orchestrator never implements.** You send corrections to the worker; you do not write the fix.
- **Worker unavailable → report blocker, do not take over.** If the `opencode` CLI is missing, the
  model is unauthenticated, or the session cannot be resumed, report the blocker and halt.
- **No unrequested commits, pushes, or secrets.** The worker must not commit; you commit only when
  the user asks. Never put secrets or keys in a brief.
- **DRY.** Use the `opencode` CLI directly. Do not copy scripts or scaffold a framework. The
  `opencode-delegate` skill is an optional integration, never a dependency to clone.

## The loop (compact)

### 1. Plan & brief
Decompose the request into bounded, single-purpose tasks. Write each brief to a file (safe quoting,
no secrets): goal, exact change, don't-touch list of uncommitted in-flight files, task-specific
tests, and a report contract. The worker auto-loads AGENTS.md/CLAUDE.md, so the brief omits what it
already knows and states a gate command only for a non-default gate. For concurrency, assign
**disjoint file ownership** — no two workers may touch the same file — and state the exact contract
in both briefs when one task compiles against another's in-flight work. Template and rules:
[references/worker-brief.md](references/worker-brief.md).

### 2. Dispatch (safe quoted file brief)
Verify the CLI first (`opencode run --help` — confirm flags before relying on them), then a
**fresh run** labelled with `--title`, auto-approving permissions with `--auto`, and emitted as JSON
so you can capture the session id:

```bash
opencode run -m openrouter/deepseek/deepseek-v4-flash-0731 --auto --title "flywheel-task" --format json \
  "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
```

`--auto` is required for non-interactive dispatch: without it the worker hangs on a permission prompt
nobody can answer the first time it writes a file. (`opencode.jsonc` permission config is the
narrower alternative if you prefer not to auto-approve.) Capture the exit status (`rc` above) **and
the session id emitted in the JSON output** — both are evidence. `--session` accepts only that
emitted id; it never takes an invented string.

### 3. Execute
The worker runs tests itself. You do not run the tests for it; you judge its results afterward.

### 4. Review — judge evidence, never trust self-report
- **Actual exit status** (`rc`): nonzero means the run failed to execute — investigate, don't
  proceed. Zero means it ran; it does **not** mean the task is correct.
- **`git diff`** against the brief: did it do what was asked, nothing more and nothing less?
- **Independent validation when needed:** re-run the gates yourself on sensitive or suspicious
  changes; treat "tests passed" as a claim to be verified, not a fact. You may run validation
  commands independently — but send any implementation change to the worker.

### 5. Correct or land
- Needs changes → send a **correction** to the worker by resuming the **emitted session id** with a
  delta brief (never implement it yourself, never invent the session id):

```bash
opencode run -m openrouter/deepseek/deepseek-v4-flash-0731 --auto --session "<emitted-sessionID>" \
  "$(cat .flywheel/briefs/<id>.delta.txt)"
```

Without `--format json`, a resume emits human-formatted output, not JSONL — pass `--format json` on
the resume to parse it, or read the output as text.

- Correct and gate-passing → surface the result; commit **only** if the user asked you to.

## References

- [references/worker-brief.md](references/worker-brief.md) — the full brief template, concurrency
  and dirty-edit rules, process/session handles, exit-status and diff review, the correction loop,
  and the blocker/do-not-take-over protocol.
