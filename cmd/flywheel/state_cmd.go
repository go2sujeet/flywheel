package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"

	"flywheel/internal/flywheel"
)

func init() {
	register("state", "derive and print flywheel state", runState)
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
