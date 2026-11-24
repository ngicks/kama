// Command kama-simple runs a workflow spec in a directory.
//
//	kama-simple [-C dir] [--spec file] [-v]
//
// The steps of the spec read and write the project in the working
// directory. -C changes to dir first, and --spec is resolved after that
// change, as git -C does.
package main

import (
	"os"

	"github.com/ngicks/kama/kamalib/kamasimple/cli"
)

func main() { os.Exit(cli.Main("kama-simple", os.Args[1:])) }
