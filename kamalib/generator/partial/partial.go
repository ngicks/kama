package partial

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"iter"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/base/filescope"

	"github.com/ngicks/opiter"

	"github.com/dave/dst"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/overlayfs/suffix"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// GeneratorId identifies the generator. Its name is the directive
// //kama:partial, and init registers it with base.MustRegister.
func GeneratorId() base.Id { return base.Id{Namespace: "kama", Name: "partial"} }

// FileSuffix is inserted into the name of a source file to name the file
// generated from it.
const FileSuffix = ".kama_partial"

// naming names the files the generator writes.
var naming = GeneratorId().Naming()

// HelpersFile is the name of the file declaring the helpers the partial
// files of a package call. The zz_gen. prefix marks a generated file no
// source file stands behind and sorts it after the files written by hand.
// An external test package takes the name with the _test suffix, as
// base.PackageFile gives it.
const HelpersFile = "zz_gen.helper.kama_partial.go"

// helperKind is the kind of the files declaring helpers, as their names
// spell it.
const helperKind = "helper"

// isHelpersFile reports whether base names a helpers file.
func isHelpersFile(base string) bool { return naming.IsShared(helperKind, base) }

func init() { base.MustRegister(GeneratorId()) }

var (
	// ErrDirective reports a //kama:partial directive with unknown args or
	// in a place it cannot apply to.
	ErrDirective = errors.New("partial: invalid directive")
	// ErrUnsupported reports a type the generator cannot give a partial
	// variant, or a field type it cannot map.
	ErrUnsupported = errors.New("partial: unsupported type")
	// ErrConflict reports a name the generated declarations would collide
	// with or shadow: a partial type, a method of one, a helper or the
	// flag set interface declared by something other than the generator, a
	// declaration the generator did not write in the flag set file or in a
	// helpers file, two
	// partial fields with one name or a partial field named like a method,
	// more helpers sharing one hashed name than suffixes tell apart, a type
	// parameter named like a predeclared identifier or like a name the
	// declarations refer to, and a package-level declaration named like a
	// predeclared identifier the generated code spells.
	ErrConflict = errors.New("partial: name conflict")
	// ErrOption reports an invalid Options value.
	ErrOption = errors.New("partial: invalid option")
	// ErrOptionFile reports an option file that cannot be loaded or does
	// not decode, an option file whose entries name packages, types or
	// fields that are not loaded, and an invalid //kama:option-file
	// directive. It wraps the error of genopt too.
	ErrOptionFile = errors.New("partial: invalid option file")
	// ErrStale reports a file of a loaded package that carries the marker of
	// the generator but that the run did not produce. It is a warning.
	ErrStale = base.ErrStale
)

// MergeMode selects how a defined slice or map field of a partial combines
// with the value it is merged onto.
type MergeMode string

const (
	// MergeOverwrite replaces the value.
	MergeOverwrite MergeMode = "overwrite"
	// MergeAppend appends slice elements and sets map entries.
	MergeAppend MergeMode = "append"
)

// MergeNestedMode selects how a defined field holding a nested partial
// combines with the value it is merged onto.
type MergeNestedMode string

const (
	// MergeNestedRecursive merges the nested partial field by field.
	MergeNestedRecursive MergeNestedMode = "recursive"
	// MergeNestedOverwrite replaces the nested value.
	MergeNestedOverwrite MergeNestedMode = "overwrite"
)

// Options configures Generate.
//
// The merge modes are the defaults of the Merge methods, which a field
// overrides with //kama:partial:merge or its option file entry. They do
// not affect the type declarations.
type Options struct {
	// Merge is the strategy of slice and map fields and of the Merge
	// methods of slice and map partial types. It is MergeOverwrite when
	// empty.
	Merge MergeMode
	// MergeNested is the strategy of fields holding partial values. It is
	// MergeNestedRecursive when empty.
	MergeNested MergeNestedMode
	// OmitFlagSet leaves out the flag set file of the packages Generate
	// writes into. A target with a flag option is an error then: its
	// methods take the flag set the file declares.
	OmitFlagSet bool
}

// WithDefaults returns o with empty fields set to their defaults.
func (o Options) WithDefaults() Options {
	o.Merge = cmp.Or(o.Merge, MergeOverwrite)
	o.MergeNested = cmp.Or(o.MergeNested, MergeNestedRecursive)

	return o
}

