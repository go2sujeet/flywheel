# flywheel

A durable orchestrator-to-worker implementation loop. Any orchestrator — **Codex, Claude Code,
an OpenCode main agent, or a supervisor subagent** — plans, briefs, dispatches, validates.
Any worker — **OpenCode CLI on any model, a same-harness subagent, or a cross-agent combination** —
does code exploration, implementation, tests, and heavy work. The worker model is pinned per task
(default `opencode-go/deepseek-v4-pro`, overridable with user approval) and every attempt is
persisted to SQLite. The orchestrator writes precise bounded briefs and judges the evidence; it never implements
the change itself.

## Install

```bash
npx skills add go2sujeet/flywheel --skill flywheel
```

Requires the `opencode` CLI installed and authenticated (`opencode auth login`), plus git.
State uses the `sqlite3` CLI (preinstalled on macOS) with DB at `.flywheel/flywheel.db`
(gitignored, seeded from `skills/flywheel/schema.sql`) — no server, no extra deps.

## Usage

Invoke the skill when you want to run an autonomous build/test/fix cycle through the worker, or keep
yourself in the reviewer/validator role.

1. **Plan & brief** — decompose into bounded tasks, pin the worker model per task, and write each brief to a file (goal, current
   state, exact change, don't-touch list, real gate commands, report contract). Claim owned files
   in `file_claims` and register the task first. See
   `skills/flywheel/references/worker-brief.md` and `skills/flywheel/references/state.md`.
2. **Dispatch (fresh run)** — cross-CLI: verify flags (`opencode run --help`), then run the safe quoted file
   brief with the pinned `-m <model>`, a `--title` label and `--format json` to capture the emitted session handle;
   same-harness: dispatch a subagent with the brief path + task id + pinned model:

   ```bash
   opencode run -m <pinned-model> --title "flywheel-task" --format json \
     "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
   # save rc AND the emitted session handle — same-model resume only, model change = fresh run
   ```

3. **Review** — judge the actual exit status and `git diff`; re-run gates independently when needed.
   Log every verdict to `reviews`/`gate_runs` and flip `tasks.status`.
4. **Correct or land** — send a correction by resuming the **emitted** session handle (never an invented
   one, same model) with `deltas` + `runs(attempt+1)` linked; on `pass`, release `file_claims` and mark `landed`; the orchestrator sends implementation changes to the worker, never writes them itself:

   ```bash
   opencode run -m <pinned-model> --session "<emitted-handle>" \
     "$(cat .flywheel/briefs/<id>.delta.txt)"
   ```

Rules: disjoint file ownership for concurrent workers; preserve dirty edits; report the blocker and
halt if the worker is unavailable (name transport + model, never silently swap models); no automatic commits/pushes; no secrets in prompts or rows. Fresh runs use
`--title` + `--format json`; resume accepts only an existing emitted handle, same model.

## Evals

Verify the loop locally before trusting it — 5 evals covering the bounded-bug-fix loop,
parallel-edit conflicts, unavailable-worker blockers, SQLite durable state, and cross-model
combos (spec in `skills/flywheel/evals/evals.json`):

```bash
python3 skills/flywheel/evals/run_evals.py --root .
```

Each eval runs in a throwaway sandbox (real DB seeded via the documented init, real brief
files, real `git diff` evidence, one live `opencode run` probe with a bogus model that fails
fast with no inference cost). Results land in `.flywheel/eval-results/` (gitignored) as
`results-<timestamp>.json` + `report-<timestamp>.md` with per-eval timing and sqlite-write
counts. Worker implementation steps are simulated via the report contract — the evals test
orchestrator-loop mechanics, not model output quality.
