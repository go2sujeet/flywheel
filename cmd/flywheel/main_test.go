package main

import (
	"flag"
	"strings"
	"testing"
)

// TestHelpTextConvertedCommands checks that every command converted in this
// increment renders help starting with its usage and naming every flag its
// FlagSet defines.
func TestHelpTextConvertedCommands(t *testing.T) {
	converted := []struct {
		name  string
		flags func() *flag.FlagSet
	}{
		{"version", nil},
		{"init", func() *flag.FlagSet { fs, _ := initFlags(); return fs }},
		{"log", func() *flag.FlagSet { fs, _ := logFlags(); return fs }},
		{"state", func() *flag.FlagSet { fs, _ := stateFlags(); return fs }},
		{"config", nil},
	}
	for _, c := range converted {
		h := helpText(c.name)
		wantPrefix := "usage: flywheel " + c.name
		if !strings.HasPrefix(h, wantPrefix) {
			t.Errorf("%s: helpText = %q, want prefix %q", c.name, h, wantPrefix)
		}
		if c.flags == nil {
			continue
		}
		c.flags().VisitAll(func(f *flag.Flag) {
			if !strings.Contains(h, f.Name) {
				t.Errorf("%s: helpText missing flag %q\n%s", c.name, f.Name, h)
			}
		})
	}
}

// TestHelpTextFallback checks that a command without registerHelp falls back
// to its name and summary.
func TestHelpTextFallback(t *testing.T) {
	for _, name := range []string{"run", "validate", "inspect", "verify", "factory"} {
		h := helpText(name)
		wantPrefix := "usage: flywheel " + name
		if !strings.HasPrefix(h, wantPrefix) {
			t.Errorf("%s: fallback helpText = %q, want prefix %q", name, h, wantPrefix)
		}
		if !strings.Contains(h, commands[name].summary) {
			t.Errorf("%s: fallback helpText missing summary\n%s", name, h)
		}
	}
}

// TestHasHelpFlag checks the help detector finds -h/--help/-help and stops at
// a literal "--".
func TestHasHelpFlag(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"-h"}, true},
		{[]string{"--help"}, true},
		{[]string{"-help"}, true},
		{[]string{"--task", "foo", "-h"}, true},
		{[]string{"-h", "--"}, true},
		{[]string{"--", "-h"}, false},
		{[]string{"--", "--help"}, false},
		{[]string{"x", "y"}, false},
		{[]string{}, false},
	}
	for _, c := range cases {
		if got := hasHelpFlag(c.args); got != c.want {
			t.Errorf("hasHelpFlag(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestGlobalHelpEndsWithHint checks the global usage points at help for flags.
func TestGlobalHelpEndsWithHint(t *testing.T) {
	g := globalHelp()
	if !strings.HasSuffix(strings.TrimRight(g, "\n"), "Run 'flywheel help <command>' for its flags.") {
		t.Errorf("globalHelp does not end with the help hint:\n%s", g)
	}
}
