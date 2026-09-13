package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"flywheel/internal/flywheel"
)

func init() {
	register("log", "append an event to the flywheel event log", runLog)
}

// appendEvents appends each event and then derives state unless noState.
func appendEvents(dir string, events []flywheel.Event, noState bool) {
	for _, e := range events {
		if err := flywheel.AppendEvent(dir, e); err != nil {
			fmt.Fprintf(os.Stderr, "flywheel log: %v\n", err)
			os.Exit(1)
		}
	}
	if noState {
		return
	}
	if _, err := flywheel.WriteState(dir); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel log: %v\n", err)
		os.Exit(1)
	}
}

// runLogJSON appends events read (strictly) from a file or stdin, one JSON
// object per line.
func runLogJSON(dir, path string, noState bool) {
	if path == "-" {
		events, err := flywheel.ParseEvents(os.Stdin, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "flywheel log: %v\n", err)
			os.Exit(1)
		}
		appendEvents(dir, events, noState)
		return
	}
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel log: %v\n", err)
		os.Exit(1)
	}
	events, err := flywheel.ParseEvents(f, true)
	f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel log: %v\n", err)
		os.Exit(1)
	}
	appendEvents(dir, events, noState)
}

func runLog(args []string) {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "target directory (default: current working directory)")
	jsonIn := fs.String("json", "", "file of events to append, or - for stdin")
	task := fs.String("task", "", "task id")
	kind := fs.String("kind", "", "event kind")
	session := fs.String("session", "", "session id")
	model := fs.String("model", "", "model name")
	attempt := fs.String("attempt", "", "attempt (r1, c1, ...)")
	rc := fs.String("rc", "", "exit code")
	reason := fs.String("reason", "", "finish reason or classification")
	verdict := fs.String("verdict", "", "pass, correct, or reject")
	brief := fs.String("brief", "", "brief file")
	commit := fs.String("commit", "", "commit id")
	note := fs.String("note", "", "free-form note")
	noState := fs.Bool("no-state", false, "skip state derivation after appending")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel log: %v\n", err)
		usage(os.Stderr)
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "flywheel log: unexpected argument %q\n", fs.Arg(0))
		usage(os.Stderr)
		os.Exit(2)
	}
	if *jsonIn != "" {
		runLogJSON(*dir, *jsonIn, *noState)
		return
	}

	var e flywheel.Event
	e.Task = *task
	e.Kind = *kind
	e.Session = *session
	e.Model = *model
	e.Attempt = *attempt
	e.Reason = *reason
	e.Verdict = *verdict
	e.Brief = *brief
	e.Commit = *commit
	e.Note = *note
	if *rc != "" {
		v, err := strconv.ParseInt(*rc, 10, strconv.IntSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "flywheel log: invalid --rc %q: %v\n", *rc, err)
			os.Exit(1)
		}
		p := new(int)
		*p = int(v)
		e.RC = p
	}
	appendEvents(*dir, []flywheel.Event{e}, *noState)
}
