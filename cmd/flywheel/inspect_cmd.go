package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"flywheel/internal/flywheel"
)

func init() {
	register("inspect", "inspect a task against the poka-yoke rules", runInspect)
	registerHelp("inspect", "flywheel inspect <task> --verdict pass|rework|scrap|escalate --session <session> [--note NOTE] [--dir DIR] [--workdir PATH]", func() *flag.FlagSet { fs, _ := inspectFlags(); return fs })
}

// inspectOptions holds the parsed inspect flags.
type inspectOptions struct {
	dir     string
	workdir string
	verdict string
	session string
	note    string
}

// inspectFlags defines inspect's flags once, so help and run share them.
func inspectFlags() (*flag.FlagSet, *inspectOptions) {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := &inspectOptions{}
	o.dir = *fs.String("dir", ".", "target directory")
	o.workdir = *fs.String("workdir", "", "git working tree to hash")
	o.verdict = *fs.String("verdict", "", "pass, rework, scrap, or escalate")
	o.session = *fs.String("session", "", "inspector session, distinct from every worker session")
	o.note = *fs.String("note", "", "optional inspection note")
	return fs, o
}

// inspectUsage prints the flywheel inspect usage line.
func inspectUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: flywheel inspect <task> --verdict pass|rework|scrap|escalate --session <session> [--note NOTE] [--dir DIR] [--workdir PATH]")
}

// runInspect implements `flywheel inspect <task>`. Each poka-yoke refusal
// exits 6 with the rule id and the fix; a usage error exits 2; any other
// error exits 1.
func runInspect(args []string) {
	fs, o := inspectFlags()
	var task string
	var parseArgs []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		task = args[0]
		parseArgs = args[1:]
	} else {
		parseArgs = args
	}
	if err := fs.Parse(parseArgs); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel inspect: %v\n", err)
		inspectUsage(os.Stderr)
		os.Exit(2)
	}
	if task == "" {
		if fs.NArg() != 1 {
			fmt.Fprintf(os.Stderr, "flywheel inspect: exactly one task id is required\n")
			inspectUsage(os.Stderr)
			os.Exit(2)
		}
		task = fs.Arg(0)
	}
	err := flywheel.InspectTask(o.dir, task, flywheel.InspectOptions{
		Dir: o.dir, Workdir: o.workdir, Verdict: o.verdict, Session: o.session, Note: o.note,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel inspect: %v\n", err)
		if flywheel.IsRuleRefusal(err) {
			os.Exit(6)
		}
		os.Exit(1)
	}
	fmt.Printf("%s inspected %s\n", task, o.verdict)
}
