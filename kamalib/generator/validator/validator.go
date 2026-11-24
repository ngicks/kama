package validator

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/validator/rule"
)

// GeneratorId identifies the generator. Its name is the directive
// //kama:validate, and init registers it with base.MustRegister.
func GeneratorId() base.Id { return base.Id{Namespace: "kama", Name: "validate"} }

// naming names the files the generator writes.
var naming = GeneratorId().Naming()

// HelpersFile is the name of the file declaring the helpers the generated
// files of a package call. An external test package takes the name with
// the _test suffix, as base.PackageFile gives it.
const HelpersFile = "zz_gen.helper.kama_validate.go"

// helperKind is the kind of the files declaring helpers, as their names
// spell it.
const helperKind = "helper"

func init() { base.MustRegister(GeneratorId()) }

var (
	// ErrDirective reports a //kama:validate directive or an option file
	// entry with unknown args, an unknown validator, or one in a place it
	// cannot apply to.
	ErrDirective = errors.New("validator: invalid directive")
	// ErrUnsupported reports a type the generator cannot give a Validate, a
	// validator applied to a type it cannot handle, and an implementor whose
	// Validate generated code cannot call.
	ErrUnsupported = errors.New("validator: unsupported type")
	// ErrHook reports a field hook a Validate would call that is missing or
	// has the wrong signature.
	ErrHook = errors.New("validator: invalid hook")
	// ErrConflict reports a name the generated declarations would collide
	// with or shadow, and a validator registered under a word of
	// //kama:validate that names no validator.
	ErrConflict = errors.New("validator: name conflict")
	// ErrOptionFile reports an option file that cannot be loaded or does
	// not decode. It wraps the error of genopt too.
	ErrOptionFile = errors.New("validator: invalid option file")
	// ErrStale reports a file of a loaded package that carries the marker of
	// the generator but that the run did not produce. It is a warning.
	ErrStale = base.ErrStale
)

// Options configures Generate.
type Options struct {
	// Registry holds the validators directives name. It is
	// rule.DefaultRegistry() when nil.
	Registry *rule.Registry
}

// WithDefaults returns o with empty fields set to their defaults.
func (o Options) WithDefaults() Options {
	if o.Registry == nil {
		o.Registry = rule.DefaultRegistry()
	}

	return o
}

// Generator generates the Validate methods of the targets of the packages
// of its Input.
func Generator() base.Generator[Input] {
	return base.Generator[Input]{Id: GeneratorId(), Generate: Generate}
}

// Input is the input of a run of Generator.
type Input = base.Input[Options, PackageOptions, TypeOptions, FieldOptions]

// Generate yields the Validate methods of the targets in the packages of
// in, one file per source file declaring targets, and the helpers files of
// each package holding targets.
//
// An error in the files of one package does not stop the files of the
// other packages.
func Generate(_ context.Context, env base.Env, in Input) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		opts := in.Options.WithDefaults()

		if err := reservedNames(opts.Registry); err != nil {
			yield(nil, err)
			return
		}

		sel, err := selectTargets(in.Packages, opts, env.Registry, in.Loaded)
		if err != nil {
			yield(nil, err)
			return
		}

		byPkg := map[string][]string{}
		pkgOf := map[string]*packages.Package{}

		for n := range sel.targets() {
			name := n.Filename()
			if !slices.Contains(byPkg[n.Id.PackagePath], name) {
				byPkg[n.Id.PackagePath] = append(byPkg[n.Id.PackagePath], name)
			}

			pkgOf[n.Id.PackagePath] = n.Detail.Pkg
		}

		for _, pkgPath := range slices.Sorted(maps.Keys(byPkg)) {
			files, err := renderPackage(
				sel,
				pkgOf[pkgPath],
				slices.Sorted(slices.Values(byPkg[pkgPath])),
			)
			if err != nil {
				if !yield(nil, err) {
					return
				}

				continue
			}

			for _, c := range files {
				rel, err := in.Rel(c.Path)
				if err != nil {
					if !yield(nil, fmt.Errorf("validator: %w", err)) {
						return
					}

					continue
				}

				c.Path, c.Params = rel, in.Loaded.HeaderParams(pkgPath)
				if !yield(c, nil) {
					return
				}
			}
		}
	}
}

// reservedNames reports the validators of reg named by a word of
// //kama:validate that names no validator.
func reservedNames(reg *rule.Registry) error {
	reserved := opiter.WrapSeq(slices.Values(reg.Names())).
		Filter(func(name string) bool {
			return slices.Contains(
				[]string{hookWord, ignoreWord, decodedWord, fileWord, packageWord},
				name,
			)
		}).
		CollectSlice()

	if len(reserved) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: validators registered as %s; hook, ignore, decoded, file and package name"+
			" no validator",
		ErrConflict, strings.Join(reserved, ", "),
	)
}
