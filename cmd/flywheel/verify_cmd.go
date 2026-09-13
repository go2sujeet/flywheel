package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"flywheel/internal/flywheel"
)

func init() {
	register("verify", "verify tasks against the poka-yoke rules", runVerify)
}

// verifyUsage prints the flywheel verify usage line.
func verifyUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: flywheel verify [<task>...] [--all] [--json] [--dir DIR]")
}

// runVerify implements `flywheel verify`. It prints PASS or FAIL per check
// with the rule id and a reason. Exit 0 when every check passes, 6 on any
// failure, 2 on a usage error, 1 on any other error. --json emits the
// machine-readable result instead of the human lines.
func runVerify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "target directory (default: current working directory)")
	all := fs.Bool("all", false, "verify every task in the event log")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	var tasks []string
	var parseArgs []string
	for len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		tasks = append(tasks, args[0])
		args = args[1:]
	}
	parseArgs = args
	if err := fs.Parse(parseArgs); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel verify: %v\n", err)
		verifyUsage(os.Stderr)
		os.Exit(2)
	}
	tasks = append(tasks, fs.Args()...)
	res, err := flywheel.VerifyTasks(*dir, flywheel.VerifyOptions{Dir: *dir, Tasks: tasks, All: *all})
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel verify: %v\n", err)
		os.Exit(1)
	}
	if *jsonOut {
		b, _ := json.Marshal(res)
		fmt.Println(string(b))
		if res.Passed {
			os.Exit(0)
		}
		os.Exit(6)
	}
	for _, item := range res.Items {
		status := "PASS"
		if !item.Pass {
			status = "FAIL"
		}
		fmt.Printf("%s %s %s: %s\n", status, item.Task, item.Rule, item.Reason)
	}
	if res.Passed {
		os.Exit(0)
	}
	os.Exit(6)
}
