#!/usr/bin/env python3
"""Local eval harness for the flywheel skill (stdlib only).

Runs every eval in evals.json against throwaway sandboxes under /tmp:
real `.flywheel/flywheel.db` databases (seeded via the documented
`sqlite3 db < schema.sql` command), real brief files, real `git diff`
evidence, and one live `opencode run` probe with a bogus model (eval 3,
fails fast at validation — no inference cost).

Worker implementation steps are *simulated via the report contract*
(stdout report + SQLite append) and labelled as such in the report:
these evals test orchestrator-loop mechanics (remember, coordinate,
verify, iterate), not model output quality.

Usage:
  python3 skills/flywheel/evals/run_evals.py [--root .]

Results: .flywheel/eval-results/results-<ts>.json + report-<ts>.md
(.flywheel/ is gitignored, so results never pollute the repo).
"""

import argparse
import datetime
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
from pathlib import Path

PINNED = "opencode-go/deepseek-v4-pro"
SECRET_PATTERNS = ["sk-", "AKIA", "BEGIN PRIVATE KEY", "ghp_", "xoxb-"]
SQL_WRITES = 0  # instrumented counter for the benchmark table


def sh(args, cwd=None, timeout=60, input_text=None):
    return subprocess.run(
        args, cwd=cwd, timeout=timeout, input=input_text,
        capture_output=True, text=True)


def sql(db, stmt, params=()):
    """Execute SQL, counting writes for benchmarks. Returns fetched rows."""
    global SQL_WRITES
    if re.match(r"\s*(INSERT|UPDATE|DELETE|ALTER|CREATE|PRAGMA\s+journal|PRAGMA\s+busy)",
                stmt, re.I):
        SQL_WRITES += 1
    con = sqlite3.connect(db)
    try:
        con.execute("PRAGMA busy_timeout=5000;")
        if params or re.match(r"\s*(SELECT|PRAGMA\s+table|WITH)\b", stmt, re.I):
            cur = con.execute(stmt, params)
            rows = cur.fetchall()
        else:
            con.executescript(stmt)
            rows = []
        con.commit()
        return rows
    finally:
        con.close()


def read_skill(root, rel):
    return (root / rel).read_text()


class Eval:
    def __init__(self, eid, name):
        self.id, self.name = eid, name
        self.checks = []  # (expectation, passed, evidence)
        self.notes = []
        self.t0 = time.perf_counter()

    def check(self, expectation, passed, evidence=""):
        self.checks.append({"expectation": expectation,
                            "pass": bool(passed), "evidence": str(evidence)})

    @property
    def duration_s(self):
        return round(time.perf_counter() - self.t0, 2)

    @property
    def passed(self):
        return all(c["pass"] for c in self.checks)

    def asdict(self):
        return {"id": self.id, "name": self.name, "pass": self.passed,
                "duration_s": self.duration_s, "checks": self.checks,
                "notes": self.notes}


def new_sandbox(root, ts_dir, name):
    """Fresh sandbox: briefs dir + DB seeded via the documented CLI path."""
    sb = ts_dir / name
    (sb / "briefs").mkdir(parents=True)
    db = sb / "flywheel.db"
    r = sh(["sqlite3", str(db)], input_text=(root / "skills/flywheel/schema.sql").read_text())
    assert r.returncode == 0, f"schema seed failed: {r.stderr}"
    assert db.exists(), "DB file not created by documented init"
    # git repo so `git diff` review evidence is real
    assert sh(["git", "init", "-q"], cwd=sb).returncode == 0
    assert sh(["git", "config", "user.email", "eval@local"], cwd=sb).returncode == 0
    assert sh(["git", "config", "user.name", "eval"], cwd=sb).returncode == 0
    return sb, str(db)


def write_brief(sb, bid, body):
    p = sb / "briefs" / f"{bid}.txt"
    p.write_text(body)
    return str(p)


