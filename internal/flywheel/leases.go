package flywheel

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Lease records one in-flight worker run: the run holder's identity and the
// window in which the run is still alive. A run renews its lease until it
// records finished; a flywheel run that is killed leaves the file behind and
// the lease expires on its own.
type Lease struct {
	Task      string `json:"task"`
	Attempt   string `json:"attempt"`
	PID       int    `json:"pid"`
	Host      string `json:"host"`
	StartedAt string `json:"started_at"`
	RenewedAt string `json:"renewed_at"`
	ExpiresAt string `json:"expires_at"`
	RunFile   string `json:"run_file"`
}

// now is the clock every lease timestamp reads; tests replace it.
var now = time.Now

// leaseMu serializes lease file operations within this process: the renewer
// goroutine rewrites the lease file while reads (and the final removal)
// happen on the run's goroutine, and on Windows a rename cannot overlap a
// concurrent open of the same file.
var leaseMu sync.Mutex

// leasePath returns the lease file path for one attempt.
func leasePath(dir, task, attempt string) string {
	return filepath.Join(dir, ".flywheel", "leases", task+"."+attempt+".json")
}

// WriteLease writes l to .flywheel/leases/<task>.<attempt>.json via a temp
// file plus rename, creating the directory.
func WriteLease(dir string, l Lease) error {
	leaseMu.Lock()
	defer leaseMu.Unlock()
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lease: %w", err)
	}
	b = append(b, '\n')
	return atomicWrite(filepath.Join(dir, ".flywheel", "leases"),
		l.Task+"."+l.Attempt+".json", "lease-*.json", b)
}

// RemoveLease deletes the lease file for one attempt; a missing file is not
// an error.
func RemoveLease(dir, task, attempt string) error {
	leaseMu.Lock()
	defer leaseMu.Unlock()
	path := leasePath(dir, task, attempt)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove lease %s: %w", path, err)
	}
	return nil
}

// ReadLeases returns every lease in .flywheel/leases, sorted by task then
// attempt. A malformed file is skipped and named in the returned error; the
// other leases are still returned.
func ReadLeases(dir string) ([]Lease, error) {
	leaseMu.Lock()
	defer leaseMu.Unlock()
	leasesDir := filepath.Join(dir, ".flywheel", "leases")
	entries, err := os.ReadDir(leasesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Lease{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", leasesDir, err)
	}
	var leases []Lease
	var problems []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(leasesDir, e.Name())
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			problems = append(problems, fmt.Errorf("read %s: %w", path, rerr))
			continue
		}
		var l Lease
		if uerr := json.Unmarshal(b, &l); uerr != nil {
			problems = append(problems, fmt.Errorf("parse %s: %w", path, uerr))
			continue
		}
		leases = append(leases, l)
	}
	sort.Slice(leases, func(i, j int) bool {
		if leases[i].Task != leases[j].Task {
			return leases[i].Task < leases[j].Task
		}
		return compareAttempt(leases[i].Attempt, leases[j].Attempt) < 0
	})
	if len(problems) > 0 {
		return leases, errors.Join(problems...)
	}
	return leases, nil
}

// compareAttempt orders attempt ids: c1 < c2 < r1 < r2, numerically.
func compareAttempt(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" || b == "" {
		return strings.Compare(a, b)
	}
	if a[0] != b[0] {
		return strings.Compare(a, b)
	}
	return attemptNum(a) - attemptNum(b)
}

// LeaseLive reports whether the lease is still live at now: live while now is
// at or before expires_at. An unparseable expiry is not live.
func LeaseLive(l Lease, now time.Time) bool {
	exp, err := time.Parse(time.RFC3339, l.ExpiresAt)
	if err != nil {
		return false
	}
	return !now.After(exp)
}