// Validate reports an unknown mode. Empty modes are valid.
func (o Options) Validate() error {
	switch o.Merge {
	case "", MergeOverwrite, MergeAppend:
	default:
		return fmt.Errorf("%w: merge %q", ErrOption, o.Merge)
	}

	switch o.MergeNested {
	case "", MergeNestedRecursive, MergeNestedOverwrite:
	default:
		return fmt.Errorf("%w: merge_nested %q", ErrOption, o.MergeNested)
	}

	return nil
}

// Generator generates the partial types of the targets of the packages of
// its Input.
func Generator() base.Generator[Input] {
	return base.Generator[Input]{Id: GeneratorId(), Generate: Generate}
}

// Input is the input of a run of Generator.
type Input = base.Input[Options, PackageOptions, TypeOptions, FieldOptions]

// Generate yields the partial type declarations and their methods for the
// targets in the packages of in, one file per source file declaring
// targets, and the helpers files and the flag set file of each package
// holding targets. See the package documentation for target selection, the
// type mapping, the methods and their helpers.
//
// A package without targets in the build variant loaded gets its flag set
// file as well when its directory in env.Out holds a partial file of the
// package: the file has no build constraint and serves the partial files
// of every variant.
//
// Generate reports the errors of every source file of a package and goes
// on with the next package.
func Generate(_ context.Context, env base.Env, in Input) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		if err := in.Options.Validate(); err != nil {
			yield(nil, err)
			return
		}

		opts := in.Options.WithDefaults()

		sel, err := selectTargets(in.Packages, opts, env.Registry, in.Loaded)
		if err != nil {
			yield(nil, err)
			return
		}

		targets := slices.Collect(maps.Values(sel.targets()))

		// The source files declaring targets, sorted.
		filenames := slices.Compact(
			opiter.SlicesValues(targets).
				Map((*typegraph.Node).Filename).
				Collect(slices.Sorted),
		)

		type pkgFiles struct {
			pkg   *packages.Package
			dir   string
			files []*partialFile
		}

		byPkg := map[string]*pkgFiles{}

		for _, filename := range filenames {
			pkg := typegraph.NodesIn(slices.Values(targets), filename)[0].Detail.Pkg

			pf := byPkg[pkg.PkgPath]
			if pf == nil {
				pf = &pkgFiles{pkg: pkg, dir: filepath.Dir(filename)}
				byPkg[pkg.PkgPath] = pf
			}

			pf.files = append(pf.files, &partialFile{src: filename})
		}

		emit := func(params map[string]any, files []base.Composed, err error) bool {
			if err != nil {
				return yield(nil, err)
			}

			for _, c := range files {
				rel, err := in.Rel(c.Path)
				if err != nil {
					if !yield(nil, fmt.Errorf("partial: %w", err)) {
						return false
					}

					continue
				}

				c.Path, c.Params = rel, params
				if !yield(c, nil) {
					return false
				}
			}

			return true
		}

		for _, pkgPath := range slices.Sorted(maps.Keys(byPkg)) {
			pf := byPkg[pkgPath]
			slices.SortFunc(
				pf.files,
				func(a, b *partialFile) int { return cmp.Compare(a.path(), b.path()) },
			)

			files, err := renderPackageFiles(sel, pf.pkg, pf.dir, pf.files, targets)
			if !emit(headerParams(opts, in.Loaded, pkgPath), files, err) {
				return
			}
		}

		if opts.OmitFlagSet {
			return
		}

		for _, pkg := range in.Packages {
			if byPkg[pkg.PkgPath] != nil {
				continue
			}

			files, err := keptFlagSet(env.Out, in, pkg)
			if !emit(headerParams(opts, in.Loaded, pkg.PkgPath), files, err) {
				return
			}
		}
	}
}