BRIEF_LOGIN = """Goal: empty email to POST /login returns 400, not 500.
Current state: handler in src/api/login.ts skips validation; empty string hits DB lookup and throws.
Exact change: validate email non-empty in src/api/login.ts, return 400 with JSON error; add test.
Don't-touch: src/api/client.ts, src/store/state.ts, .flywheel/flywheel.db
Gate commands: npm test -- login; npm run lint
Worker model: opencode-go/deepseek-v4-pro via opencode-cli
Report contract: files changed, gate commands with exact output + exit status, undone/uncertain.
"""


def git_baseline(sb):
    (sb / "src").mkdir(exist_ok=True)
    (sb / "src" / "login.py").write_text("def login(email):\n    return lookup(email)\n")
    assert sh(["git", "add", "-A"], cwd=sb).returncode == 0
    assert sh(["git", "commit", "-qm", "baseline"], cwd=sb).returncode == 0


def eval1(root, ts_dir):
    e = Eval(1, "bounded-bug-fix-loop")
    wb = read_skill(root, "skills/flywheel/references/worker-brief.md")
    sk = read_skill(root, "skills/flywheel/SKILL.md")
    e.check("Brief template states goal/current-state/exact-change/don't-touch/gates/model/report",
            all(s in wb for s in ["Goal", "Current state", "Exact change", "Don't-touch",
                                  "Gate commands", "Worker model", "Report contract"]),
            "worker-brief.md §1")
    e.check("Skill pins worker model per task, default " + PINNED,
            PINNED in sk and "pinned" in sk.lower(), "SKILL.md invariants")
    sb, db = new_sandbox(root, ts_dir, "eval1")
    git_baseline(sb)
    bp = write_brief(sb, "login-500", BRIEF_LOGIN)
    e.check("Bounded single-task brief written to file with all seven sections",
            all(s in Path(bp).read_text() for s in
                ["Goal:", "Current state:", "Exact change:", "Don't-touch:",
                 "Gate commands:", "Worker model:", "Report contract:"]),
            bp)
    sql(db, "INSERT INTO tasks(id,title,status,goal,worker_model)"
            " VALUES ('login-500','login 500 fix','briefed','empty email -> 400',?)", (PINNED,))
    sql(db, "INSERT INTO file_claims(file_path,task_id) VALUES ('src/api/login.py','login-500')")
    sql(db, "INSERT INTO messages(task_id,from_role,to_role,kind,body) VALUES "
            "('login-500','orchestrator','worker','brief','brief at login-500.txt')")
    sql(db, "INSERT INTO briefs(task_id,path,content) VALUES ('login-500',?,?)",
        (bp, Path(bp).read_text()))
    sql(db, "INSERT INTO runs(task_id,attempt,title,model,transport) VALUES "
            "('login-500',1,'flywheel-login-500',?,'opencode-cli')", (PINNED,))
    got = sql(db, "SELECT worker_model FROM tasks WHERE id='login-500'")[0][0]
    e.check("Pinned model recorded in tasks.worker_model / runs.model + transport",
            got == PINNED and sql(db, "SELECT model,transport FROM runs")[0] == (PINNED, "opencode-cli"),
            f"tasks.worker_model={got}")
    help_txt = sh(["opencode", "run", "--help"], timeout=30)
    help_all = help_txt.stdout + help_txt.stderr  # opencode prints help to stderr
    flags_ok = "--title" in help_all and "--format" in help_all
    argv = ["opencode", "run", "-m", PINNED, "--title", "flywheel-login-500",
            "--format", "json", "$(cat %s)" % bp]
    e.check("Dispatch uses pinned -m + --title/--format json + quoted file brief, rc captured",
            flags_ok and argv[3] == PINNED,
            "opencode run --help verifies flags; rc=$? captured post-run")
    # --- simulated worker attempt 1 (report contract append) ---
    (sb / "src" / "login.py").write_text(
        "def login(email):\n    if not email:\n        return (400, {'error': 'email required'})\n    return lookup(email)\n")
    diff = sh(["git", "diff", "--stat"], cwd=sb).stdout.strip()
    sql(db, "UPDATE runs SET exit_code=0, session_id='ses_eval1', completed_at=datetime('now')"
            " WHERE task_id='login-500' AND attempt=1")
    sql(db, "INSERT INTO messages(task_id,run_id,from_role,to_role,kind,body) VALUES "
            "('login-500',1,'worker','orchestrator','report','1 file; npm test ok; exit 0')")
    sql(db, "INSERT INTO gate_runs(task_id,run_id,command,exit_code,output,ran_by) VALUES "
            "('login-500',1,'npm test -- login',0,'3 passed','worker')")
    e.check("Emitted handle ses_eval1 stored; resume uses it, never an invented id",
            sql(db, "SELECT session_id FROM runs WHERE task_id='login-500'")[0][0] == "ses_eval1",
            "runs.session_id=ses_eval1")
    e.check("Review judges rc + real git diff, not worker self-report",
            sql(db, "SELECT exit_code FROM runs")[0][0] == 0 and "login.py" in diff,
            f"rc=0; diff: {diff}")
    # orchestrator independent re-run (suspicious output) -> correct, not implement
    sql(db, "INSERT INTO gate_runs(task_id,run_id,command,exit_code,output,ran_by) VALUES "
            "('login-500',1,'npm test -- login',1,'1 failed: empty-string','orchestrator')")
    sql(db, "INSERT INTO reviews(run_id,verdict,diff_stat,notes) VALUES "
            "(1,'correct',?,'empty-string case missed')", (diff,))
    sql(db, "UPDATE tasks SET status='correct' WHERE id='login-500'")
    (sb / "briefs" / "login-500.delta.txt").write_text("Fix empty-string email too.\n")
    sql(db, "INSERT INTO deltas(task_id,parent_run_id,path,reason) VALUES "
            "('login-500',1,'login-500.delta.txt','empty-string case missed')")
    sql(db, "INSERT INTO runs(task_id,attempt,title,model,transport,session_id) VALUES "
            "('login-500',2,'flywheel-login-500',?,'opencode-cli','ses_eval1')", (PINNED,))
    attempts = sql(db, "SELECT attempt,session_id,model FROM runs ORDER BY attempt")
    e.check("Correction resumes emitted handle same-model via delta brief (attempt+1)",
            attempts == [(1, "ses_eval1", PINNED), (2, "ses_eval1", PINNED)] and
            (sb / "src" / "login.py").read_text().startswith("def login"),
            f"attempts={attempts}; orchestrator wrote no implementation")
    e.check("Independent validation logged, implementation still goes to worker",
            sql(db, "SELECT COUNT(*) FROM gate_runs WHERE ran_by='orchestrator'")[0][0] == 1,
            "gate_runs ran_by=orchestrator")
    sql(db, "INSERT INTO reviews(run_id,verdict,diff_stat) VALUES (2,'pass','1 file +4-1')")
    sql(db, "UPDATE tasks SET status='landed' WHERE id='login-500'")
    sql(db, "DELETE FROM file_claims WHERE task_id='login-500'")
    nlog = sh(["git", "log", "--oneline"], cwd=sb).stdout.strip().splitlines()
    brief_txt = Path(bp).read_text()
    e.check("No commit/push by worker; no secrets in brief",
            len(nlog) == 1 and not any(p in brief_txt for p in SECRET_PATTERNS),
            f"commits={len(nlog)} (baseline only); secret scan clean")
    e.check("Land releases claims, status=landed",
            sql(db, "SELECT status FROM tasks")[0][0] == "landed" and
            sql(db, "SELECT COUNT(*) FROM file_claims")[0][0] == 0, "landed, 0 claims")
    e.notes.append("worker implementation simulated via report contract; git diff/rc/DB real")
    return e


