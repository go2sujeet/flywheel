package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"flywheel/internal/flywheel"
)

func init() {
	register("factory", "live dashboard of the factory floor", runFactory)
}

func runFactory(args []string) {
	fs := flag.NewFlagSet("factory", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "target directory (default: current working directory)")
	once := fs.Bool("once", false, "render the floor once and exit")
	asJSON := fs.Bool("json", false, "print one JSON snapshot and exit")
	interval := fs.Duration("interval", 2*time.Second, "redraw interval in live mode")
	width := fs.Int("width", 100, "render width in columns")
	now := fs.String("now", "", "RFC3339 instant to render at (default: the real clock); makes a screenshot reproducible")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "flywheel factory: %v\n", err)
		usage(os.Stderr)
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "flywheel factory: unexpected argument %q\n", fs.Arg(0))
		usage(os.Stderr)
		os.Exit(2)
	}
	if *interval <= 0 {
		fmt.Fprintf(os.Stderr, "flywheel factory: --interval must be positive, got %v\n", *interval)
		usage(os.Stderr)
		os.Exit(2)
	}
	clock, err := clockFor(*now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel factory: --now %q is not RFC3339: %v\n", *now, err)
		usage(os.Stderr)
		os.Exit(2)
	}
	w := flywheel.NewWatcher()
	color := flywheel.EnableANSI()
	if *asJSON || *once {
		render(w, *dir, *width, *asJSON, color, clock)
		return
	}
	if !color {
		// stdout is not an ANSI terminal (a pipe or a redirected file), so live
		// mode would never be seen and would only hang an automated caller.
		// Render once, plain text, and exit 0 exactly like --once.
		render(w, *dir, *width, false, color, clock)
		return
	}
	pulse(w, *dir, *width, *interval, color, clock)
}

// clockFor turns the --now flag into a clock. "" returns the real clock, read
// on every call; an RFC3339 value returns a clock fixed to that instant; any
// other value returns an error.
func clockFor(nowFlag string) (func() time.Time, error) {
	if nowFlag == "" {
		return time.Now, nil
	}
	t, err := time.Parse(time.RFC3339, nowFlag)
	if err != nil {
		return nil, err
	}
	return func() time.Time { return t }, nil
}

// render draws a single snapshot and exits; json selects the JSON form.
func render(w flywheel.Watcher, dir string, width int, asJSON bool, color bool, now func() time.Time) {
	f, err := w.Refresh(dir, now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "flywheel factory: %v\n", err)
		os.Exit(1)
	}
	if asJSON {
		flywheel.RenderJSON(os.Stdout, f)
		return
	}
	flywheel.RenderText(os.Stdout, f, width, color)
}

// pulse runs the live dashboard: clear and redraw every tick until Ctrl-C.
func pulse(w flywheel.Watcher, dir string, width int, interval time.Duration, color bool, now func() time.Time) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		fmt.Print("\x1b[H\x1b[2J")
		f, err := w.Refresh(dir, now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "flywheel factory: %v\n", err)
			os.Exit(1)
		}
		flywheel.RenderText(os.Stdout, f, width, color)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
