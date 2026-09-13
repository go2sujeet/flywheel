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
	register("validate", "run a task's gates and check owns", runValidate)
}

// validateUsage prints the flywheel validate usage line.
func validateUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: flywheel validate <task> [--dir DIR] [--workdir PATH]")
}

// runValidate implements `flywheel validate <task>`: run the task's declared
// gates on the exact tree and check that every changed path sits inside owns.
// Exit codes: 0 all gates pass and nothing is outside owns, 5 otherwise, 2
// usage, 1 any other error.
func runValidate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "target directory (default: current working directory)")
	workdir := fs.String("workdir", "", "git working tree the gates run in (default: --dir)")
	var task string
	var parseArgs []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		task = args[0]
		parseArgs = args[1:]
	} else {
		parseArgs = args
	}
	if err := fs.Parse(parseArgs); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel validate: %v\n", err)
		validateUsage(os.Stderr)
		os.Exit(2)
	}
	if task == "" {
		if fs.NArg() != 1 {
			fmt.Fprintf(os.Stderr, "flywheel validate: exactly one task id is required\n")
			validateUsage(os.Stderr)
			os.Exit(2)
		}
		task = fs.Arg(0)
	}
	res, err := flywheel.ValidateTask(*dir, task, flywheel.ValidateOptions{Dir: *dir, Workdir: *workdir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel validate: %v\n", err)
		os.Exit(1)
	}
	for _, g := range res.Gates {
		if g.HostBlocked {
			fmt.Printf("%s gate %s: host blocked the gate, rerun (rc=%d)\n", task, g.Gate, g.RC)
		} else if g.RC == 0 {
			fmt.Printf("%s gate %s: pass (%dms)\n", task, g.Gate, g.DurationMS)
		} else {
			fmt.Printf("%s gate %s: failed (rc=%d)\n", task, g.Gate, g.RC)
		}
	}
	if len(res.Outside) == 0 {
		fmt.Printf("%s owns: ok\n", task)
	} else {
		fmt.Printf("%s owns: outside %s\n", task, strings.Join(res.Outside, ", "))
	}
	if res.OK() {
		os.Exit(0)
	}
	os.Exit(5)
}
