package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"flywheel/internal/flywheel"
)

func init() {
	register("init", "scaffold flywheel state files into a directory", runInit)
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
