package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"

	"flywheel/internal/flywheel"
)

var version = "dev" // release builds set it with -ldflags "-X main.version=<tag>"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		runVersion(os.Args[2:])
	case "init":
		runInit(os.Args[2:])
	case "log":
		runLog(os.Args[2:])
	case "state":
		runState(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "flywheel: unknown subcommand %q\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "usage: flywheel <subcommand>\n\nsubcommands:\n  init      scaffold flywheel state files into a directory\n  log       append an event to the flywheel event log\n  state     derive and print flywheel state\n  version   print the flywheel version\n")
}

func runVersion(args []string) {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel version: %v\n", err)
		usage(os.Stderr)
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "flywheel version: unexpected argument %q\n", fs.Arg(0))
		usage(os.Stderr)
		os.Exit(2)
	}
	fmt.Println("flywheel " + version)
}

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "target directory (default: current working directory)")
	force := fs.Bool("force", false, "reset existing flywheel.md and .flywheel/state.json (directories and symlinks are still refused)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel init: %v\n", err)
		usage(os.Stderr)
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "flywheel init: unexpected argument %q\n", fs.Arg(0))
		usage(os.Stderr)
		os.Exit(2)
	}
	path, err := flywheel.Init(*dir, *force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel init: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(path)
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

func countKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func runState(args []string) {
	fs := flag.NewFlagSet("state", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "target directory (default: current working directory)")
	asJSON := fs.Bool("json", false, "print the derived state as JSON")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel state: %v\n", err)
		usage(os.Stderr)
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "flywheel state: unexpected argument %q\n", fs.Arg(0))
		usage(os.Stderr)
		os.Exit(2)
	}
	st, err := flywheel.WriteState(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel state: %v\n", err)
		os.Exit(1)
	}
	if *asJSON {
		b, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "flywheel state: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
		return
	}
	for _, ts := range st.Tasks {
		fmt.Printf("%s  %s  attempts=%d  %s\n", ts.ID, ts.Status, ts.Attempts, ts.Model)
	}
	ids := countKeys(st.Counts)
	slices.Sort(ids)
	line := ""
	for _, k := range ids {
		if line != "" {
			line = line + " "
		}
		line = line + fmt.Sprintf("%s=%d", k, st.Counts[k])
	}
	if line == "" {
		fmt.Println("counts")
	} else {
		fmt.Println("counts " + line)
	}
}
