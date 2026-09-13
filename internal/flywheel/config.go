package flywheel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const configFileName = "config.json"

var workerNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Config is the project configuration stored in .flywheel/config.json.
type Config struct {
	Version  int      `json:"version"`
	Workers  []Worker `json:"workers"`
	Limits   Limits   `json:"limits,omitempty"`
	Feedback Feedback `json:"feedback,omitempty"`
}

// Worker configures a single CLI worker.
type Worker struct {
	Name        string     `json:"name"`
	Adapter     string     `json:"adapter"` // "opencode" or "sim" in phase 1
	Model       string     `json:"model"`
	Variant     string     `json:"variant,omitempty"`
	MaxParallel int        `json:"max_parallel,omitempty"` // 0 means 1
	Fallbacks   []Fallback `json:"fallbacks,omitempty"`
}

// Fallback is a model to fall back to when the worker's model is unavailable.
type Fallback struct {
	Model    string `json:"model"`
	Approved bool   `json:"approved,omitempty"` // standing OK to switch without asking
}

// Limits caps shared resource use across workers.
type Limits struct {
	PerHost int     `json:"per_host,omitempty"`
	Budget  *Budget `json:"budget,omitempty"`
}

// Budget caps spending per wave.
type Budget struct {
	WaveCostUSD float64 `json:"wave_cost_usd,omitempty"`
}

// Feedback configures how results flow upstream.
type Feedback struct {
	Upstream string `json:"upstream,omitempty"` // owner/repo
	Submit   string `json:"submit,omitempty"`   // "ask" (default) or "never"
}

// DefaultConfig returns the built-in configuration used when no
// .flywheel/config.json exists.
func DefaultConfig() Config {
	return Config{
		Version: 1,
		Workers: []Worker{{
			Name:        "default",
			Adapter:     "opencode",
			Model:       "openrouter/deepseek/deepseek-v4-flash-0731",
			MaxParallel: 4,
		}},
		Feedback: Feedback{
			Upstream: "go2sujeet/flywheel",
			Submit:   "ask",
		},
	}
}

// LoadConfig reads <dir>/.flywheel/config.json. A missing file returns the
// default configuration and exists=false. Malformed JSON or unknown fields
// is an error naming the file; a successfully parsed configuration is then
// validated.
func LoadConfig(dir string) (Config, bool, error) {
	path := filepath.Join(dir, ".flywheel", configFileName)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), false, nil
		}
		return Config{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, false, err
	}
	return c, true, nil
}

// Validate checks the configuration and reports every problem found, one per
// line, in a single error.
func (c Config) Validate() error {
	var problems []string
	if c.Version != 1 {
		problems = append(problems, fmt.Sprintf("version: got %d, want 1", c.Version))
	}
	if len(c.Workers) == 0 {
		problems = append(problems, "at least one worker is required")
	}
	seen := make(map[string]bool)
	for i, w := range c.Workers {
		where := fmt.Sprintf("workers[%d]", i)
		if w.Name == "" {
			problems = append(problems, where+": name must not be empty")
		} else {
			if !workerNameRe.MatchString(w.Name) {
				problems = append(problems, fmt.Sprintf("%s: name %q must match ^[a-z0-9][a-z0-9-]*$", where, w.Name))
			}
			if seen[w.Name] {
				problems = append(problems, fmt.Sprintf("%s: duplicate name %q", where, w.Name))
			}
			seen[w.Name] = true
		}
		if w.Adapter != "opencode" && w.Adapter != "sim" {
			problems = append(problems, fmt.Sprintf("%s: adapter %q must be \"opencode\" or \"sim\"", where, w.Adapter))
		}
		if w.Model == "" {
			problems = append(problems, where+": model must not be empty")
		}
		if w.MaxParallel < 0 {
			problems = append(problems, fmt.Sprintf("%s: max_parallel %d must be >= 0", where, w.MaxParallel))
		}
		for j, f := range w.Fallbacks {
			switch {
			case f.Model == "":
				problems = append(problems, fmt.Sprintf("%s: fallbacks[%d] model must not be empty", where, j))
			case f.Model == w.Model:
				problems = append(problems, fmt.Sprintf("%s: fallback model %q must differ from the worker's model", where, f.Model))
			}
		}
	}
	if c.Limits.PerHost < 0 {
		problems = append(problems, fmt.Sprintf("limits.per_host %d must be >= 0", c.Limits.PerHost))
	}
	switch c.Feedback.Submit {
	case "", "ask", "never":
	default:
		problems = append(problems, fmt.Sprintf("feedback.submit %q must be empty, \"ask\", or \"never\"", c.Feedback.Submit))
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "\n"))
}

