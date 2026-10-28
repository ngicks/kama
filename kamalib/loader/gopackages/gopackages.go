// Package gopackages loads Go packages through golang.org/x/tools/go/packages
// with the target filesystem's staged files applied as an overlay.
package gopackages

import (
	"context"
	"errors"
	"reflect"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

// Mode is the load mode Proc uses: enough for type-checked syntax trees.
const Mode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedModule

// Packages is the result of a load.
type Packages struct {
	Pkgs []*packages.Package
}

// ProcParam configures Proc.
type ProcParam struct {
	// Patterns are package patterns. Empty means "./...".
	Patterns []string `json:"patterns"`
	// BuildFlags are passed to the build system, for example "-tags=foo".
	BuildFlags []string `json:"build_flags"`
	// Tests also loads test packages.
	Tests bool `json:"tests"`
}

// Proc loads packages from the project as its in shows it, generated files
// included.
//
//	Param(ProcParam) error
//	In(*overlayfs.Fs)
//	AsInput() *Packages
type Proc struct{}

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParam]() }

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc) OutTypes() []reflect.Type { return nil }

func (Proc) AsInputType() reflect.Type { return reflect.TypeFor[*Packages]() }

func (Proc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &instance{}, nil
}

type instance struct {
	param ProcParam
	fs    *overlayfs.Fs
	out   Packages
}

func (i *instance) Param(p ProcParam) error {
	if slices.Contains(p.Patterns, "") {
		return errors.New("gopackages: empty pattern")
	}

	if len(p.Patterns) == 0 {
		p.Patterns = []string{"./..."}
	}

	i.param = p
	return nil
}

func (i *instance) In(fs *overlayfs.Fs) { i.fs = fs }

func (i *instance) AsInput() *Packages { return &i.out }

func (i *instance) Run(ctx context.Context) error {
	overlay, err := i.fs.Overlay()
	if err != nil {
		return err
	}

	cfg := &packages.Config{
		Context:    ctx,
		Mode:       Mode,
		Dir:        i.fs.Root(),
		BuildFlags: i.param.BuildFlags,
		Tests:      i.param.Tests,
		Overlay:    overlay,
	}

	pkgs, err := packages.Load(cfg, i.param.Patterns...)
	if err != nil {
		return err
	}

	i.out.Pkgs = pkgs
	return nil
}
