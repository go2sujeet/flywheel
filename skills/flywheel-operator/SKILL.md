---
name: flywheel-operator
description: >-
  Operate the flywheel framework from any role — human or agent. Use when you want to install
  flywheel into a repo, validate it is healthy, understand its state, or drive the loop
  (plan/brief/dispatch/review/correct-or-land) as an operator rather than as a worker. Covers the
  CLI surface (control plane: plan, run, retry, handoff; data plane: status, trace, artifacts),
  the state model (flywheel.md + .flywheel/), and how to swap agents mid-session without losing
  work.
license: MIT
metadata:
  version: 0.1.0
---

# Flywheel Operator

Flywheel is a durable orchestrator-to-worker loop. **Any agent — or a human — can drive it.** The
CLI is the deterministic substrate; the skills are the judgment layer. You are the operator: you
decide what runs, who runs it, and whether it landed.

## Install

```bash
# build and validate the CLI
go build ./... && go vet ./... && go test ./...

# install the binary
go install ./cmd/flywheel

# scaffold state into a repo
flywheel init --dir <target>     # creates flywheel.md + .flywheel/state.json + .flywheel/briefs/
```

Requires: Go toolchain (to build), the `opencode` CLI (to dispatch workers), git.

## State model

Everything is files — no database.

| File | Purpose |
| --- | --- |
| `flywheel.md` | Human-readable state: Status, Main session, Workers, Roles, Task log. |
| `.flywheel/state.json` | Machine-precise state: `version`, `status`, `tasks[]`. |
| `.flywheel/briefs/` | One file per task brief (`<id>.txt`) and per correction (`<id>.delta.txt`). |
| `.flywheel/runs/` | Raw dispatch output (JSONL) per run. |
| `.flywheel/learnings.md` | Dogfood log — friction becomes spec. |

**Repo is the session.** State lives in files, not in any vendor CLI session. That is what makes
handoff free: a new head reads the same files and continues.

## Operating the loop

1. **Plan** — decompose into bounded single-purpose tasks; each gets a brief file.
2. **Brief** — goal, exact change, don't-touch list, required gates, report contract.
3. **Dispatch** — run the worker (fresh run labeled + JSON, capture rc and sessionID):
   ```bash
   opencode run -m openrouter/deepseek/deepseek-v4-flash-0731 --auto --title "task-id" --format json \
     "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
   ```
4. **Review** — judge the actual exit status and `git diff`, never self-report. Re-run gates
   independently on sensitive changes.
5. **Correct or land** — resume the emitted session id with a delta brief for corrections:
   ```bash
   opencode run -m openrouter/deepseek/deepseek-v4-flash-0731 --auto --session "<emitted-sessionID>" \
     --format json "$(cat .flywheel/briefs/<id>.delta.txt)"
   ```

## Control plane vs data plane

| | Commands | Purpose |
| --- | --- | --- |
| **Control plane** | `flywheel plan`, `run`, `retry`, `handoff` | Move work forward: create tasks, dispatch, resume, transfer between agents. |
| **Data plane** | `flywheel status`, `trace`, `artifacts` | Understand state: what's in flight, where each task sits, what each worker produced. |

Both are reachable by anyone (agent or human) through the same CLI — the judgment layer differs,
the substrate doesn't.

## Role economy

- **Planner/validator** (frontier model: Claude Code / Codex) — decomposes, briefs, judges.
- **Worker** (cheap disposable: OpenCode + DeepSeek) — explores, implements, tests, reports.
- **Operator** (you, or any agent) — decides who plays which role for a given run.

Role ≠ adapter: the role comes first; the cheapest head that can fill it is selected. Run out of
tokens on the planner mid-session? The repo is the session — a new head reads the same files and
continues. The loop never waits for a vendor.

## Health

```bash
go test ./...            # one-command validation
git status               # what's dirty
cat .flywheel/state.json # machine state
cat .flywheel/learnings.md # what the loop has taught itself
```

If a dispatch stalls or fails, report the blocker and halt — never take over the worker's job.

## Files to read

- `skills/flywheel/SKILL.md` — the orchestrator skill (full loop rules)
- `skills/flywheel/references/worker-brief.md` — brief template + dispatch safety
- `skills/flywheel-worker/SKILL.md` — the worker's contract
- `examples/` — worked briefs