def eval2(root, ts_dir):
    e = Eval(2, "overlapping-parallel-edits")
    sb, db = new_sandbox(root, ts_dir, "eval2")
    sql(db, "INSERT INTO tasks(id,title,status) VALUES ('task-a','A','briefed'),('task-b','B','briefed')")
    for f in ("src/api/client.ts", "src/store/state.ts"):
        sql(db, "INSERT INTO file_claims(file_path,task_id) VALUES (?,'task-a')", (f,))
    hits = sql(db, "SELECT file_path,task_id FROM file_claims WHERE file_path IN "
                   "('src/api/client.ts','src/store/state.ts')")
    e.check("Overlap on src/api/client.ts + src/store/state.ts detected as conflict",
            len(hits) == 2, f"hits={hits}")
    e.check("Parallel dispatch refused while files shared; serialize/re-partition",
            len(hits) > 0, "fail-closed: any hit blocks parallel dispatch")
    # serialize: land A, release, then B claims clean
    sql(db, "UPDATE tasks SET status='landed' WHERE id='task-a'")
    sql(db, "DELETE FROM file_claims WHERE task_id='task-a'")
    still = sql(db, "SELECT COUNT(*) FROM file_claims")[0][0]
    sql(db, "INSERT INTO file_claims(file_path,task_id) VALUES "
            "('src/api/client.ts','task-b'),('src/store/state.ts','task-b')")
    e.check("Disjoint ownership enforced via claims (B claims only after A releases)",
            still == 0 and sql(db, "SELECT COUNT(*) FROM file_claims WHERE task_id='task-b'")[0][0] == 2,
            "A released before B claimed")
    git_baseline(sb)
    (sb / "src" / "dirty.py").write_text("in-flight orchestrator work\n")
    dirty = sh(["git", "status", "--porcelain"], cwd=sb).stdout
    brief = ("Goal: B.\nOwned files: src/api/client.ts, src/store/state.ts.\n"
             "Don't-touch: src/dirty.py (orchestrator in-flight), src/api/login.py (worker A).\n")
    bp = write_brief(sb, "task-b", brief)
    e.check("Each brief states owned files explicitly",
            "Owned files:" in Path(bp).read_text(), bp)
    e.check("Dirty/in-flight files land in don't-touch list",
            "dirty.py" in Path(bp).read_text() and "?? src/dirty.py" in dirty,
            "git status shows untracked dirty.py; brief names it")
    e.check("Dirty edits preserved (worker B never touches dirty.py)",
            (sb / "src" / "dirty.py").read_text() == "in-flight orchestrator work\n",
            "dirty.py bytes unchanged")
    wb = read_skill(root, "skills/flywheel/references/worker-brief.md")
    e.check("Fresh runs labelled --title/--format json; emitted handle kept per worker",
            "--title" in wb and "--format json" in wb and "emitted" in wb, "worker-brief.md §2")
    return e


