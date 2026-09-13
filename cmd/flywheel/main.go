package main

import (
	"fmt"
	"io"
	"os"
	"slices"
)

var version = "dev" // release builds set it with -ldflags "-X main.version=<tag>"

// command is one flywheel subcommand: its entry point and one-line summary.
type command struct {
	run     func([]string)
	summary string
}

// commands holds every registered subcommand. Each command file registers
// itself from its own init() function, so adding a command never touches
// main.go.
var commands = map[string]command{}

// register adds a subcommand under name.
func register(name string, summary string, run func([]string)) {
	commands[name] = command{run: run, summary: summary}
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	if c, ok := commands[os.Args[1]]; ok {
		c.run(os.Args[2:])
		return
	}
	fmt.Fprintf(os.Stderr, "flywheel: unknown subcommand %q\n", os.Args[1])
	usage(os.Stderr)
	os.Exit(2)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: flywheel <subcommand>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "subcommands:")
	names := make([]string, 0, len(commands))
	for k := range commands {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %-10s %s\n", name, commands[name].summary)
	}
}
