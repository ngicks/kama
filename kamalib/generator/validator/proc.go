package validator

import (
	"slices"

	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register(GeneratorId().ProcKind(), NewProc) }

// ProcParam configures the proc of NewProc. A spec decodes into
// ProcParamPartial, which the proc merges onto DefaultProcParam.
//
//kama:partial
type ProcParam struct {
	// OptionFile names the option file, its base path relative to the root
	// of the out of the proc and its formats, under the lowercase keys base
	// and formats. Empty formats mean toml, yaml, json. None means no
	// option file. A named file that is not present is an error.
	OptionFile opt.Option[fileloader.PathInput] `json:"option_file"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial {
	return ProcParamPartial{}
}

// UnknownKeys implements workflow.UnknownKeyer. It includes the unknown keys
// of option_file.
func (p *ProcParamPartial) UnknownKeys() [][]string {
	return slices.Concat(
		unknownkey.Paths(p.Unknown),
		unknownkey.Under(p.OptionFile.Value().UnknownKeys(), "option_file"),
	)
}

// NewProc returns the proc generating the Validate methods of the packages
// its in loaded with the default registry, as base.SourceProc describes. It
// is the constructor of the kind validate.generate in
// workflow.DefaultRegistry. A package option file is the file
// //kama:option-file validate=<path> names, or else kama_validate.toml,
// .yaml, .yml or .json in the package directory when present.
func NewProc() workflow.Proc { return procWith(nil) }

// NewProcWith returns a constructor of the proc of NewProc generating with
// the validators of reg, for workflow.Registry.Register. It keeps a clone of
// reg, so later changes to reg do not reach the procs. The procs only read
// the clone, so the graphs built with them share nothing they change. A nil
// reg means ext.DefaultRegistry().
func NewProcWith(reg *ext.Registry) func() workflow.Proc {
	if reg != nil {
		reg = reg.Clone()
	}

	return func() workflow.Proc { return procWith(reg) }
}

func procWith(reg *ext.Registry) base.SourceProc[
	ProcParamPartial, ProcParam, Options, PackageOptions, TypeOptions, FieldOptions,
] {
	return base.SourceProc[
		ProcParamPartial, ProcParam, Options, PackageOptions, TypeOptions, FieldOptions,
	]{
		Generator: Generator(),
		Default:   DefaultProcParam,
		Options: func(p ProcParam) (Options, opt.Option[fileloader.PathInput], error) {
			return Options{Registry: reg}, p.OptionFile, nil
		},
		Loader:    NewLoader(nil),
		LoadError: loadError,
	}
}
