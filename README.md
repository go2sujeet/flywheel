# flywheel

A durable orchestrator-to-worker implementation loop. **Codex or Claude Code** act as the
orchestrator — plan, brief, dispatch, validate. The **OpenCode CLI running DeepSeek**
(`opencode-go/deepseek-v4-pro`) is the worker — code exploration, implementation, tests, and heavy
work. The orchestrator writes precise bounded briefs and judges the evidence; it never implements
the change itself.

## Install

```bash
npx skills add go2sujeet/flywheel --skill flywheel
```

Requires the `opencode` CLI installed and authenticated (`opencode auth login`), plus git.

## Usage

Invoke the skill when you want to run an autonomous build/test/fix cycle through the worker, or keep
yourself in the reviewer/validator role.

1. **Plan & brief** — decompose into bounded tasks and write each brief to a file (goal, current
   state, exact change, don't-touch list, real gate commands, report contract). See
   `skills/flywheel/references/worker-brief.md`.
2. **Dispatch (fresh run)** — verify flags (`opencode run --help`), then run the safe quoted file
   brief with a `--title` label and `--format json` to capture the emitted session id:

   ```bash
   opencode run -m opencode-go/deepseek-v4-pro --title "flywheel-task" --format json \
     "$(cat .flywheel/briefs/<id>.txt)"; rc=$?
   # save rc AND the sessionID emitted in the JSON output — use it for --session below
   ```

3. **Review** — judge the actual exit status and `git diff`; re-run gates independently when needed.
4. **Correct or land** — send a correction by resuming the **emitted** session id (never an invented
   one); the orchestrator sends implementation changes to the worker, never writes them itself:

   ```bash
   opencode run -m opencode-go/deepseek-v4-pro --session "<emitted-sessionID>" \
     "$(cat .flywheel/briefs/<id>.delta.txt)"
   ```

Rules: disjoint file ownership for concurrent workers; preserve dirty edits; report the blocker and
halt if the worker is unavailable; no automatic commits/pushes; no secrets in prompts. Fresh runs use
`--title` + `--format json`; `--session` accepts only an existing emitted session id.
