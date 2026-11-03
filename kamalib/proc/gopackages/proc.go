// Package gopackages provides the proc that loads Go packages from an
// overlayfs.Fs.
package gopackages

import (
	"context"
	"errors"
	"reflect"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("packages.load", NewProc) }

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
//	Out()
//	AsInput() *Packages
type Proc struct{}

// NewProc returns a new Proc.
func NewProc() workflow.Proc { return Proc{} }

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParam]() }

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*overlayfs.Fs]()}
}

func (Proc) OutTypes() []reflect.Type { return nil }

func (Proc) AsInputType() reflect.Type { return reflect.TypeFor[*Packages]() }

func (Proc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &instance{}, nil
}

func (Proc) CleanUp(context.Context) error { return nil }

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

func (i *instance) Out() {}

func (i *instance) AsInput() *Packages { return &i.out }

func (i *instance) Run(ctx context.Context, _ workflow.Reporter) error {
	pkgs, err := gopackages.Load(ctx, i.fs, gopackages.Options{
		Patterns:   i.param.Patterns,
		BuildFlags: i.param.BuildFlags,
		Tests:      i.param.Tests,
	})
	if err != nil {
		return err
	}

	i.out.Pkgs = pkgs
	return nil
}
