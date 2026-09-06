# flywheel

A durable orchestrator-to-worker implementation loop. **Codex or Claude Code** act as the
orchestrator — plan, brief, dispatch, validate. The **OpenCode CLI running DeepSeek**
(`opencode-go/deepseek-v4-pro`) is the worker — code exploration, implementation, tests, and heavy
work. The orchestrator writes precise bounded briefs and judges the evidence; it never implements
the change itself.

The loop in one picture. The `--session` edge is the point: a correction resumes the same worker,
it never restarts it, and the orchestrator never writes the fix.

```mermaid
flowchart TD
    A["Plan and brief"] --> B[Dispatch]
    B --> C["Worker executes"]
    C --> D["Review the evidence"]
    D --> E{"Diff matches the brief?"}
    E -- yes --> F[Land]
    E -- no --> G[Correction]
    G -- "--session" --> C
```

## Install

```bash
npx skills add go2sujeet/flywheel --skill flywheel
```

Requires the `opencode` CLI installed and authenticated (`opencode auth login`), plus git.

## Usage

Invoke the skill when you want to run an autonomous build/test/fix cycle through the worker, or keep
yourself in the reviewer/validator role.

1. **Plan & brief** — decompose into bounded tasks and write each brief to a file (goal, exact
   change, don't-touch list, task-specific tests, report contract — the worker auto-loads
   AGENTS.md/CLAUDE.md, so omit what it already knows). See
   `skills/flywheel/references/worker-brief.md`; worked examples live in [`examples/`](examples/).
2. **Dispatch (fresh run)** — verify flags (`opencode run --help`), then run the safe quoted file
   brief with `--auto` (required non-interactively), a `--title` label, and `--format json` to
   capture the emitted session id:

   ```bash
   opencode run -m opencode-go/deepseek-v4-pro --auto --title "flywheel-task" --format json \
     "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
   # save rc AND the sessionID emitted in the JSON output — use it for --session below
   ```

   `--auto` is required: without it the worker hangs on a permission prompt nobody can answer the
   first time it writes a file; `opencode.jsonc` permission config is the narrower alternative.

3. **Review** — judge the actual exit status and `git diff`; re-run gates independently when needed.
4. **Correct or land** — send a correction by resuming the **emitted** session id (never an invented
   one); the orchestrator sends implementation changes to the worker, never writes them itself:

   ```bash
   opencode run -m opencode-go/deepseek-v4-pro --auto --session "<emitted-sessionID>" \
     --format json "$(cat .flywheel/briefs/<id>.delta.txt)"
   ```

   `--format json` matters on resumes too: without it the resume emits human-formatted output, not
   JSONL.

Rules: disjoint file ownership for concurrent workers; preserve dirty edits; report the blocker and
halt if the worker is unavailable; no automatic commits/pushes; no secrets in prompts. Fresh runs use
`--auto` + `--title` + `--format json`; `--session` accepts only an existing emitted session id, and
resumes need `--format json` too.

## The boundary

Everything the loop does is split cleanly in two: the orchestrator plans and judges, the worker
executes. Implementation never crosses upward — that single rule is the skill's core invariant.

```mermaid
flowchart TD
    subgraph O["Orchestrator"]
        O1[Plan]
        O2["Write briefs"]
        O3["Judge exit status and diff"]
        O4["Re-run gates"]
        O5[Decide]
    end
    subgraph W["Worker"]
        W1["Explore code"]
        W2[Implement]
        W3["Run tests"]
        W4[Report]
    end
    O2 -- "brief down" --> W1
    W4 -- "evidence back" --> O3
```

## Concurrency

Parallel workers are allowed only under disjoint file ownership. But files disjoint does not mean
tasks independent — one task may compile against a signature another task is writing.

```mermaid
flowchart TD
    subgraph WA["Worker A"]
        A["file-a.rs"]
    end
    subgraph WB["Worker B"]
        B["file-b.rs"]
    end
    A -. "signature, not a file" .-> B
    L["lib.rs registration file"]
    A --> L
    B --> L
```

Registration files like `lib.rs` are choke points almost every task wants to touch: serialize on
them or give one task sole ownership. Between batches, run the cleanup-and-verify step from
Troubleshooting — verify zero opencode processes, never mid-flight — they accumulate silently and
only show up later as unexplained stalls.

## Troubleshooting

When a dispatch is not finishing, branch on the evidence instead of guessing:

```mermaid
flowchart TD
    A["Dispatch is not finishing"] --> B{"rc nonzero?"}
    B -- yes --> C["Run failed to execute: check args, binary, auth"]
    B -- no --> D{"wc -c output shows zero bytes?"}
    D -- yes --> E["Silent stall: verify no dispatch running, pkill serve, verify clean, re-dispatch"]
    D -- no --> F{"Events stop after tool calls?"}
    F -- yes --> G["Permission block: add --auto"]
    F -- no --> H["Still writing: healthy, wait"]
```

| Symptom | Fix |
| --- | --- |
| Worker hangs on the first file write | Re-dispatch with `--auto` (permission block; `opencode.jsonc` is the narrower alternative). |
| Output file has zero bytes after ~30s | Silent stall — confirm with `wc -c`, then run the recovery protocol below (precondition first) and re-dispatch. |
| Exit status `144` | Your own `pkill` — expected, not a worker failure. |
| Resume output is not JSONL | Re-dispatch the resume with `--format json`. |

**Recovery from a stall — precondition first.** Cleanup is a precondition, not a remedy: verify the
environment is clean before every dispatch that follows another one, and never clean up while a
dispatch is running.

```bash
pgrep -f "opencode run" | wc -l    # MUST be 0 — never clean up mid-flight
pkill -f "opencode serve"
pgrep -x opencode | wc -l          # MUST be 0 before dispatching
```

1. **Never kill `opencode serve` while any dispatch is running** — it is shared, and killing it
   takes down healthy work. This looks exactly like a mysterious race condition, but it is
   self-inflicted: the `pgrep -f "opencode run"` count MUST be 0 before any cleanup.
2. **Verify with `pgrep -x opencode`, never a `-f` pattern match** — `-f`/`-fl` reads full command
   lines, and a dispatch passes its brief as an argument, so a brief mentioning opencode inflates
   the count (observed 67 when the true state was one server and zero orphans). `pgrep -x` matches
   the process name exactly.
3. **A verified-clean environment strongly improves the odds, but it is not a cure.** Treat it as a
   precondition for a dispatch rather than a post-stall
   remedy.** Dispatching from a state verified as zero opencode processes produced output within a
   second, three times in a row; without verifying, the same brief stalled repeatedly. Honest caveat: this is not a complete explanation. A dispatch has also stalled AFTER a verified-clean check, and has succeeded with a stray process present. Long, multi-step briefs stall far more than short ones. Cleaning up first clearly helps and costs nothing; it is not a guarantee, and the underlying cause is not fully understood.

Run the cleanup-and-verify as its **own step** and read the numbers before dispatching — an
orchestrator that runs cleanup and dispatch in one shell command cannot see the verification output
once the dispatch is backgrounded, so it cannot confirm the precondition held (this alone caused two
inexplicable stalls). Normal completion leaks a `serve` process, so if dispatches stall after a long
session, check for orphans before blaming the model or the network.

Full detail lives in `skills/flywheel/references/worker-brief.md` — this table is a scannable
index, not a replacement.
