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

// parseRunArgs splits `flywheel run` arguments into the task and the flags,
// accepting flags before or after the positional task.
func parseRunArgs(args []string) (task string, opts runOptions, err error) {
	opts.dir = "."
	opts.startTimeout = 60 * time.Second
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			return "", opts, flag.ErrHelp
		case a == "-dir" || a == "--dir":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("flag needs an argument: %s", a)
			}
			i++
			opts.dir = args[i]
		case a == "-worker" || a == "--worker":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("flag needs an argument: %s", a)
			}
			i++
			opts.worker = args[i]
		case a == "-model" || a == "--model":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("flag needs an argument: %s", a)
			}
			i++
			opts.model = args[i]
		case a == "-delta" || a == "--delta":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("flag needs an argument: %s", a)
			}
			i++
			opts.delta = args[i]
		case a == "-start-timeout" || a == "--start-timeout":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("flag needs an argument: %s", a)
			}
			i++
			d, perr := time.ParseDuration(args[i])
			if perr != nil {
				return "", opts, fmt.Errorf("invalid --start-timeout %q: %v", args[i], perr)
			}
			opts.startTimeout = d
		case a == "-resume" || a == "--resume":
			opts.resume = true
		case strings.HasPrefix(a, "--dir=") || strings.HasPrefix(a, "-dir="):
			opts.dir = flagValue(a)
		case strings.HasPrefix(a, "--worker=") || strings.HasPrefix(a, "-worker="):
			opts.worker = flagValue(a)
		case strings.HasPrefix(a, "--model=") || strings.HasPrefix(a, "-model="):
			opts.model = flagValue(a)
		case strings.HasPrefix(a, "--delta=") || strings.HasPrefix(a, "-delta="):
			opts.delta = flagValue(a)
		case strings.HasPrefix(a, "--start-timeout=") || strings.HasPrefix(a, "-start-timeout="):
			d, perr := time.ParseDuration(flagValue(a))
			if perr != nil {
				return "", opts, fmt.Errorf("invalid --start-timeout %q: %v", flagValue(a), perr)
			}
			opts.startTimeout = d
		case strings.HasPrefix(a, "-") && a != "-":
			return "", opts, fmt.Errorf("flag provided but not defined: %s", a)
		default:
			if task != "" {
				return "", opts, fmt.Errorf("unexpected argument %q", a)
			}
			task = a
		}
	}
	if task == "" {
		return "", opts, fmt.Errorf("exactly one task id is required")
	}
	return task, opts, nil
}

// flagValue returns the value after the first '=' in a flag argument.
func flagValue(a string) string {
	return a[strings.IndexByte(a, '=')+1:]
}

// runRun implements `flywheel run <task>`: dispatch the configured worker,
// stream the run into .flywheel/runs/, and record every transition as an
// event. Exit codes: 0 clean stop, 3 start timeout, 4 failed run (nonzero rc,
// capped, or error), 2 usage, 1 any other error.
func runRun(args []string) {
	task, opts, err := parseRunArgs(args)
	if err != nil {
		if err == flag.ErrHelp {
			runUsage(os.Stderr)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "flywheel run: %v\n", err)
		runUsage(os.Stderr)
		os.Exit(2)
	}
	res, err := flywheel.Run(opts.dir, flywheel.RunOptions{
		Task: task, Worker: opts.worker, Model: opts.model, Resume: opts.resume,
		DeltaPath: opts.delta, StartTimeout: opts.startTimeout, Progress: os.Stdout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel run: %v\n", err)
		os.Exit(1)
	}
	os.Exit(flywheel.ExitCode(res))
}