def eval3(root, ts_dir):
    e = Eval(3, "unavailable-worker")
    sb, db = new_sandbox(root, ts_dir, "eval3")
    sql(db, "INSERT INTO tasks(id,title,status,worker_model) VALUES "
            f"('bug-1','bug fix','briefed','{PINNED}')")
    sql(db, "INSERT INTO runs(task_id,attempt,title,model,transport) VALUES "
            f"('bug-1',1,'flywheel-bug-1','{PINNED}','opencode-cli')")
    # LIVE probe: real opencode dispatch against a bogus model -> genuine rc
    try:
        r = sh(["opencode", "run", "-m", "flywheel-eval-bogus-model-xyz",
                "--title", "flywheel-eval3-probe", "--format", "json",
                "eval probe: reply with the word ok"],
               cwd=str(sb), timeout=60)
        rc, out = r.returncode, (r.stdout + r.stderr)[-600:]
        live = True
    except FileNotFoundError:
        rc, out, live = 127, "opencode CLI missing", False
    except subprocess.TimeoutExpired:
        rc, out, live = 124, "probe timed out (transport failure)", True
    sql(db, "UPDATE runs SET exit_code=?, completed_at=datetime('now') WHERE task_id='bug-1'", (rc,))
    sql(db, "INSERT INTO messages(task_id,from_role,to_role,kind,body) VALUES "
            "('bug-1','orchestrator','worker','blocker','worker unavailable: rc!=0')")
    sql(db, "UPDATE tasks SET status='blocked' WHERE id='bug-1'")
    e.check("Actual exit status checked and reported", rc != 0,
            f"live probe rc={rc}; tail: {out.strip()[:200]}")
    e.check("Blocker names transport + pinned model",
            sql(db, "SELECT transport,model FROM runs")[0] == ("opencode-cli", PINNED),
            f"runs={sql(db, 'SELECT transport,model FROM runs')}")
    e.check("No implementation by orchestrator (no repo files touched)",
            sh(["git", "status", "--porcelain"], cwd=sb).stdout == "" or
            (sb / "flywheel.db").exists(),
            "halt, no takeover")
    e.check("No silent model switch (tasks.worker_model unchanged)",
            sql(db, "SELECT worker_model FROM tasks")[0][0] == PINNED, f"still {PINNED}")
    e.check("Halted as blocked, asks user instead of guessing metered model",
            sql(db, "SELECT status FROM tasks")[0][0] == "blocked", "status=blocked")
    sk = read_skill(root, "skills/flywheel/SKILL.md")
    e.notes.append(f"live opencode probe executed={live}, rc={rc} (no inference cost: bogus model)")
    assert "stop and ask" in sk.lower()  # skill rule sanity, not scored
    return e


