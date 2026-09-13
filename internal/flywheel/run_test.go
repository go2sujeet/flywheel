package flywheel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTask returns an initialized temp dir with a planned T1 event pointing
// at a brief file.
func setupTask(t *testing.T) string {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("one line brief\n"), 0o644); err != nil {
		t.Fatalf("write brief: %v", err)
	}
	if err := AppendEvent(dir, Event{TS: "2026-09-12T00:00:00Z", Task: "T1", Kind: "planned", Brief: "b.txt"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	return dir
}

// simConfig returns a config whose single worker replays the fixture model.
func simConfig(model string) Config {
	return Config{
		Version: 1,
		Workers: []Worker{{Name: "sim", Adapter: "sim", Model: model}},
	}
}

// shaOf returns the hex SHA-256 of the file at path.
func shaOf(path string, t *testing.T) string {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestRunSimClean(t *testing.T) {
	dir := setupTask(t)
	model := fixturePath("clean.jsonl", t)
	if err := WriteConfig(dir, simConfig(model)); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	res, err := Run(dir, RunOptions{Task: "T1", Progress: &buf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Attempt != "r1" {
		t.Errorf("attempt = %q, want r1", res.Attempt)
	}
	if res.Session != "ses_test_clean_001" {
		t.Errorf("session = %q, want ses_test_clean_001", res.Session)
	}
	if res.RC != 0 || res.Reason != "stop" {
		t.Errorf("rc/reason = %d/%q, want 0/stop", res.RC, res.Reason)
	}
	if res.Steps != 2 {
		t.Errorf("steps = %d, want 2", res.Steps)
	}
	if res.Tokens == nil || res.Tokens.Input != 200 || res.Tokens.Output != 70 ||
		res.Tokens.Reasoning != 15 || res.Tokens.CacheRead != 1000 || res.Tokens.CacheWrite != 5 {
		t.Errorf("tokens = %v, want 200/70/15/1000/5", res.Tokens)
	}
	if res.Cost < 0.00399 || res.Cost > 0.00401 {
		t.Errorf("cost = %v, want about 0.004", res.Cost)
	}
	if ExitCode(res) != 0 {
		t.Errorf("ExitCode() = %d, want 0", ExitCode(res))
	}

	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	if len(evs) != 6 {
		t.Fatalf("events = %d, want 6", len(evs))
	}
	if evs[0].Kind != "planned" {
		t.Errorf("evs[0] kind = %q, want planned", evs[0].Kind)
	}
	d := evs[1]
	briefSum := sha256.Sum256([]byte("one line brief\n"))
	if d.Kind != "dispatched" || d.Attempt != "r1" || d.Adapter != "sim" ||
		d.Model != model || d.Path != ".flywheel/runs/T1.r1.jsonl" ||
		d.SHA256 != hex.EncodeToString(briefSum[:]) {
		t.Errorf("dispatched event = %v", d)
	}
	policyB, err := os.ReadFile(filepath.Join(dir, ".flywheel", "opencode-worker.json"))
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	policySum := sha256.Sum256(policyB)
	if d.Note != "policy sha256="+hex.EncodeToString(policySum[:]) {
		t.Errorf("dispatched note = %q, want the policy sha256", d.Note)
	}
	if evs[2].Kind != "started" || evs[2].Session != "ses_test_clean_001" {
		t.Errorf("started event = %v", evs[2])
	}
	if evs[3].Kind != "worker_plan" || evs[3].Path != ".flywheel/runs/T1.r1.plan.md" || evs[3].SHA256 == "" {
		t.Errorf("worker_plan event = %v", evs[3])
	}
	if evs[4].Kind != "report" || evs[4].Path != ".flywheel/runs/T1.r1.report.md" || evs[4].SHA256 == "" {
		t.Errorf("report event = %v", evs[4])
	}
	f := evs[5]
	if f.Kind != "finished" || f.RC == nil || *f.RC != 0 || f.Reason != "stop" ||
		f.Steps != 2 || f.Tokens == nil || f.Tokens.Input != 200 ||
		f.Cost < 0.00399 || f.Cost > 0.00401 {
		t.Errorf("finished event = %v", f)
	}
	runSHA := shaOf(filepath.Join(dir, ".flywheel", "runs", "T1.r1.jsonl"), t)
	if f.SHA256 != runSHA {
		t.Errorf("finished sha256 = %q, want run file %q", f.SHA256, runSHA)
	}

	// The run file reproduces the fixture byte-for-byte; plan and report hold
	// the recorded texts.
	runB, err := os.ReadFile(filepath.Join(dir, ".flywheel", "runs", "T1.r1.jsonl"))
	if err != nil {
		t.Fatalf("read run file: %v", err)
	}
	fixtureB, err := os.ReadFile(model)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if string(runB) != string(fixtureB) {
		t.Error("run file does not reproduce the fixture byte-for-byte")
	}
	planB, err := os.ReadFile(filepath.Join(dir, ".flywheel", "runs", "T1.r1.plan.md"))
	if err != nil {
		t.Fatalf("read plan file: %v", err)
	}
	if string(planB) != "PLAN read the layout, then implement the command registry." {
		t.Errorf("plan file = %q", planB)
	}
	reportB, err := os.ReadFile(filepath.Join(dir, ".flywheel", "runs", "T1.r1.report.md"))
	if err != nil {
		t.Fatalf("read report file: %v", err)
	}
	if string(reportB) != "Implemented the command registry. Files changed: main.go. Tests pass." {
		t.Errorf("report file = %q", reportB)
	}
	plog := string(buf.Bytes())
	for _, want := range []string{
		"T1 r1 dispatched sim",
		"T1 r1 started ses_test_clean_001",
		"T1 r1 plan recorded",
		"T1 r1 report recorded",
		"T1 r1 finished rc=0 reason=stop steps=2",
	} {
		if !strings.Contains(plog, want) {
			t.Errorf("progress missing %q; got:\n%s", want, plog)
		}
	}
}

func TestRunSimAttemptNumbering(t *testing.T) {
	dir := setupTask(t)
	if err := WriteConfig(dir, simConfig(fixturePath("clean.jsonl", t))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	res1, err := Run(dir, RunOptions{Task: "T1", Progress: &buf})
	if err != nil {
		t.Fatalf("Run() 1 error = %v", err)
	}
	res2, err := Run(dir, RunOptions{Task: "T1", Progress: &buf})
	if err != nil {
		t.Fatalf("Run() 2 error = %v", err)
	}
	if res1.Attempt != "r1" || res2.Attempt != "r2" {
		t.Errorf("fresh attempts = %q, %q, want r1, r2", res1.Attempt, res2.Attempt)
	}

	delta := filepath.Join(dir, ".flywheel", "briefs", "T1.delta.txt")
	if err := os.MkdirAll(filepath.Dir(delta), 0o755); err != nil {
		t.Fatalf("mkdir briefs: %v", err)
	}
	if err := os.WriteFile(delta, []byte("fix the registry\n"), 0o644); err != nil {
		t.Fatalf("write delta: %v", err)
	}
	resC1, err := Run(dir, RunOptions{Task: "T1", Resume: true, Progress: &buf})
	if err != nil {
		t.Fatalf("Run() c1 error = %v", err)
	}
	resC2, err := Run(dir, RunOptions{Task: "T1", Resume: true, Progress: &buf})
	if err != nil {
		t.Fatalf("Run() c2 error = %v", err)
	}
	if resC1.Attempt != "c1" || resC2.Attempt != "c2" {
		t.Errorf("resume attempts = %q, %q, want c1, c2", resC1.Attempt, resC2.Attempt)
	}
	if resC1.Session != "ses_test_clean_001" {
		t.Errorf("c1 session = %q, want ses_test_clean_001", resC1.Session)
	}
}

func TestRunResumeWithoutSessionErrors(t *testing.T) {
	dir := setupTask(t)
	if err := WriteConfig(dir, simConfig(fixturePath("clean.jsonl", t))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	_, err := Run(dir, RunOptions{Task: "T1", Resume: true, Progress: &buf})
	if err == nil {
		t.Fatal("Run() resume without a session: got nil error, want refusal")
	} else if !strings.Contains(err.Error(), "no session") {
		t.Errorf("Run() error = %v, want 'no session'", err)
	}
}

func TestRunUnplannedTaskErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := WriteConfig(dir, simConfig(fixturePath("clean.jsonl", t))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	_, err := Run(dir, RunOptions{Task: "T1", Progress: &buf})
	if err == nil {
		t.Fatal("Run() unplanned task: got nil error, want refusal")
	} else if !strings.Contains(err.Error(), "no planned event") {
		t.Errorf("Run() error = %v, want 'no planned event'", err)
	}
}

func TestRunSimCapped(t *testing.T) {
	dir := setupTask(t)
	if err := WriteConfig(dir, simConfig(fixturePath("capped.jsonl", t))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	res, err := Run(dir, RunOptions{Task: "T1", Progress: &buf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.RC != 0 || res.Reason != "length" {
		t.Errorf("rc/reason = %d/%q, want 0/length", res.RC, res.Reason)
	}
	if ExitCode(res) != 4 {
		t.Errorf("ExitCode() = %d, want 4", ExitCode(res))
	}
}

func TestRunSimProviderError(t *testing.T) {
	dir := setupTask(t)
	if err := WriteConfig(dir, simConfig(fixturePath("provider-error.jsonl", t))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	res, err := Run(dir, RunOptions{Task: "T1", Progress: &buf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Reason != "error" {
		t.Errorf("reason = %q, want error", res.Reason)
	}
	if ExitCode(res) != 4 {
		t.Errorf("ExitCode() = %d, want 4", ExitCode(res))
	}
	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	if f := evs[len(evs)-1]; f.Kind != "finished" || f.Reason != "error" {
		t.Errorf("finished event = %v, want reason error", f)
	}
}

func TestRunStartTimeoutSilent(t *testing.T) {
	dir := setupTask(t)
	if err := WriteConfig(dir, simConfig(fixturePath("clean.jsonl", t))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	res, err := Run(dir, RunOptions{
		Task: "T1", StartTimeout: 50 * time.Millisecond, SimDelay: time.Second, Progress: &buf,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.RC != -1 || res.Reason != "silent" {
		t.Errorf("rc/reason = %d/%q, want -1/silent", res.RC, res.Reason)
	}
	if ExitCode(res) != 3 {
		t.Errorf("ExitCode() = %d, want 3", ExitCode(res))
	}
	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	if f := evs[len(evs)-1]; f.Kind != "finished" || f.Reason != "silent" {
		t.Errorf("finished event = %v, want reason silent", f)
	}
	runB, err := os.ReadFile(filepath.Join(dir, ".flywheel", "runs", "T1.r1.jsonl"))
	if err != nil {
		t.Fatalf("read run file: %v", err)
	}
	if len(runB) != 0 {
		t.Errorf("run file = %d bytes, want empty", len(runB))
	}
}

func TestWorkerEnvAndPolicy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".flywheel"), 0o755); err != nil {
		t.Fatalf("mkdir .flywheel: %v", err)
	}
	env := workerEnv(dir)
	found := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "OPENCODE_CONFIG=") {
			found = kv
		}
	}
	if found != "OPENCODE_CONFIG="+filepath.Join(dir, ".flywheel", "opencode-worker.json") {
		t.Errorf("workerEnv() = %q, want OPENCODE_CONFIG at the policy path", found)
	}
	sha, err := workerPolicySHA(dir)
	if err != nil {
		t.Fatalf("workerPolicySHA() error = %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".flywheel", "opencode-worker.json"))
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	if string(b) != workerPermissionPolicy {
		t.Errorf("policy file = %q, want the embedded policy", b)
	}
	sum := sha256.Sum256(b)
	if sha != hex.EncodeToString(sum[:]) {
		t.Errorf("workerPolicySHA() = %q, want %q", sha, hex.EncodeToString(sum[:]))
	}
}

func TestWorkerPolicyMatchesCanonicalFile(t *testing.T) {
	b, err := os.ReadFile("../../skills/flywheel/references/worker-permissions.json")
	if err != nil {
		t.Fatalf("read canonical policy: %v", err)
	}
	content := strings.ReplaceAll(string(b), "\r\n", "\n")
	if content != workerPermissionPolicy {
		t.Error("embedded workerPermissionPolicy differs from skills/flywheel/references/worker-permissions.json")
	}
	var doc struct {
		Schema     string `json:"$schema"`
		Permission struct {
			Bash map[string]string `json:"bash"`
		} `json:"permission"`
	}
	if err := json.Unmarshal([]byte(workerPermissionPolicy), &doc); err != nil {
		t.Fatalf("policy is not valid JSON: %v", err)
	}
	if doc.Permission.Bash["*"] != "allow" {
		t.Errorf(`permission.bash["*"] = %q, want allow`, doc.Permission.Bash["*"])
	}
	if doc.Permission.Bash["git stash*"] != "deny" {
		t.Errorf(`permission.bash["git stash*"] = %q, want deny`, doc.Permission.Bash["git stash*"])
	}
}

func TestRunLongRunWithEarlyFirstLineFinishesStop(t *testing.T) {
	dir := setupTask(t)
	if err := WriteConfig(dir, simConfig(fixturePath("clean.jsonl", t))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	res, err := Run(dir, RunOptions{
		Task: "T1", StartTimeout: 200 * time.Millisecond, SimLineDelay: 300 * time.Millisecond, Progress: &buf,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.RC != 0 || res.Reason != "stop" {
		t.Errorf("rc/reason = %d/%q, want 0/stop (a long run with an early first line is not silent)", res.RC, res.Reason)
	}
}

func TestRunCommandSessionOnlyOnResume(t *testing.T) {
	dir := setupTask(t)
	if err := WriteConfig(dir, simConfig(fixturePath("clean.jsonl", t))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	delta := filepath.Join(dir, ".flywheel", "briefs", "T1.delta.txt")
	if err := os.MkdirAll(filepath.Dir(delta), 0o755); err != nil {
		t.Fatalf("mkdir briefs: %v", err)
	}
	if err := os.WriteFile(delta, []byte("fix it\n"), 0o644); err != nil {
		t.Fatalf("write delta: %v", err)
	}
	var got []RunRequest
	commandHook = func(req RunRequest) { got = append(got, req) }
	defer func() { commandHook = nil }()

	var buf bytes.Buffer
	if _, err := Run(dir, RunOptions{Task: "T1", Progress: &buf}); err != nil {
		t.Fatalf("Run() fresh error = %v", err)
	}
	if _, err := Run(dir, RunOptions{Task: "T1", Resume: true, Progress: &buf}); err != nil {
		t.Fatalf("Run() resume error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("commandHook captured %d requests, want 2", len(got))
	}
	if got[0].Session != "" {
		t.Errorf("fresh run session = %q, want empty (a fresh run must not pass --session)", got[0].Session)
	}
	if got[1].Session != "ses_test_clean_001" {
		t.Errorf("resume session = %q, want ses_test_clean_001", got[1].Session)
	}
	for i, req := range got {
		if req.PromptFile == "" || !filepath.IsAbs(req.PromptFile) {
			t.Errorf("request %d PromptFile = %q, want an absolute path", i, req.PromptFile)
		}
	}
	if got[0].PromptFile != filepath.Join(dir, "b.txt") {
		t.Errorf("fresh PromptFile = %q, want the brief %q", got[0].PromptFile, filepath.Join(dir, "b.txt"))
	}
	if got[1].PromptFile != delta {
		t.Errorf("resume PromptFile = %q, want the delta %q", got[1].PromptFile, delta)
	}
}

func TestRunMissingFixtureRecordsFinished(t *testing.T) {
	dir := setupTask(t)
	if err := WriteConfig(dir, simConfig(filepath.Join(dir, "nope.jsonl"))); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}
	var buf bytes.Buffer
	_, err := Run(dir, RunOptions{Task: "T1", Progress: &buf})
	if err == nil {
		t.Fatal("Run() with a missing fixture: got nil error, want failure")
	}
	evs, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents() error = %v", err)
	}
	f := evs[len(evs)-1]
	if f.Kind != "finished" || f.Reason != "error" {
		t.Errorf("last event = %v, want finished reason error", f)
	}
	if f.Note == "" {
		t.Error("finished error event has no note")
	}
}

func TestWorkerEnvUsesAbsoluteConfigPath(t *testing.T) {
	env := workerEnv(".")
	found := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "OPENCODE_CONFIG=") {
			found = strings.TrimPrefix(kv, "OPENCODE_CONFIG=")
		}
	}
	if found == "" {
		t.Fatal("workerEnv() missing OPENCODE_CONFIG")
	}
	if !filepath.IsAbs(found) {
		t.Errorf("OPENCODE_CONFIG = %q, want an absolute path", found)
	}
}
