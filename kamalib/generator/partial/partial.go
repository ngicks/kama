package partial

import (
	"cmp"
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ngicks/kama/kamalib/generator/utils/base"

	"github.com/ngicks/opiter"

	"github.com/dave/dst"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// GeneratorName is the name the generator marks its declarations with.
const GeneratorName = "kama-partial"

// FileSuffix is inserted into the name of a source file to name the file
// generated from it.
const FileSuffix = ".kama_partial"

// naming names the files the generator writes.
var naming = base.Naming{Name: "kama_partial"}

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

// generatorPath is the import path of the generator, named by the first
// line of the header of every generated file.
const generatorPath = "github.com/ngicks/kama/kamalib/generator/partial"

func init() { base.MustRegister(GeneratorName) }

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
	// Registry tells the declarations emitted by generators. It is
	// base.DefaultRegistry when nil.
	Registry *genmark.Registry
	// Loaded holds the options of the directives and the option files of
	// the packages Generate is given. The Loader of NewLoader loads them.
	// When Loaded is nil, Generate loads the directives itself with
	// LoadPackages and reads no option file.
	Loaded *LoadedOptions
	// OmitFlagSet leaves out the flag set file of the packages Generate
	// writes into. A target with a flag option is an error then: its
	// methods take the flag set the file declares.
	OmitFlagSet bool
	// Now gives the time the header of the generated files records. It is
	// time.Now when nil.
	Now func() time.Time
}