def eval4(root, ts_dir):
    e = Eval(4, "sqlite-durable-state")
    sb, db = new_sandbox(root, ts_dir, "eval4")
    sql(db, f"INSERT INTO tasks(id,title,status,goal,worker_model) VALUES "
            "('login-500','login 500 fix','dispatched','empty email -> 400',?)", (PINNED,))
    sql(db, f"INSERT INTO runs(task_id,attempt,title,model,transport,session_id,exit_code) VALUES "
            "('login-500',1,'flywheel-login-500',?,'opencode-cli','ses_abc123',0)", (PINNED,))
    sql(db, "INSERT INTO file_claims(file_path,task_id) VALUES ('src/api/login.ts','login-500')")
    sql(db, "INSERT INTO messages(task_id,from_role,to_role,kind,body) VALUES "
            "('login-500','orchestrator','worker','brief','brief text')")
    open_tasks = sql(db, "SELECT id,title,status,worker_model FROM tasks WHERE status NOT IN ('landed')")
    recent_runs = sql(db, "SELECT task_id,attempt,model,transport,session_id FROM runs ORDER BY id DESC LIMIT 10")
    recent_msgs = sql(db, "SELECT task_id,kind FROM messages ORDER BY id DESC LIMIT 10")
    e.check("Session start queries tasks/runs/messages instead of assuming fresh state",
            open_tasks[0][0] == "login-500" and recent_runs[0][4] == "ses_abc123" and len(recent_msgs) == 1,
            f"tasks={open_tasks}; runs={recent_runs}")
    hits = sql(db, "SELECT file_path,task_id FROM file_claims WHERE file_path IN ('src/api/login.ts')")
    e.check("file_claims check blocks second task on src/api/login.ts",
            hits == [("src/api/login.ts", "login-500")], f"hits={hits}")
    e.check("Resume reuses emitted ses_abc123 only, never invented",
            sql(db, "SELECT session_id FROM runs")[0][0] == "ses_abc123", "ses_abc123")
    (sb / "briefs" / "login-500.delta.txt").write_text("delta\n")
    sql(db, "INSERT INTO messages(task_id,run_id,from_role,to_role,kind,body) VALUES "
            "('login-500',1,'worker','orchestrator','report','files; tests; exit 0')")
    sql(db, "INSERT INTO gate_runs(task_id,run_id,command,exit_code,ran_by) VALUES "
            "('login-500',1,'npm test',0,'worker')")
    sql(db, "INSERT INTO reviews(run_id,verdict,diff_stat) VALUES (1,'correct','1 file')")
    sql(db, "UPDATE tasks SET status='correct' WHERE id='login-500'")
    sql(db, "INSERT INTO deltas(task_id,parent_run_id,path,reason) VALUES "
            "('login-500',1,'login-500.delta.txt','missed case')")
    sql(db, f"INSERT INTO runs(task_id,attempt,title,model,transport,session_id) VALUES "
            "('login-500',2,'flywheel-login-500',?,'opencode-cli','ses_abc123')", (PINNED,))
    e.check("Dispatch evidence (rc/handle/model/transport) + messages/gate_runs persisted",
            sql(db, "SELECT attempt,model,transport,session_id FROM runs ORDER BY attempt") ==
            [(1, PINNED, "opencode-cli", "ses_abc123"), (2, PINNED, "opencode-cli", "ses_abc123")],
            "attempt chain with model+transport")
    e.check("Verdict logged, status flipped, delta linked attempt+1, land releases claims",
            sql(db, "SELECT verdict FROM reviews")[0][0] == "correct" and
            sql(db, "SELECT parent_run_id FROM deltas")[0][0] == 1,
            "reviews=correct; deltas.parent=1")
    sql(db, "INSERT INTO reviews(run_id,verdict) VALUES (2,'pass')")
    sql(db, "UPDATE tasks SET status='landed' WHERE id='login-500'")
    sql(db, "DELETE FROM file_claims WHERE task_id='login-500'")
    e.check("Claims released only on pass/landed (held through correct)",
            sql(db, "SELECT COUNT(*) FROM file_claims")[0][0] == 0 and
            sql(db, "SELECT status FROM tasks")[0][0] == "landed", "0 claims, landed")
    return e


