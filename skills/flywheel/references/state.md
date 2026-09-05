# Flywheel state — SQLite memory for main + worker agents

One file, no server: `.flywheel/flywheel.db` (gitignored). Schema source of truth
is [`schema.sql`](../schema.sql). Use the `sqlite3` CLI only — no vendored
scripts, no framework. If `sqlite3` is missing, fall back to brief files +
stdout report and note the gap in `messages`.

## 0. Init (once per repo) and remember (every session start)

```bash
mkdir -p .flywheel/briefs
sqlite3 .flywheel/flywheel.db < skills/flywheel/schema.sql
# or after `npx skills add`, path is whatever your skills dir is —
# the rule is: <this-skill-dir>/schema.sql seeds .flywheel/flywheel.db
```

Migration from 0.2.x DBs (adds per-task model pinning + per-attempt model/transport;
fresh DBs already have these). Run once, safe to skip if columns exist — check with
`PRAGMA table_info(tasks);` / `PRAGMA table_info(runs);` first:

```bash
sqlite3 .flywheel/flywheel.db "ALTER TABLE tasks ADD COLUMN worker_model TEXT NOT NULL DEFAULT 'opencode-go/deepseek-v4-pro';"
sqlite3 .flywheel/flywheel.db "ALTER TABLE tasks ADD COLUMN orchestrator_model TEXT NOT NULL DEFAULT '';"
sqlite3 .flywheel/flywheel.db "ALTER TABLE runs ADD COLUMN model TEXT NOT NULL DEFAULT '';"
sqlite3 .flywheel/flywheel.db "ALTER TABLE runs ADD COLUMN transport TEXT NOT NULL DEFAULT 'opencode-cli';"
# backfill existing attempts from their task pin, then continue:
# UPDATE runs SET model=(SELECT worker_model FROM tasks WHERE id=runs.task_id) WHERE model='';
# new writes always use transport IN ('opencode-cli','subagent','other')
```

Concurrency note: `journal_mode=WAL` persists from schema.sql, but
`busy_timeout` is per-connection — prefix every write batch with
`PRAGMA busy_timeout=5000;`, e.g. `sqlite3 .flywheel/flywheel.db
"PRAGMA busy_timeout=5000; INSERT INTO ..."`. Reads can skip it.

Every session, before planning, restore memory — never invent a session id:

```bash
sqlite3 .flywheel/flywheel.db \
  "SELECT id,title,status,worker_model FROM tasks WHERE status NOT IN ('landed');"
sqlite3 .flywheel/flywheel.db \
  "SELECT task_id,attempt,model,transport,session_id,exit_code FROM runs ORDER BY id DESC LIMIT 10;"
sqlite3 .flywheel/flywheel.db \
  "SELECT task_id,kind,substr(body,1,400) FROM messages ORDER BY id DESC LIMIT 10;"
```

Resume rule: the resume handle is whatever `runs.session_id` holds for that row's
transport (`--session` for opencode-cli, harness resume id for subagent) — same model
only, never invented. If the
task row says `dispatched` with no matching run, the dispatch never landed —
re-brief, don't resume. A user-approved model change is a fresh run (new attempt, no
handle reuse across models).

## 1. Coordinate — claim files before you brief

```bash
# fail closed: any hit means overlap, serialize or re-partition
sqlite3 .flywheel/flywheel.db \
  "SELECT file_path,task_id FROM file_claims WHERE file_path IN ('src/api/client.ts','src/store/state.ts');"
# on clean check, claim + register task (with pinned worker model) + log brief
sqlite3 .flywheel/flywheel.db \
  "PRAGMA busy_timeout=5000; INSERT OR REPLACE INTO tasks(id,title,status,goal,worker_model) VALUES ('login-500','login 500 fix','briefed','empty email -> 400','opencode-go/deepseek-v4-pro');
   INSERT INTO file_claims(file_path,task_id) VALUES ('src/api/login.ts','login-500');
   INSERT INTO messages(task_id,from_role,to_role,kind,body) VALUES ('login-500','orchestrator','worker','brief','brief at .flywheel/briefs/login-500.txt');"
```

Release on land only: `DELETE FROM file_claims WHERE task_id='login-500';`
Never hold claims for landed tasks. Dirty-edit files from `git status --porcelain`
go in the brief don't-touch list AND stay unclaimed by anyone else.

## 2. Dispatch — persist the attempt, capture evidence