// WithDefaults returns o with empty fields set to their defaults.
func (o Options) WithDefaults() Options {
	o.Merge = cmp.Or(o.Merge, MergeOverwrite)

	o.MergeNested = cmp.Or(o.MergeNested, MergeNestedRecursive)
	if o.Registry == nil {
		o.Registry = base.DefaultRegistry
	}

	if o.Now == nil {
		o.Now = time.Now
	}

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

// File is a generated file. Its Path is absolute, next to the source file
// the file derives from.
type File = base.File

// Generate returns the partial type declarations and their methods for
// the targets in pkgs, one file per source file declaring targets, and the
// helpers files and the flag set file of each package holding targets,
// sorted by path. See the
// package documentation for target selection, the type mapping, the
// methods and their helpers.
//
// The warnings are those of Options.Loaded, followed by one wrapping
// ErrStale for each file of pkgs carrying the marker of the generator that
// is not among the returned files, sorted by path. The flag set file of a
// package the run produces no files for is not reported.
//
// pkgs must be loaded as gopackages.Load does. base.SourcePackages picks
// the packages read, test variants and external test packages included.
// The returned content is not formatted beyond what printing the syntax
// tree gives.
func Generate(pkgs []*packages.Package, opts Options) ([]File, []error, error) {
	if err := opts.Validate(); err != nil {
		return nil, nil, err
	}

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

	targets := slices.Collect(maps.Values(sel.targets()))

	// The source files declaring targets, sorted.
	filenames := slices.Compact(
		opiter.WrapSeq(slices.Values(targets)).
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

	var files []File

	for _, pkgPath := range slices.Sorted(maps.Keys(byPkg)) {
		pf := byPkg[pkgPath]
		slices.SortFunc(
			pf.files,
			func(a, b *partialFile) int { return cmp.Compare(a.path(), b.path()) },
		)

		hdr, err := fileHeader(opts, pkgPath, now)
		if err != nil {
			return nil, nil, err
		}

		out, err := renderPackage(sel, hdr, pf.pkg, pf.dir, pf.files, targets)
		if err != nil {
			return nil, nil, err
		}

		if opts.OmitFlagSet {
			if err := sel.checkNoFlags(pkgPath); err != nil {
				return nil, nil, err
			}
		} else {
			// The flag set holds every method any flag kind calls: a partial
			// file of a build variant other than the one loaded keeps calling the
			// methods its flags use.
			content, err := renderFlagSet(pf.pkg, hdr)
			if err != nil {
				return nil, nil, err
			}

			out = append(
				out,
				File{
					Path:    filepath.Join(pf.dir, base.PackageFile(pf.pkg, FlagSetFile)),
					Content: content,
				},
			)
		}

		if err := base.CheckPredeclared(pf.pkg, out, ErrConflict); err != nil {
			return nil, nil, err
		}

		files = append(files, out...)
	}

	slices.SortFunc(files, func(a, b File) int { return cmp.Compare(a.Path, b.Path) })

	generated := map[string]bool{}
	for pkgPath := range byPkg {
		generated[pkgPath] = true
	}

	stale := base.StaleCheck{
		Registry:  opts.Registry,
		Generator: GeneratorName,
		// The flag set file has no build constraint and serves the partial
		// files of every build variant. A package the run produced nothing
		// for may still declare targets in files of a variant not loaded,
		// which keep needing it. A package the run produced files for
		// gets it, so it cannot be stale there, unless OmitFlagSet leaves it
		// out: a file an earlier run wrote is stale then.
		Keep: func(pkg *packages.Package, path string) bool {
			return !generated[pkg.PkgPath] &&
				filepath.Base(path) == base.PackageFile(pkg, FlagSetFile)
		},
	}

	return files, slices.Concat(opts.Loaded.Warnings, stale.Files(srcPkgs, files)), nil
}

// renderPackage prints the partial files of pkg, sorted by path, and the
// helpers files of dir.
//
// A helper is declared in one file of the package, which the files calling
// it must agree on before any is printed. A first pass therefore collects
// the helpers each file calls and the packages of supportPaths it spells,
// with every one of them required. The second pass prints each file
// requiring only those it spells: a required import is added whether the
// file uses it or not. targets are the targets of the run.
func renderPackage(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	dir string,
	files []*partialFile,
	targets []*typegraph.Node,
) ([]File, error) {
	helpers := newHelperSet()

	for _, f := range files {
		nodes := typegraph.NodesIn(slices.Values(targets), f.src)

		_, p, err := renderFileWith(sel, hdr, pkg, f, nodes, supportPaths, helpers)
		if err != nil {
			return nil, err
		}

		f.calls, f.support = p.calls, p.support
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

	out := make([]File, 0, len(files)+1)
	for _, f := range files {
		nodes := typegraph.NodesIn(slices.Values(targets), f.src)

		content, _, err := renderFileWith(
			sel, hdr, pkg, f, nodes, slices.Sorted(maps.Keys(f.support)), helpers,
		)
		if err != nil {
			return nil, err
		}

		out = append(out, File{Path: f.path(), Content: content})
	}
	// A helpers file declares its helpers sorted by name, that is by op and
	// then by the hashes of their signatures.
	callers := opiter.WrapSeq(slices.Values(files)).
		Map(func(f *partialFile) base.Caller { return f.caller(targets) }).
		CollectSlice()

	shared := filepath.Join(dir, base.PackageFile(pkg, HelpersFile))
	for _, hf := range base.HostShared(naming, helperKind, shared, callers) {
		content, err := renderHelpers(sel, hdr, pkg, hf, helpers)
		if err != nil {
			return nil, err
		}

		out = append(out, File{Path: hf.Path, Content: content})
	}

	return out, nil
}

// renderHelpers prints hf, declaring the helpers it hosts of helpers.
func renderHelpers(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	hf base.Host,
	helpers *helperSet,
) ([]byte, error) {
	reserve := slices.Clone(localNames)

	require := map[string]bool{}

	for _, name := range hf.Names {
		h := helpers.byName[name]

		reserve = opiter.WrapSeq(slices.Values(h.typeParams())).
			Map(typeParamName).
			AppendSeq(reserve)

		maps.Copy(require, h.spelled)
	}

	scope, err := base.NewFileScope(pkg, reserve, slices.Sorted(maps.Keys(require)))
	if err != nil {
		return nil, err
	}

	p := &mapper{
		sel: sel, FileScope: scope,
		helpers: helpers, calls: map[*helper]bool{}, support: map[string]bool{},
		refs: map[string]bool{},
	}

	var src strings.Builder

	for _, name := range hf.Names {
		text, err := p.helperDecl(name, helpers.byName[name])
		if err != nil {
			return nil, err
		}

		src.WriteString(text)
	}

	decls, err := genhelper.ParseDecls([]byte(src.String()))
	if err != nil {
		return nil, fmt.Errorf("partial: parse helpers of %s: %w", hf.Path, err)
	}

	content, err := genhelper.PrintDstFile(pkg.Name, hdr, hf.BuildLine, decls, scope.Imports)
	if err != nil {
		return nil, fmt.Errorf("partial: print %s: %w", hf.Path, err)
	}

	return content, nil
}

// checkHelperNames reports a package-level declaration of pkg named like a
// helper files call that the generator did not write.
func checkHelperNames(sel *selection, pkg *packages.Package, files []*partialFile) error {
	names := map[string]bool{}
	for _, f := range files {
		maps.Copy(names, f.used)
	}

	declaredErr := func(obj types.Object) error {
		return fmt.Errorf(
			"%w: %s: %s.%s is already declared for a helper of the generated methods",
			ErrConflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, obj.Name(),
		)
	}

	return errors.Join(
		opiter.WrapSeq(
			slices.Values(
				base.UnownedDecls(pkg, sel.opts.Registry, slices.Sorted(maps.Keys(names))),
			),
		).
			Map(declaredErr).
			CollectSlice()...,
	)
}

// fileHeader returns the comment lines every generated file of the package
// at pkgPath starts with.
func fileHeader(opts Options, pkgPath string, now time.Time) (dst.Decorations, error) {
	params := opts.Loaded.HeaderParams(pkgPath)
	params["merge"] = string(opts.Merge)
	params["merge_nested"] = string(opts.MergeNested)

	return base.Header(generatorPath, params, now)
}

// renderFileWith prints the partial declarations of f, those of the
// targets nodes it declares, with the packages of require imported.
func renderFileWith(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	f *partialFile,
	nodes []*typegraph.Node,
	require []string,
	helpers *helperSet,
) ([]byte, *mapper, error) {
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

	scope, err := base.NewFileScope(pkg, reserve, require)
	if err != nil {
		return nil, nil, err
	}

	p := &mapper{
		sel: sel, FileScope: scope,
		helpers: helpers, calls: map[*helper]bool{}, support: map[string]bool{},
	}

	var decls []dst.Decl

	for _, n := range nodes {
		d, err := p.decl(n)
		if err != nil {
			return nil, nil, err
		}

		decls = append(decls, d...)
	}

	// The partial types refer to the source types, so they must build under
	// the same constraint. A constraint in the file name is kept by the
	// output name.
	line, _ := base.BuildConstraint(nodes[0].Detail.File)

	content, err := genhelper.PrintDstFile(pkg.Name, hdr, line, decls, scope.Imports)
	if err != nil {
		return nil, nil, fmt.Errorf("partial: print %s: %w", nodes[0].Id.String(), err)
	}

	return content, p, nil
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
		"//",
		base.Marker(GeneratorName),
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
