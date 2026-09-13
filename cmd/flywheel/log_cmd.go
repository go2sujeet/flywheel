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
	registerHelp("log", "flywheel log [flags]", func() *flag.FlagSet { fs, _ := logFlags(); return fs })
}

// logOptions holds the parsed log flags.
type logOptions struct {
	dir     string
	jsonIn  string
	task    string
	kind    string
	session string
	model   string
	attempt string
	rc      string
	reason  string
	verdict string
	brief   string
	commit  string
	note    string
	noState bool
}

// logFlags defines log's flags once, so help and run share them.
func logFlags() (*flag.FlagSet, *logOptions) {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := &logOptions{}
	o.dir = *fs.String("dir", ".", "target directory")
	o.jsonIn = *fs.String("json", "", "file of events to append, or - for stdin")
	o.task = *fs.String("task", "", "task id")
	o.kind = *fs.String("kind", "", "event kind")
	o.session = *fs.String("session", "", "session id")
	o.model = *fs.String("model", "", "model name")
	o.attempt = *fs.String("attempt", "", "attempt (r1, c1, ...)")
	o.rc = *fs.String("rc", "", "exit code")
	o.reason = *fs.String("reason", "", "finish reason or classification")
	o.verdict = *fs.String("verdict", "", "pass, correct, or reject")
	o.brief = *fs.String("brief", "", "brief file")
	o.commit = *fs.String("commit", "", "commit id")
	o.note = *fs.String("note", "", "free-form note")
	o.noState = *fs.Bool("no-state", false, "skip state derivation after appending")
	return fs, o
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
	fs, o := logFlags()
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
	if o.jsonIn != "" {
		runLogJSON(o.dir, o.jsonIn, o.noState)
		return
	}

	var e flywheel.Event
	e.Task = o.task
	e.Kind = o.kind
	e.Session = o.session
	e.Model = o.model
	e.Attempt = o.attempt
	e.Reason = o.reason
	e.Verdict = o.verdict
	e.Brief = o.brief
	e.Commit = o.commit
	e.Note = o.note
	if o.rc != "" {
		v, err := strconv.ParseInt(o.rc, 10, strconv.IntSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "flywheel log: invalid --rc %q: %v\n", o.rc, err)
			os.Exit(1)
		}
		p := new(int)
		*p = int(v)
		e.RC = p
	}
	appendEvents(o.dir, []flywheel.Event{e}, o.noState)
}
