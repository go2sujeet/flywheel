package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"flywheel/internal/flywheel"
)

func init() {
	register("init", "scaffold flywheel state files into a directory", runInit)
	registerHelp("init", "flywheel init [flags]", func() *flag.FlagSet { fs, _ := initFlags(); return fs })
}

// initOptions holds the parsed init flags.
type initOptions struct {
	dir     string
	force   bool
	model   string
	variant string
}

// initFlags defines init's flags once, so help and run share them.
func initFlags() (*flag.FlagSet, *initOptions) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := &initOptions{}
	fs.StringVar(&o.dir, "dir", ".", "target directory")
	fs.StringVar(&o.model, "model", "", "seed a new .flywheel/config.json's default worker model")
	fs.StringVar(&o.variant, "variant", "", "seed a new .flywheel/config.json's default worker variant")
	fs.BoolVar(&o.force, "force", false, "reset existing flywheel.md and .flywheel/state.json (directories and symlinks are still refused)")
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
	configExisted := false
	if _, err := os.Stat(filepath.Join(o.dir, ".flywheel", "config.json")); err == nil {
		configExisted = true
	}
	path, created, err := flywheel.InitSeeded(o.dir, o.force, o.model, o.variant)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel init: %v\n", err)
		os.Exit(1)
	}
	if len(created) == 0 && !o.force {
		fmt.Printf("init: nothing to do (%s present)\n", strings.Join(presentScaffoldFiles(o.dir), ", "))
	} else {
		fmt.Println(path)
		for _, p := range created {
			fmt.Printf("created: %s\n", p)
		}
		fmt.Println("next: flywheel log --task <id> --kind planned --brief <path>")
	}
	if configExisted && (o.model != "" || o.variant != "") {
		fmt.Println("config.json exists; change it with: flywheel config set model|variant <value>")
	}
	ignored := flywheel.IgnoredStateFiles(path)
	if len(ignored) > 0 {
		for _, p := range ignored {
			fmt.Fprintf(os.Stderr, "warning: git ignores %s; flywheel's state would never be committed\n", p)
		}
		fmt.Fprintln(os.Stderr, "add to .gitignore:")
		for _, p := range ignored {
			fmt.Fprintln(os.Stderr, "!"+p)
		}
	}
}

// presentScaffoldFiles lists which of the scaffold files init creates already
// exist in dir, as relative paths, for the quiet nothing-to-do message.
func presentScaffoldFiles(dir string) []string {
	var present []string
	for _, name := range []string{"flywheel.md", ".flywheel/state.json", ".flywheel/events.jsonl", ".flywheel/config.json", ".flywheel/.gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err == nil {
			present = append(present, name)
		}
	}
	return present
}