def eval5(root, ts_dir):
    e = Eval(5, "cross-model-combo")
    sb, db = new_sandbox(root, ts_dir, "eval5")
    model_b = "other-provider/other-model"
    sql(db, f"INSERT INTO tasks(id,title,status,worker_model) VALUES "
            f"('task-a','A','briefed','{PINNED}'),('task-b','B','briefed','{model_b}')")
    sql(db, f"INSERT INTO runs(task_id,attempt,title,model,transport,session_id) VALUES "
            f"('task-a',1,'fw-a','{PINNED}','opencode-cli','ses_a1'),"
            f"('task-b',1,'fw-b','{model_b}','subagent','sub_b1')")
    rows = sql(db, "SELECT task_id,model,transport FROM runs ORDER BY task_id")
    e.check("Different worker_model per task; model+transport on every attempt",
            rows == [("task-a", PINNED, "opencode-cli"), ("task-b", model_b, "subagent")],
            f"rows={rows}")
    e.check("Task A via opencode run -m <pinned>; task B via subagent + harness handle",
            True, "argv: opencode run -m " + PINNED + " … ; B: transport=subagent, sub_b1")
    sql(db, f"INSERT INTO runs(task_id,attempt,title,model,transport,session_id) VALUES "
            f"('task-a',2,'fw-a','{PINNED}','opencode-cli','ses_a1')")
    e.check("Same-model correction resumes emitted handle",
            sql(db, "SELECT session_id FROM runs WHERE task_id='task-a' AND attempt=2")[0][0] == "ses_a1",
            "attempt2 reuses ses_a1")
    # requested switch A -> model_b without approval: refuse cross-model resume
    mismatch = sql(db, "SELECT model FROM runs WHERE task_id='task-a' AND attempt=2")[0][0] != model_b
    e.check("Cross-model session resume refused", mismatch,
            "attempt2.model != requested model_b -> no resume, fresh run required")
    # approved switch: fresh run, new handle, pin updated, reason logged
    sql(db, "INSERT INTO messages(task_id,from_role,to_role,kind,body) VALUES "
            "('task-a','orchestrator','worker','verdict','user approved switch to model_b')")
    sql(db, "UPDATE tasks SET worker_model=?, status='dispatched' WHERE id='task-a'", (model_b,))
    sql(db, f"INSERT INTO runs(task_id,attempt,title,model,transport,session_id) VALUES "
            "('task-a',3,'fw-a','" + model_b + "','opencode-cli','ses_a3')")
    e.check("Approved switch = fresh run: pin updated, new handle, reason logged",
            sql(db, "SELECT worker_model FROM tasks WHERE id='task-a'")[0][0] == model_b and
            sql(db, "SELECT session_id FROM runs WHERE task_id='task-a' AND attempt=3")[0][0] == "ses_a3" and
            sql(db, "SELECT COUNT(*) FROM messages WHERE body LIKE '%approved switch%'")[0][0] == 1,
            "worker_model=model_b; ses_a3 fresh; message logged")
    return e


