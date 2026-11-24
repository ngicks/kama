// Package cli is the command line of kama-simple: its flags, its terminal
// output and its run.
//
// A third party extends kama-simple with its own generators by writing a
// main package that registers them, usually by blank-importing their
// packages, and calls Main:
//
//	package main
//
//	import (
//		"os"
//
//		_ "example.com/acme/greet"
//
//		"github.com/ngicks/kama/kamalib/kamasimple/cli"
//	)
//
//	func main() { os.Exit(cli.Main("acme-kama", os.Args[1:])) }
//
// The command knows the procs of kamalib and those the imports register.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ngicks/kama/kamalib/kamasimple"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Main runs the command name with args, the command line arguments
// without the command name, and returns its exit status. It writes to the
// standard error and stops the run on an interrupt or SIGTERM.
//
//	name [-C dir] [--spec file] [-v]
//
// The steps of the spec read and write the project in the working
// directory. -C changes to dir first, and --spec is resolved after that
// change, as git -C does.
func Main(name string, args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := Run(ctx, name, args, os.Stderr); err != nil {
		Error(os.Stderr, name, err)
		return 1
	}

	return 0
}

// Run parses args as Main does and runs the spec with kamasimple.Run,
// writing the usage, the warnings and the progress to stderr. Asking for
// the usage is no error.
func Run(ctx context.Context, name string, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("C", "", "change to `dir` before doing anything else")
	spec := flags.String("spec", "kama.yaml", "workflow spec `file`, resolved after -C")
	verbose := flags.Bool("v", false, "print the progress of every step")

	if err := flags.Parse(args); err != nil {
		// The flag set has printed the usage already.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if *dir != "" {
		if err := os.Chdir(*dir); err != nil {
			return err
		}
	}

	return kamasimple.Run(ctx, kamasimple.Options{
		Spec:     *spec,
		Progress: Progress(stderr, *verbose),
	})
}

// Progress returns the ProgressControl writing to w what a run reports: a
// line for each warning, and with verbose a line for each progress message
// of a step too. A warning with an empty step is written without one.
func Progress(w io.Writer, verbose bool) workflow.ProgressControl {
	pc := workflow.ProgressControl{
		OnWarning: func(step string, warning error) {
			if step == "" {
				fmt.Fprintf(w, "warning: %v\n", warning)
				return
			}

			fmt.Fprintf(w, "warning: %s: %v\n", step, warning)
		},
	}
	if verbose {
		pc.OnProgress = func(step, msg string) { fmt.Fprintf(w, "%s: %s\n", step, msg) }
	}

	return pc
}

// Error writes err to w as the last line of a failed run of the command
// name.
func Error(w io.Writer, name string, err error) {
	fmt.Fprintf(w, "%s: %v\n", name, err)
}
