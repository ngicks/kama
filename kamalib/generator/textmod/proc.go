package textmod

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

// ProcKind returns the kind of the proc in workflow.DefaultRegistry,
// textmod.replace. The proc replaces text in files, so its verb is not
// generate.
func ProcKind() string { return GeneratorId().ProcKindVerb("replace") }

func init() { workflow.Register(ProcKind(), NewProc) }

// ProcParam configures the proc.
type ProcParam struct {
	// Mods maps an id to the source of the text/template computing the new
	// value of the spec tagged with it.
	Mods map[string]string `json:"mods"`
	// Vars are the caller values a template reads as .Vars.
	Vars map[string]any `json:"vars"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// Default returns the values a ProcParam takes for the keys a spec leaves
// out: no mods and no vars.
func (ProcParam) Default() ProcParam { return ProcParam{} }

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParam) UnknownKeys() [][]string { return unknownkey.Paths(p.Unknown) }

// proc runs Generator on the packages its in loaded, reading and staging
// the files in its out. It has no stale check: the generator puts no
// generated mark on a file. It takes optional inputs of any type, which
// the templates read through the function input.
//
//	Param(base.ParamOf[ProcParam]) error
//	In(*gopackages.Packages)
//	Out(*overlayfs.Fs)
//	Optional(map[string]any)
var proc = base.Proc[base.ParamOf[ProcParam], ProcParam, *gopackages.Packages, Input]{
	Generator:     Generator(),
	Check:         checkParam,
	Input:         procInput,
	TakesOptional: true,
}

// NewProc returns the proc of the generator. It is the constructor of the
// kind textmod.replace in workflow.DefaultRegistry.
func NewProc() workflow.Proc { return proc }

// checkParam parses every mod with functions bound to an empty Run, so a
// dry run reports a template that does not parse. Parsing calls no
// function, so input passes before the optional inputs exist.
func checkParam(p ProcParam) error {
	funcs, err := Funcs(Run{})
	if err != nil {
		return err
	}

	var errs []error

	for _, id := range slices.Sorted(maps.Keys(p.Mods)) {
		if _, err := ParseMod(id, p.Mods[id], funcs); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func procInput(
	_ context.Context,
	p ProcParam,
	pkgs *gopackages.Packages,
	out *overlayfs.Fs,
) (Input, base.Scope, error) {
	return Input{
		Pkgs: base.SourcePackages(pkgs.Pkgs),
		Root: out.Root(),
		Mods: p.Mods,
		Vars: p.Vars,
	}, base.Scope{}, nil
}