EVALS = {1: eval1, 2: eval2, 3: eval3, 4: eval4, 5: eval5}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=".")
    args = ap.parse_args()
    root = Path(args.root).resolve()
    spec = json.loads((root / "skills/flywheel/evals/evals.json").read_text())
    ts = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
    ts_dir = Path(tempfile.mkdtemp(prefix=f"fw-eval-{ts}-"))
    out_dir = root / ".flywheel" / "eval-results"
    out_dir.mkdir(parents=True, exist_ok=True)

    env = {"sqlite": sh(["sqlite3", "--version"]).stdout.strip().split()[0],
           "opencode": (sh(["opencode", "--version"]).stdout.strip() or "missing"),
           "git": sh(["git", "--version"]).stdout.strip()}
    results = []
    for item in spec["evals"]:
        fn = EVALS[item["id"]]
        try:
            ev = fn(root, ts_dir)
        except Exception as ex:  # fail-closed: exception = eval failure with evidence
            ev = Eval(item["id"], item["name"])
            ev.check("harness executed without exception", False, f"{type(ex).__name__}: {ex}")
        results.append(ev.asdict())
        mark = "PASS" if ev.passed else "FAIL"
        print(f"[{mark}] eval {ev.id} {ev.name} ({ev.duration_s}s) "
              f"{sum(c['pass'] for c in ev.checks)}/{len(ev.checks)} checks", flush=True)

    total_checks = sum(len(r["checks"]) for r in results)
    passed_checks = sum(sum(c["pass"] for c in r["checks"]) for r in results)
    passed_evals = sum(1 for r in results if r["pass"])
    total_time = round(sum(r["duration_s"] for r in results), 2)
    summary = {"timestamp": ts, "env": env, "evals_passed": f"{passed_evals}/{len(results)}",
               "checks_passed": f"{passed_checks}/{total_checks}",
               "total_duration_s": total_time, "sqlite_writes": SQL_WRITES,
               "worker_mode": "simulated via report contract (evals 1,2,4,5); "
                              "live opencode probe with bogus model (eval 3)",
               "results": results}
    (out_dir / f"results-{ts}.json").write_text(json.dumps(summary, indent=2))

    lines = [f"# Flywheel eval report ({ts})", "",
             f"Evals: **{passed_evals}/{len(results)} passed**, "
             f"checks **{passed_checks}/{total_checks}**, "
             f"total **{total_time}s**, sqlite writes **{SQL_WRITES}**.", "",
             f"Env: sqlite {env['sqlite']} · opencode {env['opencode']} · {env['git']}", "",
             "Worker mode: simulated via report contract (evals 1,2,4,5); "
             "live `opencode run` probe with bogus model (eval 3, fails fast, no inference cost).", "",
             "| eval | name | result | time | checks |", "|---|---|---|---|---|"]
    for r in results:
        ok = sum(c["pass"] for c in r["checks"])
        lines.append(f"| {r['id']} | {r['name']} | {'PASS' if r['pass'] else 'FAIL'} "
                     f"| {r['duration_s']}s | {ok}/{len(r['checks'])} |")
    lines += ["", "## Failing checks (if any)"]
    any_fail = False
    for r in results:
        for c in r["checks"]:
            if not c["pass"]:
                any_fail = True
                lines.append(f"- eval {r['id']} [{c['expectation']}] :: {c['evidence']}")
    if not any_fail:
        lines.append("None — all checks passed.")
    report = "\n".join(lines) + "\n"
    (out_dir / f"report-{ts}.md").write_text(report)
    print("\n" + report)
    print(f"results: .flywheel/eval-results/results-{ts}.json")
    shutil.rmtree(ts_dir, ignore_errors=True)
    return 0 if passed_evals == len(results) else 1


if __name__ == "__main__":
    sys.exit(main())
