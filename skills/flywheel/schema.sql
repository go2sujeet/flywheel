-- Flywheel durable state. Single file, no server, no dependency beyond the
-- sqlite3 CLI (preinstalled on macOS, standard on Linux CI).
-- DB lives at .flywheel/flywheel.db (gitignored). This file is the source of
-- truth bundled with the skill; init creates the DB from it, never commit the DB.
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
PRAGMA synchronous=NORMAL;

-- Remember: one row per bounded task. status is the resumable state machine.
-- worker_model is pinned per task (default opencode-go/deepseek-v4-pro, overridable
-- with user approval); orchestrator_model is informational (whoever briefed it).
CREATE TABLE IF NOT EXISTS tasks (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'queued'
              CHECK (status IN ('queued','briefed','dispatched','review','correct','landed','blocked')),
  goal        TEXT NOT NULL DEFAULT '',
  current_state TEXT NOT NULL DEFAULT '',
  exact_change  TEXT NOT NULL DEFAULT '',
  dont_touch  TEXT NOT NULL DEFAULT '',
  gates       TEXT NOT NULL DEFAULT '',
  worker_model TEXT NOT NULL DEFAULT 'opencode-go/deepseek-v4-pro',
  orchestrator_model TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Briefs: every dispatch input, content-addressed so review can diff intent vs outcome.
CREATE TABLE IF NOT EXISTS briefs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  path       TEXT NOT NULL,
  content    TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_briefs_task ON briefs(task_id);

-- Runs / iterate: one row per attempt. attempt increments, session_id is the
-- worker session handle (OpenCode sessionID for opencode-cli transport, harness
-- subagent/run id for subagent transport) — ALWAYS the emitted value, never
-- invented. model repeats the task's pinned worker_model at dispatch time so a
-- mid-task model change is visible; transport in opencode-cli/subagent/other.
CREATE TABLE IF NOT EXISTS runs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id      TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  attempt      INTEGER NOT NULL DEFAULT 1,
  session_id   TEXT,
  title        TEXT NOT NULL DEFAULT '',
  model        TEXT NOT NULL DEFAULT '',
  transport    TEXT NOT NULL DEFAULT 'opencode-cli'
               CHECK (transport IN ('opencode-cli','subagent','other')),
  exit_code    INTEGER,
  dispatched_at TEXT NOT NULL DEFAULT (datetime('now')),
  completed_at  TEXT,
  UNIQUE (task_id, attempt)
);
CREATE INDEX IF NOT EXISTS idx_runs_task ON runs(task_id);
CREATE INDEX IF NOT EXISTS idx_runs_session ON runs(session_id);

-- Coordinate: active file ownership. One row per claimed file; delete on land.
-- Disjoint check = any row matching a file you want to claim.
CREATE TABLE IF NOT EXISTS file_claims (
  file_path  TEXT PRIMARY KEY,
  task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  claimed_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_claims_task ON file_claims(task_id);

-- Communicate: append-only orchestrator <-> worker log. kind in
-- brief/delta/report/review/verdict/blocker. Worker writes reports here when
-- instructed by its brief; orchestrator always writes briefs/verdicts.
CREATE TABLE IF NOT EXISTS messages (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  run_id     INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  from_role  TEXT NOT NULL CHECK (from_role IN ('orchestrator','worker')),
  to_role    TEXT NOT NULL CHECK (to_role IN ('orchestrator','worker')),
  kind       TEXT NOT NULL DEFAULT 'report',
  body       TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task_id, id);

-- Verify: orchestrator judgement per run. verdict drives next action.
CREATE TABLE IF NOT EXISTS reviews (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id      INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  verdict     TEXT NOT NULL CHECK (verdict IN ('pass','correct','blocked')),
  diff_stat   TEXT NOT NULL DEFAULT '',
  gate_rerun  TEXT NOT NULL DEFAULT '',
  notes       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_reviews_run ON reviews(run_id);

-- Verify evidence: every gate execution, worker or orchestrator, with output.
CREATE TABLE IF NOT EXISTS gate_runs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  run_id     INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  command    TEXT NOT NULL,
  exit_code  INTEGER,
  output     TEXT NOT NULL DEFAULT '',
  ran_by     TEXT NOT NULL CHECK (ran_by IN ('worker','orchestrator')),
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_gates_task ON gate_runs(task_id, id);

-- Iterate: delta briefs linked to the parent run they correct.
CREATE TABLE IF NOT EXISTS deltas (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id       TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  parent_run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  path          TEXT NOT NULL,
  reason        TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
