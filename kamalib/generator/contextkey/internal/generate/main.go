// Command generate runs a workflow spec in the working directory.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"go.yaml.in/yaml/v4"

	_ "github.com/ngicks/kama/kamalib/generator/contextkey"
	_ "github.com/ngicks/kama/kamalib/proc/fileloader"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func main() {
	specPath := flag.String("spec", "spec.yaml", "workflow spec file")
	flag.Parse()

	if err := run(context.Background(), *specPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, specPath string) error {
	b, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}

	var spec workflow.Spec
	if err := yaml.Unmarshal(b, &spec); err != nil {
		return fmt.Errorf("decode %q: %w", specPath, err)
	}

	g, err := spec.Build(ctx, workflow.DefaultRegistry)
	if err != nil {
		return err
	}

	return g.Run(ctx, workflow.ProgressControl{
		OnWarning: func(step string, w error) {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", step, w)
		},
	})
}
