package fieldtree

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// ProcKind returns the kind of the proc in workflow.DefaultRegistry,
// fieldtree.text. The proc computes text and writes no file, so its verb
// is not generate.
func ProcKind() string { return GeneratorId().ProcKindVerb("text") }

func init() { workflow.Register(ProcKind(), NewProc) }

// ProcParam configures the proc.
type ProcParam struct {
	// Types maps an id to a target named by type, for a type that carries
	// no directive.
	Types   map[string]Target `json:"types"`
	Unknown map[string]any    `json:",embed"`
}

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParam) UnknownKeys() [][]string {
	paths := unknownkey.Paths(p.Unknown)
	for _, id := range slices.Sorted(maps.Keys(p.Types)) {
		paths = append(paths, unknownkey.Paths(p.Types[id].Unknown, "types", id)...)
	}

	return paths
}

// Proc draws the trees of the targets of the packages its in loaded. It
// writes no file. Its AsInput value is the Texts of the run, which a later
// step names in its in or optional list, as textmod.replace does to put a
// tree into a help text. It reports the warnings of Generate.
//
//	Param(ProcParam) error
//	In(*gopackages.Packages)
//	Out()
//	AsInput() Texts
type Proc struct{}

// NewProc returns the proc. It is the constructor of the kind
// fieldtree.text in workflow.DefaultRegistry.
func NewProc() workflow.Proc { return Proc{} }

func (Proc) ParamType() reflect.Type { return reflect.TypeFor[ProcParam]() }

func (Proc) InTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*gopackages.Packages]()}
}

func (Proc) OutTypes() []reflect.Type { return nil }

func (Proc) AsInputType() reflect.Type { return reflect.TypeFor[Texts]() }

func (Proc) Instantiate(context.Context, bool) (workflow.Instance, error) {
	return &instance{}, nil
}

func (Proc) CleanUp(context.Context) error { return nil }

type instance struct {
	param ProcParam
	pkgs  *gopackages.Packages
	out   Texts
}

func (i *instance) Param(p ProcParam) error {
	var errs []error

	for _, id := range slices.Sorted(maps.Keys(p.Types)) {
		if _, _, ok := splitTypeKey(p.Types[id].Type); !ok {
			errs = append(errs, fmt.Errorf(
				"%w: types.%s: %q is no <import path>.<type name>", ErrTarget, id, p.Types[id].Type,
			))
		}
	}

	i.param = p

	return errors.Join(errs...)
}

func (i *instance) In(pkgs *gopackages.Packages) { i.pkgs = pkgs }

func (i *instance) Out() {}

func (i *instance) AsInput() Texts { return i.out }

func (i *instance) Run(_ context.Context, rep workflow.Reporter) error {
	out, err := Generate(Input{
		Pkgs:     base.SourcePackages(i.pkgs.Pkgs),
		Types:    i.param.Types,
		Registry: genext.DefaultRegistry(),
	}, rep.Warn)

	i.out = out

	return err
}