```bash
sqlite3 .flywheel/flywheel.db \
  "PRAGMA busy_timeout=5000; INSERT INTO briefs(task_id,path,content) VALUES ('login-500','.flywheel/briefs/login-500.txt',readfile('.flywheel/briefs/login-500.txt'));
   INSERT INTO runs(task_id,attempt,title,model,transport) VALUES ('login-500',1,'flywheel-login-500','opencode-go/deepseek-v4-pro','opencode-cli');
   UPDATE tasks SET status='dispatched',updated_at=datetime('now') WHERE id='login-500';"
# Transport A — cross-CLI:
opencode run -m opencode-go/deepseek-v4-pro --title "flywheel-login-500" --format json \
  "$(cat .flywheel/briefs/login-500.txt)"; rc=$?
# Transport B — same-harness subagent: dispatch with brief path + task id + pinned model,
# persist the harness-emitted run id as session_id with transport='subagent'.
# then persist what came back — rc AND emitted session handle, both are evidence
sqlite3 .flywheel/flywheel.db \
  "PRAGMA busy_timeout=5000; UPDATE runs SET exit_code=$rc, completed_at=datetime('now') WHERE id=(SELECT MAX(id) FROM runs WHERE task_id='login-500');"
# session handle goes here verbatim from the transport output, never invented:
# UPDATE runs SET session_id='<emitted-handle>' WHERE id=<that-run-id>;
```

`readfile()` needs the CLI build with file I/O (macOS/Homebrew yes). If it
errors, store the path and keep content in the brief file — don't block dispatch.

## 3. Communicate — worker write-back (brief instructs this)

The worker sees only brief + tree, and the DB is in the tree, so the brief's
report contract tells it to append (best-effort, stdout stays canonical):

```bash
sqlite3 .flywheel/flywheel.db \
  "INSERT INTO messages(task_id,from_role,to_role,kind,body) VALUES ('login-500','worker','orchestrator','report','files: ...; tests: ...; exit: 0');
   INSERT INTO gate_runs(task_id,run_id,command,exit_code,output,ran_by) VALUES ('login-500',(SELECT MAX(id) FROM runs WHERE task_id='login-500'),'npm test',0,'...','worker');"
```

If the worker can't (no sqlite3, read-only checkout), it prints the report
contract to stdout and the orchestrator inserts it. Either way the orchestrator
logs its verdict:

```bash
sqlite3 .flywheel/flywheel.db \
  "INSERT INTO messages(task_id,from_role,to_role,kind,body) VALUES ('login-500','orchestrator','worker','verdict','correct: ...');"
```

## 4. Verify — judge evidence, log it

```bash
sqlite3 .flywheel/flywheel.db \
  "INSERT INTO reviews(run_id,verdict,diff_stat,notes) VALUES (<run-id>,'correct','2 files +45-12','scope ok, gate output suspicious');
   UPDATE tasks SET status='correct',updated_at=datetime('now') WHERE id='login-500';"
# independent re-run you performed yourself:
sqlite3 .flywheel/flywheel.db \
  "INSERT INTO gate_runs(task_id,run_id,command,exit_code,output,ran_by) VALUES ('login-500',<run-id>,'npm test',1,'...','orchestrator');"
```

Verdict drives next step: `pass` → land, `correct` → delta, `blocked` → halt.
`pass` also releases claims (see §1) and sets `tasks.status='landed'`.

## 5. Iterate — delta linked to parent run, session chain intact

```bash
sqlite3 .flywheel/flywheel.db \
  "PRAGMA busy_timeout=5000; INSERT INTO deltas(task_id,parent_run_id,path,reason) VALUES ('login-500',<run-id>,'.flywheel/briefs/login-500.delta.txt','empty-string case missed');
   INSERT INTO runs(task_id,attempt,title,model,transport,session_id) VALUES ('login-500',<prev-attempt+1>,'flywheel-login-500','opencode-go/deepseek-v4-pro','opencode-cli','<emitted-handle>');
   UPDATE tasks SET status='dispatched' WHERE id='login-500';"
opencode run -m opencode-go/deepseek-v4-pro --session "<emitted-handle>" \
  "$(cat .flywheel/briefs/login-500.delta.txt)"; rc=$?
```

Attempt = parent attempt + 1, same model, never reuse. Session handle is copied from `runs`,
never constructed. User-approved model change = fresh run (new attempt, new handle, updated
`tasks.worker_model`), never a cross-model resume. Repeat review until `pass` or `blocked`.

## Recovery

- DB missing → re-init from `schema.sql`, re-register tasks from
  `.flywheel/briefs/*.txt`, note the gap. Brief files stay the fallback source.
- WAL files (`.db-wal`) are normal; leave them, keep `.flywheel/` gitignored.
- Corrupt DB → `sqlite3 .flywheel/flywheel.db "PRAGMA integrity_check;"`, restore
  from brief files if needed, never silently restart task ids.
