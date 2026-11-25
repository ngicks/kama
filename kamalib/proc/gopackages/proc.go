// Package gopackages provides the proc that loads Go packages from an
// overlayfs.Fs.
package gopackages

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register("packages.load", NewProc) }

// ErrPackageErrors is wrapped by the warning about the errors of a package
// a load with ProcParam.AllowTypeErrors keeps.
var ErrPackageErrors = errors.New("gopackages: package kept with errors")

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
	// AllowTypeErrors keeps the packages that do not type-check, as
	// gopackages.Options.AllowTypeErrors tells, and warns about the errors
	// of each package. A step feeding a generator that reads the syntax
	// trees only sets it. A generated file that does not compile yet or no
	// longer compiles then does not stop the generator.
	AllowTypeErrors bool `json:"allow_type_errors"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParam) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// Proc loads packages from the project as its in shows it, generated files
// included. With ProcParam.AllowTypeErrors, it warns, wrapping
// ErrPackageErrors, about the errors of each package it keeps, in package
// path order.
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

func (i *instance) Run(ctx context.Context, rep workflow.Reporter) error {
	pkgs, err := gopackages.Load(ctx, i.fs, gopackages.Options{
		Patterns:        i.param.Patterns,
		BuildFlags:      i.param.BuildFlags,
		Tests:           i.param.Tests,
		AllowTypeErrors: i.param.AllowTypeErrors,
	})
	if err != nil {
		return err
	}

	for path, errs := range opiter.MapsSorted(gopackages.Errors(pkgs)) {
		rep.Warn(fmt.Errorf("%w: %w", ErrPackageErrors, gopackages.PackageErrors{path: errs}))
	}

	i.out.Pkgs = pkgs

	return nil
}
