---
name: flywheel-operator
description: >-
  Operate the flywheel framework from any role — human or agent. Use when you want to install
  flywheel into a repo, validate it is healthy, understand its state, or drive the loop
  (plan/brief/dispatch/review/correct-or-land) as an operator rather than as a worker. The CLI
  currently implements only init and version; plan, run, retry, handoff, status, trace, and
  artifacts are planned, not built. Until they land, drive the loop manually — write brief
  files and run the raw worker commands shown here. There is no automatic handoff command yet;
  handoff is done by writing state files and passing emitted session IDs by hand.
license: MIT
metadata:
  version: 0.1.0
---

# Flywheel Operator

Flywheel is a durable orchestrator-to-worker loop. **Any agent — or a human — can drive it.** The
CLI is the deterministic substrate; the skills are the judgment layer. You are the operator: you
decide what runs, who runs it, and whether it landed.

## What the CLI implements today

Only two subcommands exist. Everything else in this skill is the manual workflow that runs on the
same state files until the planned subcommands land — don't invoke commands that aren't built.

| Command | Status | What it does |
| --- | --- | --- |
| `flywheel init` | **implemented** | Scaffold `flywheel.md` + `.flywheel/state.json` + `.flywheel/briefs/`; refuses if either state file already exists unless `--force`. |
| `flywheel version` | **implemented** | Print the flywheel version. |
| `flywheel plan`, `run`, `retry`, `handoff` | **planned** | Control plane: create tasks, dispatch, resume, transfer between agents. |
| `flywheel status`, `trace`, `artifacts` | **planned** | Data plane: in-flight work, task positions, worker outputs. |

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
| `.flywheel/runs/` | Planned: raw dispatch output (JSONL) per run — create it by hand until `run` lands. |
| `.flywheel/learnings.md` | Dogfood log — friction becomes spec (create it by hand). |

**Repo is the session.** State lives in files, not in any vendor CLI session. That is what makes
handoff free: a new head reads the same files and continues. Because `handoff` is not built yet,
transfer is manual — write the current state into `flywheel.md` and the brief files, and pass the
emitted session ID by hand to the next head.

## Operating the loop

The CLI has no plan/run/retry/handoff commands yet, so each step is done with files and raw
commands. `flywheel init` only scaffolds; the loop below is the manual fallback and runs on the
same state files the planned subcommands will automate.

1. **Plan** — decompose into bounded single-purpose tasks; each gets a brief file:
   `cat > .flywheel/briefs/<id>.txt` with goal, exact change, don't-touch list, required gates,
   report contract.
2. **Brief** — the brief file from step 1 is the brief: goal, exact change, don't-touch list,
   required gates, report contract.
3. **Dispatch (manual fallback)** — run the worker directly, capture rc and sessionID:
   ```bash
   opencode run -m openrouter/deepseek/deepseek-v4-flash-0731 --auto --title "task-id" --format json \
     "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
   ```
   Record the exit code and the emitted session ID by hand (append to `.flywheel/runs/<id>.jsonl`
   or note them in `flywheel.md`) — `flywheel run` will do this when it lands.
4. **Review** — judge the actual exit status and `git diff`, never self-report. Re-run gates
   independently on sensitive changes.
5. **Correct or land (manual fallback)** — resume the emitted session ID with a delta brief for
   corrections. Pass the session ID by hand; there is no automatic handoff:
   ```bash
   opencode run -m openrouter/deepseek/deepseek-v4-flash-0731 --auto --session "<emitted-sessionID>" \
     --format json "$(cat .flywheel/briefs/<id>.delta.txt)"
   ```

## Control plane vs data plane

| | Commands (all planned) | Purpose | Until they land |
| --- | --- | --- | --- |
| **Control plane** | `flywheel plan`, `run`, `retry`, `handoff` | Move work forward: create tasks, dispatch, resume, transfer between agents. | Write brief files and run the raw `opencode` commands by hand (above). |
| **Data plane** | `flywheel status`, `trace`, `artifacts` | Understand state: what's in flight, where each task sits, what each worker produced. | Read `flywheel.md`, `.flywheel/state.json`, and `.flywheel/briefs/` directly. |

Both are reachable by anyone (agent or human) — the judgment layer differs, the substrate
doesn't. The CLI commands are planned; do not invoke them yet.

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
cat .flywheel/learnings.md # what the loop has taught itself (create by hand until planned)
```

If a dispatch stalls or fails, report the blocker and halt — never take over the worker's job.

## Files to read

- `skills/flywheel/SKILL.md` — the orchestrator skill (full loop rules)
- `skills/flywheel/references/worker-brief.md` — brief template + dispatch safety
- `skills/flywheel-worker/SKILL.md` — the worker's contract
- `examples/` — worked briefs