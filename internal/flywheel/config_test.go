package flywheel

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadConfigMissingFileReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, exists, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if exists {
		t.Fatal("LoadConfig() exists = true, want false for a missing file")
	}
	if !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Errorf("LoadConfig() = %+v, want default %+v", cfg, DefaultConfig())
	}
}

func TestWriteConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	if err := WriteConfig(dir, cfg); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, ".flywheel", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Error("config.json does not end with a trailing newline")
	}

	got, exists, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !exists {
		t.Fatal("LoadConfig() exists = false, want true after WriteConfig")
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("LoadConfig() = %+v, want %+v", got, cfg)
	}
}

func TestConfigValidateCollectsProblems(t *testing.T) {
	cfg := Config{
		Version: 2,
		Workers: []Worker{
			{Name: "Bad Name!", Adapter: "nope", MaxParallel: -1,
				Fallbacks: []Fallback{{Model: "fb"}, {Model: ""}}},
			{Name: "Bad Name!", Adapter: "sim", Model: "m",
				Fallbacks: []Fallback{{Model: "m"}}},
		},
		Limits:   Limits{PerHost: -3},
		Feedback: Feedback{Submit: "maybe"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want every problem reported")
	}
	msg := err.Error()
	for _, want := range []string{
		"version: got 2, want 1",
		`name "Bad Name!" must match`,
		`adapter "nope"`,
		"model must not be empty",
		"max_parallel -1 must be >= 0",
		"duplicate name",
		"fallbacks[1] model must not be empty",
		`fallback model "m" must differ`,
		"limits.per_host -3 must be >= 0",
		`feedback.submit "maybe"`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("Validate() error missing %q; got:\n%s", want, msg)
		}
	}
	if lines := strings.Split(msg, "\n"); len(lines) < 9 {
		t.Errorf("Validate() error has %d lines, want one problem per line:\n%s", len(lines), msg)
	}
}

func TestLoadConfigRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".flywheel", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"version":1,"workers":[{"name":"default","adapter":"opencode","model":"m","bogus":1}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	_, _, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("LoadConfig() = nil error, want rejection of an unknown field")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("LoadConfig() error = %v, want it to name the file %s", err, path)
	}
}

func TestConfigGet(t *testing.T) {
	cfg := Config{
		Version: 1,
		Workers: []Worker{
			{Name: "default", Adapter: "opencode", Model: "m0", Variant: "v0", MaxParallel: 4,
				Fallbacks: []Fallback{{Model: "f1", Approved: true}, {Model: "f2"}}},
			{Name: "extra", Adapter: "sim", Model: "m1",
				Fallbacks: []Fallback{{Model: "f3", Approved: true}}},
		},
		Limits:   Limits{PerHost: 7},
		Feedback: Feedback{Upstream: "owner/repo", Submit: "never"},
	}
	cases := []struct {
		key, want string
	}{
		{"model", "m0"},
		{"variant", "v0"},
		{"adapter", "opencode"},
		{"max_parallel", "4"},
		{"fallbacks", "f1"},
		{"fallbacks.all", "f1,f2"},
		{"workers.default.model", "m0"},
		{"workers.default.fallbacks", "f1"},
		{"workers.extra.model", "m1"},
		{"workers.extra.adapter", "sim"},
		{"workers.extra.max_parallel", "0"},
		{"workers.extra.fallbacks", "f3"},
		{"workers.extra.fallbacks.all", "f3"},
		{"feedback.upstream", "owner/repo"},
		{"feedback.submit", "never"},
		{"limits.per_host", "7"},
	}
	for _, tc := range cases {
		got, err := cfg.Get(tc.key)
		if err != nil {
			t.Errorf("Get(%q) error = %v", tc.key, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Get(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestConfigGetUnknownKeyListsValidKeys(t *testing.T) {
	cfg := DefaultConfig()
	_, err := cfg.Get("bogus")
	if err == nil {
		t.Fatal("Get(\"bogus\") = nil error, want error listing valid keys")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bogus") {
		t.Errorf("Get() error = %q, want mention of the unknown key", msg)
	}
	for _, k := range []string{
		"model", "variant", "adapter", "max_parallel", "fallbacks", "fallbacks.all",
		"workers.default.model", "feedback.upstream", "feedback.submit", "limits.per_host",
	} {
		if !strings.Contains(msg, k) {
			t.Errorf("Get() error missing valid key %q; got:\n%s", k, msg)
		}
	}
}

func TestConfigWorkerLookup(t *testing.T) {
	cfg := DefaultConfig()
	if w, ok := cfg.Worker("default"); !ok || w.Model != "openrouter/deepseek/deepseek-v4-flash-0731" {
		t.Errorf("Worker(default) = %+v, %v, want the default worker", w, ok)
	}
	if _, ok := cfg.Worker("nope"); ok {
		t.Error("Worker(nope) = found, want not found")
	}
	if got := cfg.DefaultWorker(); got.Name != "default" {
		t.Errorf("DefaultWorker() = %+v, want the first worker", got)
	}
}
