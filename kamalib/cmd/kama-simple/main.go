// Command kama-simple runs a workflow spec in a directory.
//
//	kama-simple [-C dir] [--spec file] [-v]
//
// The steps of the spec read and write the project in the working
// directory. -C changes to dir first, and --spec is resolved after that
// change, as git -C does.
package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/ngicks/kama/kamalib/kamasimple"
	"github.com/ngicks/kama/kamalib/kamasimple/cli"
)

const name = "kama-simple"

func main() {
	if err := run(os.Args[1:]); err != nil {
		cli.Error(os.Stderr, name, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return kamasimple.Run(ctx, kamasimple.Options{
		Spec:     *spec,
		Progress: cli.Progress(os.Stderr, *verbose),
	})
}
