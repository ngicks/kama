// Package kamasimple runs a workflow spec with the procs kamalib ships.
//
// It is the plain kama interface: a spec file names steps, and the steps
// read and write the project in the working directory. Importing the
// package registers the procs of kamalib in workflow.DefaultRegistry.
//
// An unknown key of the spec file, of one of its steps or of a param fails
// nothing. Run reports each one as a warning wrapping
// workflow.ErrUnknownKey before the first step runs.
package kamasimple

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"

	// The procs a spec may name.
	_ "github.com/ngicks/kama/kamalib/generator/contextkey"
	_ "github.com/ngicks/kama/kamalib/generator/jsonschema"
	_ "github.com/ngicks/kama/kamalib/generator/partial"
	_ "github.com/ngicks/kama/kamalib/generator/textmod"
	_ "github.com/ngicks/kama/kamalib/generator/validator"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	_ "github.com/ngicks/kama/kamalib/proc/fileloader"
	_ "github.com/ngicks/kama/kamalib/proc/gopackages"
	_ "github.com/ngicks/kama/kamalib/proc/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Options configures Run.
type Options struct {
	// Spec is the path of the workflow spec file. Its extension gives the
	// format: .yaml, .yml, .json or .toml.
	Spec string
	// Registry holds the procs the spec may name. It is
	// workflow.DefaultRegistry when nil.
	Registry *workflow.Registry
	// Progress receives the warnings and the progress of the steps. A
	// warning about the spec file itself comes with an empty step. A nil
	// OnWarning drops the warnings, the stale warnings of the generators
	// among them; cli.Progress writes them out.
	Progress workflow.ProgressControl
}

// Run builds the graph of the spec file of opts and runs it. The steps
// work in the working directory of the process unless the spec tells them
// otherwise.
func Run(ctx context.Context, opts Options) error {
	spec, err := LoadSpec(opts.Spec)
	if err != nil {
		return err
	}

	reg := opts.Registry
	if reg == nil {
		reg = workflow.DefaultRegistry
	}

	g, err := spec.Build(ctx, reg)
	if err != nil {
		return fmt.Errorf("kamasimple: %s: %w", opts.Spec, err)
	}

	if opts.Progress.OnWarning != nil {
		for _, w := range g.Warnings() {
			opts.Progress.OnWarning("", fmt.Errorf("kamasimple: %s: %w", opts.Spec, w))
		}
	}

	return g.Run(ctx, opts.Progress)
}

// LoadSpec decodes the workflow spec file at name, in the format its
// extension gives.
//
// Every format is decoded into plain values first and then into the Spec
// with encoding/json/v2, as a step param is. A spec therefore reads the
// same in YAML, JSON and TOML: a list of one step name may be written as a
// scalar in each of them. The Spec captures the keys it does not know.
func LoadSpec(name string) (workflow.Spec, error) {
	dir, file := filepath.Split(name)
	if dir == "" {
		dir = "."
	}

	var raw any
	if err := fileloader.LoadFile(os.DirFS(dir), file, &raw); err != nil {
		return workflow.Spec{}, fmt.Errorf("kamasimple: %w", err)
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return workflow.Spec{}, fmt.Errorf("kamasimple: %s: %w", name, err)
	}

	var spec workflow.Spec
	if err := json.Unmarshal(b, &spec); err != nil {
		return workflow.Spec{}, fmt.Errorf("kamasimple: %s: %w", name, err)
	}

	return spec, nil
}
