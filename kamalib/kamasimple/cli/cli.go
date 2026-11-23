// Package cli holds the terminal output of the kama-simple command.
package cli

import (
	"fmt"
	"io"

	"github.com/ngicks/kama/kamalib/workflow"
)

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
