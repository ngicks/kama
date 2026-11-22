package validator

import (
	"cmp"
	"errors"
	"go/types"
	"maps"
	"slices"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// GeneratorName is the name the generator marks its declarations with.
const GeneratorName = "kama-validate"

// naming names the files the generator writes.
var naming = base.Naming{Name: "kama_validate"}

// HelpersFile is the name of the file declaring the helpers the generated
// files of a package call.
const HelpersFile = "zz_gen.helper.kama_validate.go"

// helperKind is the kind of the files declaring helpers, as their names
// spell it.
const helperKind = "helper"

// generatorPath is the import path of the generator, named by the first
// line of the header of every generated file.
const generatorPath = "github.com/ngicks/kama/kamalib/generator/validator"

// validationPath is the import path of the package of the errors generated
// code returns.
const validationPath = "github.com/ngicks/kama/kamalib/pkg/validation"

const (
	optPath     = "github.com/ngicks/option/opt"
	undPath     = "github.com/ngicks/option/und"
	elasticPath = "github.com/ngicks/option/elastic"
)

func init() { base.MustRegister(GeneratorName) }

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
	// with or shadow.
	ErrConflict = errors.New("validator: name conflict")
	// ErrOptionFile reports an option file that cannot be loaded or does
	// not decode. It wraps the error of genopt too.
	ErrOptionFile = errors.New("validator: invalid option file")
	// ErrArgs marks an error of Validator.Check that the args cause for
	// any type. A validator wraps it as fmt.Errorf("%w: ...", ErrArgs) does.
	// The generator reports such an error at once. It does not apply the
	// validator again to the value a pointer or an option holds. That
	// attempt would fail the same way.
	ErrArgs = errors.New("validator: invalid args")
	// ErrStale reports a file of a loaded package that carries the marker of
	// the generator but that the run did not produce. It is a warning.
	ErrStale = base.ErrStale
)

// Options configures Generate.
type Options struct {
	// Registry holds the validators directives name. It is
	// DefaultRegistry() when nil.
	Registry *Registry
	// Markers tells the declarations emitted by generators. It is
	// genmark.DefaultRegistry when nil.
	Markers *genmark.Registry
	// Loaded holds the options of the directives and the option files of
	// the packages Generate is given. The Loader of NewLoader loads them.
	// When Loaded is nil, Generate loads the directives itself with
	// LoadPackages and reads no option file.
	Loaded *LoadedOptions
	// Now gives the time the header of the generated files records. It is
	// time.Now when nil.
	Now func() time.Time
}

// WithDefaults returns o with empty fields set to their defaults.
func (o Options) WithDefaults() Options {
	if o.Registry == nil {
		o.Registry = DefaultRegistry()
	}

	if o.Markers == nil {
		o.Markers = genmark.DefaultRegistry
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	return o
}

// File is a generated file. Its Path is absolute, next to the source file
// the file derives from.
type File = base.File

// Generate returns the Validate methods of the targets in pkgs, one file per
// source file declaring targets, and the helpers files of each package
// holding targets, sorted by path.
//
// The warnings are those of Options.Loaded, followed by one wrapping
// ErrStale for each file of pkgs carrying the marker of the generator that
// is not among the returned files.
//
// pkgs must be loaded as gopackages.Load does. Test variants are skipped.
func Generate(pkgs []*packages.Package, opts Options) ([]File, []error, error) {
	opts = opts.WithDefaults()
	now := opts.Now()

	srcPkgs := base.SourcePackages(pkgs)

	if opts.Loaded == nil {
		loaded, err := NewLoader(nil).LoadPackages(srcPkgs)
		if err != nil {
			return nil, nil, loadError(err)
		}

		opts.Loaded = loaded
	}

	sel, err := selectTargets(srcPkgs, opts)
	if err != nil {
		return nil, nil, err
	}

	byPkg := map[string][]string{}
	pkgOf := map[string]*packages.Package{}

	for n := range sel.targets() {
		name := sel.filename(n)
		if !slices.Contains(byPkg[n.Id.PackagePath], name) {
			byPkg[n.Id.PackagePath] = append(byPkg[n.Id.PackagePath], name)
		}

		pkgOf[n.Id.PackagePath] = n.Detail.Pkg
	}

	var (
		files []File
		errs  []error
	)

	for _, pkgPath := range slices.Sorted(maps.Keys(byPkg)) {
		hdr, err := fileHeader(opts, pkgPath, now)
		if err != nil {
			return nil, nil, err
		}

		out, err := renderPackage(
			sel,
			hdr,
			pkgOf[pkgPath],
			slices.Sorted(slices.Values(byPkg[pkgPath])),
		)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		files = append(files, out...)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}

	slices.SortFunc(files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })

	stale := base.StaleCheck{Registry: opts.Markers, Generator: GeneratorName}

	return files, slices.Concat(opts.Loaded.Warnings, stale.Files(srcPkgs, files)), nil
}

// isNamed reports whether t refers to the type name of the package at
// pkgPath, through aliases.
func isNamed(t types.Type, pkgPath, name string) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()

	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == name
}

// typeArg returns the first type argument of the instantiated type t.
func typeArg(t types.Type) types.Type {
	return types.Unalias(t).(*types.Named).TypeArgs().At(0)
}
