package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"flywheel/internal/flywheel"
)

func init() {
	register("run", "dispatch a worker for a task", runRun)
	registerHelp("run", "flywheel run <task> [--dir DIR] [--worker NAME] [--model MODEL] [--resume] [--delta FILE] [--start-timeout DURATION]", func() *flag.FlagSet { fs, _ := runFlags(); return fs })
}

// runOptions holds the parsed `flywheel run` flags.
type runOptions struct {
	dir          string
	worker       string
	model        string
	resume       bool
	delta        string
	startTimeout time.Duration
}

// runUsage prints the flywheel run usage line.
func runUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: flywheel run <task> [--dir DIR] [--worker NAME] [--model MODEL] [--resume] [--delta FILE] [--start-timeout DURATION]")
}

// runFlags defines run's flags once, so help and run share them.
func runFlags() (*flag.FlagSet, *runOptions) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := &runOptions{}
	o.dir = *fs.String("dir", ".", "target directory")
	o.worker = *fs.String("worker", "", "worker name")
	o.model = *fs.String("model", "", "model name")
	o.resume = *fs.Bool("resume", false, "resume the task's last session")
	o.delta = *fs.String("delta", "", "delta brief file")
	o.startTimeout = *fs.Duration("start-timeout", 60*time.Second, "startup timeout")
	return fs, o
}

// runRun implements `flywheel run <task>`: dispatch the configured worker,
// stream the run into .flywheel/runs/, and record every transition as an
// event. Exit codes: 0 clean stop, 3 start timeout, 4 failed run (nonzero rc,
// capped, or error), 2 usage, 1 any other error.
func runRun(args []string) {
	fs, o := runFlags()
	var task string
	var parseArgs []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		task = args[0]
		parseArgs = args[1:]
	} else {
		parseArgs = args
	}
	if err := fs.Parse(parseArgs); err != nil {
		if err == flag.ErrHelp {
			runUsage(os.Stderr)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "flywheel run: %v\n", err)
		runUsage(os.Stderr)
		os.Exit(2)
	}
	if task == "" {
		if fs.NArg() != 1 {
			fmt.Fprintf(os.Stderr, "flywheel run: exactly one task id is required\n")
			runUsage(os.Stderr)
			os.Exit(2)
		}
		task = fs.Arg(0)
	} else if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "flywheel run: unexpected argument %q\n", fs.Arg(0))
		runUsage(os.Stderr)
		os.Exit(2)
	}
	res, err := flywheel.Run(o.dir, flywheel.RunOptions{
		Task: task, Worker: o.worker, Model: o.model, Resume: o.resume,
		DeltaPath: o.delta, StartTimeout: o.startTimeout, Progress: os.Stdout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel run: %v\n", err)
		os.Exit(1)
	}
	os.Exit(flywheel.ExitCode(res))
}
