package main

import (
	"os"
	"strings"
	"testing"
)

// TestDocsCommandTable checks every registered command appears in the
// flywheel-operator skill's command table as "flywheel <name>", so the
// documented CLI never drifts from the implemented one.
func TestDocsCommandTable(t *testing.T) {
	data, err := os.ReadFile("../../skills/flywheel-operator/SKILL.md")
	if err != nil {
		t.Errorf("read skills/flywheel-operator/SKILL.md: %v", err)
		return
	}
	for name := range commands {
		if !strings.Contains(string(data), "flywheel "+name) {
			t.Errorf("command %q is missing from the flywheel-operator SKILL.md command table; add `flywheel %s` to it", name, name)
		}
	}
}
