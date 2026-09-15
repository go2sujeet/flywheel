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
	registerHelp("verify", "flywheel verify [<task>...] [--all] [--json] [--dir DIR]", func() *flag.FlagSet { fs, _ := verifyFlags(); return fs })
}

// verifyOptions holds the parsed verify flags.
type verifyOptions struct {
	dir     string
	all     bool
	jsonOut bool
}

// verifyFlags defines verify's flags once, so help and run share them.
func verifyFlags() (*flag.FlagSet, *verifyOptions) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := &verifyOptions{}
	fs.StringVar(&o.dir, "dir", ".", "target directory")
	fs.BoolVar(&o.all, "all", false, "verify every task in the event log")
	fs.BoolVar(&o.jsonOut, "json", false, "print machine-readable JSON")
	return fs, o
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
	fs, o := verifyFlags()
	pos, perr := parseArgs(fs, args)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "flywheel verify: %v\n", perr)
		verifyUsage(os.Stderr)
		os.Exit(2)
	}
	tasks := pos
	res, err := flywheel.VerifyTasks(o.dir, flywheel.VerifyOptions{Dir: o.dir, Tasks: tasks, All: o.all})
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel verify: %v\n", err)
		os.Exit(1)
	}
	if o.jsonOut {
		b, _ := json.Marshal(res)
		fmt.Println(string(b))
		if res.Passed {
			os.Exit(0)
		}
		os.Exit(6)
	}
	if len(res.Items) == 0 {
		fmt.Println("nothing to verify")
		os.Exit(0)
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
