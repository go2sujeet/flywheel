package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func init() {
	register("version", "print the flywheel version", runVersion)
	registerHelp("version", "flywheel version", nil)
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