// renderPackageFiles returns the files of pkg holding targets: its partial
// files and helpers files, as renderPackage gives them, and its flag set
// file unless the options of sel leave it out.
func renderPackageFiles(
	sel *selection,
	pkg *packages.Package,
	dir string,
	files []*partialFile,
	targets []*typegraph.Node,
) ([]base.Composed, error) {
	out, err := renderPackage(sel, pkg, dir, files, targets)
	if err != nil {
		return nil, err
	}

	if sel.opts.OmitFlagSet {
		if err := sel.checkNoFlags(pkg.PkgPath); err != nil {
			return nil, err
		}
	} else {
		// The flag set holds every method any flag kind calls: a partial file
		// of a build variant other than the one loaded keeps calling the
		// methods its flags use.
		c, err := renderFlagSet(pkg, dir)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	if err := base.CheckPredeclared(pkg, out, ErrConflict); err != nil {
		return nil, err
	}

	return out, nil
}

// keptFlagSet returns the flag set file of pkg, a package the run generates
// no file for, when a partial file of pkg other than its flag set file lies
// in its directory in out, and no file otherwise. Such a partial file is of
// a build variant the run did not load, and calls the flag set file.
func keptFlagSet(out fs.FS, in Input, pkg *packages.Package) ([]base.Composed, error) {
	if len(pkg.Syntax) == 0 {
		return nil, nil
	}

	dir := filepath.Dir(pkg.Fset.File(pkg.Syntax[0].Pos()).Name())

	rel, err := in.Rel(dir)
	if err != nil {
		return nil, fmt.Errorf("partial: %w", err)
	}

	entries, err := fs.ReadDir(out, rel)
	if err != nil {
		return nil, fmt.Errorf("partial: read %q: %w", rel, err)
	}

	flagSet := base.PackageFile(pkg, FlagSetFile)

	served := false

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == flagSet || !suffix.IsSuffixed(name, FileSuffix) {
			continue
		}

		src, err := fs.ReadFile(out, path.Join(rel, name))
		if err != nil {
			return nil, fmt.Errorf("partial: read %q: %w", path.Join(rel, name), err)
		}

		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.PackageClauseOnly)
		if err == nil && f.Name.Name == pkg.Name {
			served = true
			break
		}
	}

	if !served {
		return nil, nil
	}

	c, err := renderFlagSet(pkg, dir)
	if err != nil {
		return nil, err
	}

	files := []base.Composed{c}
	if err := base.CheckPredeclared(pkg, files, ErrConflict); err != nil {
		return nil, err
	}

	return files, nil
}