// Worker returns the worker with the given name.
func (c Config) Worker(name string) (Worker, bool) {
	for _, w := range c.Workers {
		if w.Name == name {
			return w, true
		}
	}
	return Worker{}, false
}

// DefaultWorker returns the first worker, which owns the bare keys in Get.
func (c Config) DefaultWorker() Worker {
	if len(c.Workers) == 0 {
		return Worker{}
	}
	return c.Workers[0]
}

// Get returns a configuration value for scripts and skills. Bare keys apply
// to the default worker; every worker key is also addressable as
// workers.<name>.<key>.
func (c Config) Get(key string) (string, error) {
	if v, ok := c.defaultKey(key); ok {
		return v, nil
	}
	if rest, ok := strings.CutPrefix(key, "workers."); ok {
		if dot := strings.IndexByte(rest, '.'); dot > 0 {
			if w, ok := c.Worker(rest[:dot]); ok {
				if v, ok := workerValue(w, rest[dot+1:]); ok {
					return v, nil
				}
			}
		}
	}
	switch key {
	case "feedback.upstream":
		return c.Feedback.Upstream, nil
	case "feedback.submit":
		return c.Feedback.Submit, nil
	case "limits.per_host":
		return strconv.Itoa(c.Limits.PerHost), nil
	}
	return "", fmt.Errorf("unknown key %q; valid keys: %s", key, strings.Join(c.validKeys(), ", "))
}

// defaultKey resolves the bare worker keys against the default worker.
func (c Config) defaultKey(key string) (string, bool) {
	if len(c.Workers) == 0 {
		return "", false
	}
	return workerValue(c.Workers[0], key)
}

// workerValue resolves a worker-scoped key.
func workerValue(w Worker, key string) (string, bool) {
	switch key {
	case "model":
		return w.Model, true
	case "variant":
		return w.Variant, true
	case "adapter":
		return w.Adapter, true
	case "max_parallel":
		return strconv.Itoa(w.MaxParallel), true
	case "fallbacks":
		return joinFallbacks(w.Fallbacks, true), true
	case "fallbacks.all":
		return joinFallbacks(w.Fallbacks, false), true
	}
	return "", false
}

// joinFallbacks lists fallback models, comma-separated, optionally restricted
// to approved ones.
func joinFallbacks(fbs []Fallback, approvedOnly bool) string {
	var models []string
	for _, f := range fbs {
		if approvedOnly && !f.Approved {
			continue
		}
		models = append(models, f.Model)
	}
	return strings.Join(models, ",")
}

// validKeys lists every key Get accepts, including each worker's keys.
func (c Config) validKeys() []string {
	keys := []string{
		"adapter", "fallbacks", "fallbacks.all", "feedback.submit",
		"feedback.upstream", "limits.per_host", "max_parallel", "model", "variant",
	}
	for _, w := range c.Workers {
		for _, k := range []string{"adapter", "fallbacks", "fallbacks.all", "max_parallel", "model", "variant"} {
			keys = append(keys, "workers."+w.Name+"."+k)
		}
	}
	sort.Strings(keys)
	return keys
}

// WriteConfig validates c and writes it to <dir>/.flywheel/config.json as
// 2-space-indented JSON with a trailing newline. The write goes through a
// temp file and rename so readers never observe partial output; .flywheel/ is
// created if missing.
func WriteConfig(dir string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	b = append(b, '\n')
	dotFlywheel := filepath.Join(dir, ".flywheel")
	if err := os.MkdirAll(dotFlywheel, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dotFlywheel, err)
	}
	path := filepath.Join(dotFlywheel, configFileName)
	tmp, err := os.CreateTemp(dotFlywheel, ".config.json.tmp*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}
