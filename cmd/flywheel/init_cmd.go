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
	registerHelp("init", "flywheel init [flags]", func() *flag.FlagSet { fs, _ := initFlags(); return fs })
}

// initOptions holds the parsed init flags.
type initOptions struct {
	dir   string
	force bool
}

// initFlags defines init's flags once, so help and run share them.
func initFlags() (*flag.FlagSet, *initOptions) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := &initOptions{}
	o.dir = *fs.String("dir", ".", "target directory")
	o.force = *fs.Bool("force", false, "reset existing flywheel.md and .flywheel/state.json (directories and symlinks are still refused)")
	return fs, o
}

func runInit(args []string) {
	fs, o := initFlags()
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
	path, err := flywheel.Init(o.dir, o.force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel init: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(path)
}