// renderPackage prints the partial files of pkg, sorted by path, and the
// helpers files of dir.
//
// A helper is declared in one file of the package, which the files calling
// it must agree on before any is printed. A first pass therefore collects
// the helpers each file calls and the packages of supportPaths it spells,
// with every one of them required. The second pass prints each file
// requiring only those it spells: a required import is added whether the
// file uses it or not. targets are the targets of the run. The paths of the
// files are absolute.
func renderPackage(
	sel *selection,
	pkg *packages.Package,
	dir string,
	files []*partialFile,
	targets []*typegraph.Node,
) ([]base.Composed, error) {
	helpers := newHelperSet()

	var errs []error

	for _, f := range files {
		nodes := typegraph.NodesIn(slices.Values(targets), f.src)

		_, p, err := renderFileWith(sel, pkg, f, nodes, supportPaths, helpers)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		f.calls, f.support = p.calls, p.support
	}

	// The second pass needs the helpers of every file settled.
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	if err := helpers.resolve(); err != nil {
		return nil, err
	}

	for _, f := range files {
		f.used = map[string]bool{}
		for h := range f.calls {
			f.used[helpers.name(h)] = true
		}
	}

	if err := checkHelperNames(sel, pkg, files); err != nil {
		return nil, err
	}

	out := make([]base.Composed, 0, len(files)+1)
	for _, f := range files {
		nodes := typegraph.NodesIn(slices.Values(targets), f.src)

		c, _, err := renderFileWith(
			sel, pkg, f, nodes, slices.Sorted(maps.Keys(f.support)), helpers,
		)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}
	// A helpers file declares its helpers sorted by name, that is by op and
	// then by the hashes of their signatures.
	callers := opiter.SlicesValues(files).
		Map(func(f *partialFile) base.Caller { return f.caller(targets) }).
		CollectSlice()

	shared := filepath.Join(dir, base.PackageFile(pkg, HelpersFile))
	for _, hf := range base.HostShared(naming, helperKind, shared, callers) {
		c, err := renderHelpers(sel, pkg, hf, helpers)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, nil
}

// renderHelpers returns hf, declaring the helpers it hosts of helpers.
func renderHelpers(
	sel *selection,
	pkg *packages.Package,
	hf base.Host,
	helpers *helperSet,
) (base.Composed, error) {
	reserve := slices.Clone(localNames)

	require := map[string]bool{}

	for _, name := range hf.Names {
		h := helpers.byName[name]

		reserve = opiter.SlicesValues(h.typeParams()).
			Map(typeParamName).
			AppendSeq(reserve)

		maps.Copy(require, h.spelled)
	}

	scope, err := filescope.New(pkg, reserve, slices.Sorted(maps.Keys(require)))
	if err != nil {
		return base.Composed{}, err
	}

	p := &mapper{
		sel: sel, Scope: scope,
		helpers: helpers, calls: map[*helper]bool{}, support: map[string]bool{},
		refs: map[string]bool{},
	}

	var src strings.Builder

	for _, name := range hf.Names {
		text, err := p.helperDecl(name, helpers.byName[name])
		if err != nil {
			return base.Composed{}, err
		}

		src.WriteString(text)
	}

	parts, err := filescope.ParseParts([]byte(src.String()))
	if err != nil {
		return base.Composed{}, fmt.Errorf("partial: helpers of %s: %w", hf.Path, err)
	}

	return base.Composed{
		Path:            hf.Path,
		BuildConstraint: hf.BuildLine,
		Package:         pkg.Name,
		Imports:         scope.Imports,
		Parts:           parts,
	}, nil
}

// checkHelperNames reports a package-level declaration of pkg named like a
// helper files call that the generator did not write.
func checkHelperNames(sel *selection, pkg *packages.Package, files []*partialFile) error {
	names := map[string]bool{}
	for _, f := range files {
		maps.Copy(names, f.used)
	}

	declaredErr := func(obj types.Object) error {
		return base.ExplainConflict(
			fmt.Errorf(
				"%w: %s: %s.%s is already declared for a helper of the generated methods",
				ErrConflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, obj.Name(),
			),
			sel.reg,
			base.DeclDocs(pkg, obj)...,
		)
	}

	return errors.Join(
		opiter.SlicesValues(
			base.UnownedDecls(pkg, sel.reg, slices.Sorted(maps.Keys(names))),
		).
			Map(declaredErr).
			CollectSlice()...,
	)
}

// headerParams returns the params the header of every generated file of the
// package at pkgPath records.
func headerParams(opts Options, loaded *LoadedOptions, pkgPath string) map[string]any {
	params := loaded.HeaderParams(pkgPath)
	params["merge"] = string(opts.Merge)
	params["merge_nested"] = string(opts.MergeNested)

	return params
}

// renderFileWith returns the partial file of f, declaring the partial
// declarations of the targets nodes it declares, with the packages of
// require imported.
func renderFileWith(
	sel *selection,
	pkg *packages.Package,
	f *partialFile,
	nodes []*typegraph.Node,
	require []string,
	helpers *helperSet,
) (base.Composed, *mapper, error) {
	// The identifiers the generated declarations declare themselves are
	// reserved: the type parameters and the locals, which an import of the
	// same name would shadow or be shadowed by. A local renamed off its
	// base name reserves the new name itself, through the link of its
	// scope to the imports.
	reserve := slices.Clone(localNames)

	for _, n := range nodes {
		// Only files with field mask methods reserve their locals, so that the
		// import names of other files stay as they were.
		if sel.fieldMasked(n.Id) {
			reserve = append(reserve, fieldMaskLocals...)
		}

		tparams := n.Detail.Type.TypeParams()

		reserve = opiter.WrapSeq(tparams.TypeParams()).
			Map(typeParamName).
			AppendSeq(reserve)

		reserve = append(
			reserve,
			base.MethodTypeParams(pkg.Types.Scope(), n.Detail.Type.TypeParams(), localNames)...)
	}

	scope, err := filescope.New(pkg, reserve, require)
	if err != nil {
		return base.Composed{}, nil, err
	}

	p := &mapper{
		sel: sel, Scope: scope,
		helpers: helpers, calls: map[*helper]bool{}, support: map[string]bool{},
	}

	var parts []base.Part

	for _, n := range nodes {
		d, err := p.decl(n)
		if err != nil {
			return base.Composed{}, nil, err
		}

		printed, err := filescope.Parts(d)
		if err != nil {
			return base.Composed{}, nil, fmt.Errorf("partial: print %s: %w", n.Id.String(), err)
		}

		parts = append(parts, printed...)
	}

	// The partial types refer to the source types, so they must build under
	// the same constraint. A constraint in the file name is kept by the
	// output name.
	line, _ := base.BuildConstraint(nodes[0].Detail.File)

	return base.Composed{
		Path:            f.path(),
		BuildConstraint: line,
		Package:         pkg.Name,
		Imports:         scope.Imports,
		Parts:           parts,
	}, p, nil
}

// decl returns the partial declaration of n and its methods.
func (p *mapper) decl(n *typegraph.Node) ([]dst.Decl, error) {
	named := n.Detail.Type
	p.refs = map[string]bool{}

	tparams, err := p.typeParams(n, named.TypeParams())
	if err != nil {
		return nil, err
	}

	w := p.newMethodWriter(n)

	var ty dst.Expr

	switch u := named.Underlying().(type) {
	case *types.Struct:
		ty, err = p.structType(n, u)
		if err == nil {
			err = w.structMethods(u)
		}
	case *types.Slice:
		var elem dst.Expr

		elem, err = p.partialExpr(p.matchValue(u.Elem()))

		ty = &dst.ArrayType{Elt: elem}

		if err == nil {
			err = w.collectionMethods(false, u.Elem())
		}
	case *types.Map:
		var key, value dst.Expr

		key, err = p.TypeExpr(u.Key())
		if err == nil {
			value, err = p.partialExpr(p.matchValue(u.Elem()))
		}

		ty = &dst.MapType{Key: key, Value: value}

		if err == nil {
			err = w.collectionMethods(true, u.Elem())
		}
	}

	if err == nil {
		err = p.checkTypeParams(named)
	}

	if err != nil {
		return nil, fmt.Errorf("%s (%s): %w", n.Id.String(), p.Position(named.Obj().Pos()), err)
	}

	typeDecl := &dst.GenDecl{
		Tok: token.TYPE,
		Specs: []dst.Spec{&dst.TypeSpec{
			Name:       dst.NewIdent(partialName(named.Obj().Name())),
			TypeParams: tparams,
			Type:       ty,
		}},
	}
	typeDecl.Decs.Before = dst.EmptyLine
	typeDecl.Decs.Start = dst.Decorations{
		fmt.Sprintf(
			"// %s is the partial variant of %s.",
			partialName(named.Obj().Name()),
			named.Obj().Name(),
		),
	}

	methods, err := w.decls()
	if err != nil {
		return nil, err
	}

	return slices.Concat([]dst.Decl{typeDecl}, methods), nil
}

// checkTypeParams reports a type parameter of named sharing its name with
// a predeclared identifier or with a package-level identifier the
// generated declarations refer to, which the parameter would shadow.
// Import names avoid type parameters instead.
func (p *mapper) checkTypeParams(named *types.Named) error {
	refs := maps.Clone(p.refs)
	refs[named.Obj().Name()] = true
	refs[partialName(named.Obj().Name())] = true
	shadowErr := func(tp *types.TypeParam) error {
		switch {
		// The methods spell predeclared identifiers such as error, nil and
		// false, which a type parameter of that name would denote instead.
		case types.Universe.Lookup(tp.Obj().Name()) != nil:
			return fmt.Errorf(
				"%w: %s: type parameter %s of %s shadows the predeclared %s the generated code spells",
				ErrConflict,
				p.Position(tp.Obj().Pos()),
				tp.Obj().Name(),
				named.Obj().Name(),
				tp.Obj().Name(),
			)
		case refs[tp.Obj().Name()]:
			return fmt.Errorf(
				"%w: %s: type parameter %s shadows %s the partial declarations refer to",
				ErrConflict, p.Position(tp.Obj().Pos()), tp.Obj().Name(), tp.Obj().Name(),
			)
		}

		return nil
	}

	// errors.Join drops the nil errors of type parameters shadowing nothing.
	return errors.Join(
		opiter.WrapSeq(named.TypeParams().TypeParams()).
			Map(shadowErr).
			CollectSlice()...,
	)
}

func partialName(typeName string) string { return typeName + "Partial" }
