package partial

import (
	"slices"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register(GeneratorId().ProcKind(), NewProc) }

// ProcParam configures the proc of NewProc. See Options for the modes. A
// spec decodes into ProcParamPartial, which the proc merges onto
// DefaultProcParam.
//
//kama:partial
type ProcParam struct {
	// MergeStrategy is "overwrite" or "append". The partial variant has a
	// Merge method, so the field cannot take that name.
	MergeStrategy string `json:"merge"`
	// MergeNestedStrategy is "recursive" or "overwrite".
	MergeNestedStrategy string `json:"merge_nested"`
	// OptionFile names the option file, its base path relative to the root
	// of the out of the proc and its formats, under the lowercase keys base
	// and formats. Empty formats mean toml, yaml, json. None means no
	// option file. A named file that is not present is an error.
	OptionFile opt.Option[fileloader.PathInput] `json:"option_file"`
	// FlagSet tells whether the proc writes the flag set file of the
	// packages it generates into. A package needs the file when one of its
	// targets has a flag option, in any build variant. Turning it off with
	// such a target loaded is an error.
	FlagSet bool `json:"flag_set"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial {
	return ProcParamPartial{
		MergeStrategy:       und.Defined(string(MergeOverwrite)),
		MergeNestedStrategy: und.Defined(string(MergeNestedRecursive)),
		FlagSet:             und.Defined(true),
	}
}

// UnknownKeys implements workflow.UnknownKeyer. It includes the unknown keys
// of option_file.
func (p *ProcParamPartial) UnknownKeys() [][]string {
	return slices.Concat(
		unknownkey.Paths(p.Unknown),
		unknownkey.Under(p.OptionFile.Value().UnknownKeys(), "option_file"),
	)
}

// NewProc returns the proc generating the partial types of the packages
// its in loaded, as base.SourceProc describes. A package option file is
// the file //kama:option-file partial=<path> names, or else
// kama_partial.toml, .yaml, .yml or .json in the package directory when
// present.
func NewProc() workflow.Proc { return proc }

var proc = base.SourceProc[
	ProcParamPartial, ProcParam, Options, PackageOptions, TypeOptions, FieldOptions,
]{
	Generator: Generator(),
	Default:   DefaultProcParam,
	Options:   procOptions,
	Loader:    NewLoader(nil),
	LoadError: loadError,
}

// procOptions returns the options of p and the option file it names.
func procOptions(p ProcParam) (Options, opt.Option[fileloader.PathInput], error) {
	opts := Options{
		Merge:       MergeMode(p.MergeStrategy),
		MergeNested: MergeNestedMode(p.MergeNestedStrategy),
		OmitFlagSet: !p.FlagSet,
	}
	if err := opts.Validate(); err != nil {
		return Options{}, opt.None[fileloader.PathInput](), err
	}

	return opts.WithDefaults(), p.OptionFile, nil
}
