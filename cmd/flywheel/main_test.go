package main

import (
	"flag"
	"strings"
	"testing"
)

// TestHelpTextAllCommands ranges over the commands map itself: every
// registered command must have registerHelp, its help must start with the
// "flywheel <name>" usage, and the help must name every flag its FlagSet
// defines.
func TestHelpTextAllCommands(t *testing.T) {
	for name := range commands {
		c := commands[name]
		if c.usage == "" {
			t.Errorf("%s: registered without registerHelp", name)
			continue
		}
		h := helpText(name)
		wantPrefix := "usage: flywheel " + name
		if !strings.HasPrefix(h, wantPrefix) {
			t.Errorf("%s: helpText = %q, want prefix %q", name, h, wantPrefix)
		}
		if c.flags == nil {
			continue
		}
		c.flags().VisitAll(func(f *flag.Flag) {
			if !strings.Contains(h, f.Name) {
				t.Errorf("%s: helpText missing flag %q\n%s", name, f.Name, h)
			}
		})
	}
}

// TestBareAction checks the bare `flywheel` decision: open the factory view
// when ./.flywheel exists, print the global help otherwise.
func TestBareAction(t *testing.T) {
	if got := bareAction(true); got != "factory" {
		t.Errorf("bareAction(true) = %q, want %q", got, "factory")
	}
	if got := bareAction(false); got != "help" {
		t.Errorf("bareAction(false) = %q, want %q", got, "help")
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
