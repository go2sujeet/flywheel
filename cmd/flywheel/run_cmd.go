package main

import (
	"flag"
	"fmt"
	"io"
	"os"
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
	fs.StringVar(&o.dir, "dir", ".", "target directory")
	fs.StringVar(&o.worker, "worker", "", "worker name")
	fs.StringVar(&o.model, "model", "", "model name")
	fs.BoolVar(&o.resume, "resume", false, "resume the task's last session")
	fs.StringVar(&o.delta, "delta", "", "delta brief file")
	fs.DurationVar(&o.startTimeout, "start-timeout", 60*time.Second, "startup timeout")
	return fs, o
}

// runRun implements `flywheel run <task>`: dispatch the configured worker,
// stream the run into .flywheel/runs/, and record every transition as an
// event. Exit codes: 0 clean stop, 3 start timeout, 4 failed run (nonzero rc,
// capped, or error), 2 usage, 1 any other error.
func runRun(args []string) {
	fs, o := runFlags()
	pos, err := parseArgs(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			runUsage(os.Stderr)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "flywheel run: %v\n", err)
		runUsage(os.Stderr)
		os.Exit(2)
	}
	if len(pos) != 1 {
		fmt.Fprintf(os.Stderr, "flywheel run: exactly one task id is required\n")
		runUsage(os.Stderr)
		os.Exit(2)
	}
	task := pos[0]
	res, err := flywheel.Run(o.dir, flywheel.RunOptions{
		Task: task, Worker: o.worker, Model: o.model, Resume: o.resume,
		DeltaPath: o.delta, StartTimeout: o.startTimeout, Progress: os.Stdout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel run: %v\n", err)
		if flywheel.IsNoWorkerSession(err) {
			os.Exit(2)
		}
		os.Exit(1)
	}
	os.Exit(flywheel.ExitCode(res))
}
