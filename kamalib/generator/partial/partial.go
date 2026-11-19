package partial

import (
	"bytes"
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

	"github.com/ngicks/kama/kamalib/generator/base"

	"github.com/ngicks/opiter"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
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
	// ErrOptionFile reports an option file that does not decode, or whose
	// entries name packages, types or fields that are not loaded. Proc also
	// wraps errors wrapping genopt.ErrPackageFile in it: an invalid
	// //kama:option-file directive and a package option file that cannot be
	// loaded.
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
	// genmark.DefaultRegistry when nil.
	Registry *genmark.Registry
	// OptionFile holds options given beside the directives, as
	// LoadOptionFile or DecodeOptionFile return them. It is none when nil.
	OptionFile *OptionFile
	// PackageOptionFiles hold the options of package option files, as
	// DecodePackageOptionFile returns them, at most one per package. The
	// options of each apply to its own package.
	PackageOptionFiles []*OptionFile
	// Now gives the time the header of the generated files records. It is
	// time.Now when nil.
	Now func() time.Time
}

// WithDefaults returns o with empty fields set to their defaults.
func (o Options) WithDefaults() Options {
	o.Merge = cmp.Or(o.Merge, MergeOverwrite)

	o.MergeNested = cmp.Or(o.MergeNested, MergeNestedRecursive)
	if o.Registry == nil {
		o.Registry = genmark.DefaultRegistry
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

	seen := map[string]string{}

	for _, f := range o.PackageOptionFiles {
		switch {
		case f == nil || f.Package == "":
			return fmt.Errorf("%w: a package option file names no package", ErrOption)
		}

		if prev, ok := seen[f.Package]; ok {
			return fmt.Errorf(
				"%w: package %s has two option files, %s and %s",
				ErrOption, f.Package, prev, f.Name,
			)
		}

		seen[f.Package] = f.Name
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
// The warnings wrap ErrStale, one for each file of pkgs carrying the
// marker of the generator that is not among the returned files, sorted by
// path. The flag set file of a package the run produces no files for is
// not reported.
//
// pkgs must be loaded as gopackages.Load does. Test variants are skipped.
// The returned content is not formatted beyond what printing the syntax
// tree gives.
func Generate(pkgs []*packages.Package, opts Options) ([]File, []error, error) {
	if err := opts.Validate(); err != nil {
		return nil, nil, err
	}

	opts = opts.WithDefaults()
	now := opts.Now()

	srcPkgs := base.SourcePackages(pkgs)

	sel, err := selectTargets(srcPkgs, opts)
	if err != nil {
		return nil, nil, err
	}

	byFile := map[string][]*typegraph.Node{}

	for _, n := range sel.targets {
		filename := sel.filename(n)
		byFile[filename] = append(byFile[filename], n)
	}

	type pkgFiles struct {
		pkg   *packages.Package
		dir   string
		files []*partialFile
	}

	byPkg := map[string]*pkgFiles{}

	for _, filename := range slices.Sorted(maps.Keys(byFile)) {
		nodes := byFile[filename]
		slices.SortFunc(nodes, func(a, b *typegraph.Node) int {
			return cmp.Compare(a.Detail.Ts.Pos(), b.Detail.Ts.Pos())
		})
		pkg := nodes[0].Detail.Pkg

		pf := byPkg[pkg.PkgPath]
		if pf == nil {
			pf = &pkgFiles{pkg: pkg, dir: filepath.Dir(filename)}
			byPkg[pkg.PkgPath] = pf
		}

		buildLine, constrained := base.BuildConstraint(nodes[0].Detail.File)
		pf.files = append(pf.files, &partialFile{
			path:        naming.SourceFile(filename),
			nodes:       nodes,
			constrained: constrained || base.Constrained(filename),
			helpersPath: naming.SharedFileOf(helperKind, filename),
			buildLine:   buildLine,
		})
	}

	var files []File

	for _, pkgPath := range slices.Sorted(maps.Keys(byPkg)) {
		pf := byPkg[pkgPath]
		slices.SortFunc(
			pf.files,
			func(a, b *partialFile) int { return cmp.Compare(a.path, b.path) },
		)

		hdr, err := fileHeader(opts, pkgPath, now)
		if err != nil {
			return nil, nil, err
		}

		out, err := renderPackage(sel, hdr, pf.pkg, pf.dir, pf.files)
		if err != nil {
			return nil, nil, err
		}
		// The flag set holds every method any flag kind calls: a partial
		// file of a build variant other than the one loaded keeps calling the
		// methods its flags use.
		content, err := renderFlagSet(pf.pkg, hdr)
		if err != nil {
			return nil, nil, err
		}

		out = append(out, File{Path: filepath.Join(pf.dir, FlagSetFile), Content: content})

		contents := opiter.WrapSeq(slices.Values(out)).
			Map(func(f File) []byte { return f.Content }).
			CollectSlice()

		if err := checkPredeclared(pf.pkg, contents); err != nil {
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
		// always gets it, so it cannot be stale there.
		Keep: func(pkg *packages.Package, path string) bool {
			return !generated[pkg.PkgPath] && filepath.Base(path) == FlagSetFile
		},
	}

	return files, stale.Files(srcPkgs, files), nil
}

// renderPackage prints the partial files of pkg, sorted by path, and the
// helpers files of dir.
//
// A helper is declared in one file of the package, which the files calling
// it must agree on before any is printed. A first pass therefore collects
// the helpers each file calls and the packages of supportPaths it spells,
// with every one of them required. The second pass prints each file
// requiring only those it spells: a required import is added whether the
// file uses it or not.
func renderPackage(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	dir string,
	files []*partialFile,
) ([]File, error) {
	helpers := newHelperSet()
	for _, f := range files {
		_, p, err := renderFileWith(sel, hdr, pkg, f, supportPaths, helpers)
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
		content, _, err := renderFileWith(
			sel, hdr, pkg, f, slices.Sorted(maps.Keys(f.support)), helpers,
		)
		if err != nil {
			return nil, err
		}

		out = append(out, File{Path: f.path, Content: content})
	}
	// A helpers file declares its helpers sorted by name, that is by op and
	// then by the hashes of their signatures.
	for _, hf := range base.HostShared(filepath.Join(dir, HelpersFile), callers(files)) {
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
	parser := importdecl.NewParser(pkg.Types)
	parser.Reserve(types.Universe.Names()...)
	parser.Reserve(pkg.Types.Scope().Names()...)
	parser.Reserve(localNames...)

	require := map[string]bool{}

	for _, name := range hf.Names {
		h := helpers.byName[name]

		parser.Reserve(
			opiter.WrapSeq(slices.Values(h.typeParams())).
				Map(typeParamName).
				CollectSlice()...,
		)

		maps.Copy(require, h.spelled)
	}

	for _, path := range slices.Sorted(maps.Keys(require)) {
		err := parser.Require(importdecl.Import{
			Package: importdecl.Package{Path: path, Name: filepath.Base(path)},
		})
		if err != nil {
			return nil, err
		}
	}

	m, err := parser.ParseDst(nil)
	if err != nil {
		return nil, err
	}

	p := &mapper{
		sel: sel, pkg: pkg, parser: parser, m: m,
		helpers: helpers, calls: map[*helper]bool{}, support: map[string]bool{},
		refs: map[string]bool{},
	}
	file := &dst.File{Name: dst.NewIdent(pkg.Name)}

	file.Decs.Start = append(slices.Clone(hdr), "\n")
	if hf.BuildLine != "" {
		file.Decs.Start = append(file.Decs.Start, hf.BuildLine, "\n")
	}

	var src strings.Builder

	for _, name := range hf.Names {
		text, err := p.helperDecl(name, helpers.byName[name])
		if err != nil {
			return nil, err
		}

		src.WriteString(text)
	}

	decls, err := gofragment.ParseDstDecls([]byte(src.String()))
	if err != nil {
		return nil, fmt.Errorf("partial: parse helpers of %s: %w", hf.Path, err)
	}

	for _, d := range decls {
		d.Decorations().Before = dst.EmptyLine
	}

	file.Decls = decls
	m.AddMissingDst(file)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return nil, fmt.Errorf("partial: print %s: %w", hf.Path, err)
	}

	return buf.Bytes(), nil
}

// checkHelperNames reports a package-level declaration of pkg named like a
// helper files call that the generator did not write.
func checkHelperNames(sel *selection, pkg *packages.Package, files []*partialFile) error {
	names := map[string]bool{}
	for _, f := range files {
		maps.Copy(names, f.used)
	}

	declaredErr := func(helper string) error {
		obj := pkg.Types.Scope().Lookup(helper)
		if obj == nil || sel.generated[obj] {
			return nil
		}

		return fmt.Errorf(
			"%w: %s: %s.%s is already declared for a helper of the generated methods",
			ErrConflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, helper,
		)
	}

	// errors.Join drops the nil errors of helpers declared by nothing else.
	return errors.Join(
		opiter.WrapSeq2(opiter.MapsSorted(names)).
			OmitL().
			Map(declaredErr).
			CollectSlice()...,
	)
}

// fileHeader returns the comment lines every generated file of the package
// at pkgPath starts with.
func fileHeader(opts Options, pkgPath string, now time.Time) (dst.Decorations, error) {
	var optionFile, packageOptionFile any
	if opts.OptionFile != nil {
		optionFile = opts.OptionFile.Name
	}

	pf := opiter.WrapSeq(slices.Values(opts.PackageOptionFiles)).
		Filter(func(f *OptionFile) bool { return f.Package == pkgPath }).
		First()

	if pf.IsSome() {
		packageOptionFile = pf.Value().Name
	}

	return base.Header(generatorPath, map[string]any{
		"merge":               string(opts.Merge),
		"merge_nested":        string(opts.MergeNested),
		"option_file":         optionFile,
		"package_option_file": packageOptionFile,
	}, now)
}

// renderFileWith prints the partial declarations of f, with the packages
// of require imported.
func renderFileWith(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	f *partialFile,
	require []string,
	helpers *helperSet,
) ([]byte, *mapper, error) {
	nodes := f.nodes
	parser := importdecl.NewParser(pkg.Types)
	// Import names must not be taken by declarations of the package, nor
	// by the type parameters and locals of the generated declarations,
	// which would shadow them. A predeclared name taken by an import would
	// shadow the predeclared identifier the declarations spell, as a
	// package named string would for und.Und[string]. A local renamed off
	// its base name reserves the new name itself, through the link of its
	// scope to the Map.
	parser.Reserve(types.Universe.Names()...)
	parser.Reserve(pkg.Types.Scope().Names()...)
	parser.Reserve(localNames...)

	for _, n := range nodes {
		// Only files with field mask methods reserve their locals, so that the
		// import names of other files stay as they were.
		if sel.fieldMasked(n.Id) {
			parser.Reserve(fieldMaskLocals...)
		}

		tparams := n.Detail.Type.TypeParams()

		parser.Reserve(
			opiter.WrapSeq(tparams.TypeParams()).
				Map(typeParamName).
				CollectSlice()...,
		)

		parser.Reserve(methodTypeParams(pkg.Types.Scope(), n.Detail.Type.TypeParams())...)
	}

	for _, path := range require {
		err := parser.Require(importdecl.Import{
			Package: importdecl.Package{Path: path, Name: filepath.Base(path)},
		})
		if err != nil {
			return nil, nil, err
		}
	}

	m, err := parser.ParseDst(nil)
	if err != nil {
		return nil, nil, err
	}

	p := &mapper{
		sel: sel, pkg: pkg, parser: parser, m: m,
		helpers: helpers, calls: map[*helper]bool{}, support: map[string]bool{},
	}
	file := &dst.File{Name: dst.NewIdent(pkg.Name)}

	file.Decs.Start = append(slices.Clone(hdr), "\n")
	// The partial types refer to the source types, so they must build under
	// the same constraint. A constraint in the file name is kept by the
	// output name.
	if line, ok := base.BuildConstraint(nodes[0].Detail.File); ok {
		file.Decs.Start = append(file.Decs.Start, line, "\n")
	}

	for _, n := range nodes {
		decls, err := p.decl(n)
		if err != nil {
			return nil, nil, err
		}

		file.Decls = append(file.Decls, decls...)
	}

	m.AddMissingDst(file)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return nil, nil, fmt.Errorf("partial: print %s: %w", nodes[0].Id.String(), err)
	}

	return buf.Bytes(), p, nil
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
		var fields []fieldInfo

		ty, fields, err = p.structType(n, u)
		if err == nil {
			err = w.structMethods(fields)
		}
	case *types.Slice:
		var elem mapped

		elem, err = p.value(u.Elem(), false)

		ty = &dst.ArrayType{Elt: elem.expr}
		if err == nil {
			err = w.collectionMethods(false, elem, u.Elem())
		}
	case *types.Map:
		var (
			key   dst.Expr
			value mapped
		)

		key, err = p.print(u.Key())
		if err == nil {
			value, err = p.value(u.Elem(), false)
		}

		ty = &dst.MapType{Key: key, Value: value.expr}
		if err == nil {
			err = w.collectionMethods(true, value, u.Elem())
		}
	}

	if err == nil {
		err = p.checkTypeParams(named)
	}

	if err != nil {
		return nil, fmt.Errorf("%s (%s): %w", n.Id.String(), p.position(named.Obj().Pos()), err)
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
		genmark.Marker(GeneratorName),
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
				p.position(tp.Obj().Pos()),
				tp.Obj().Name(),
				named.Obj().Name(),
				tp.Obj().Name(),
			)
		case refs[tp.Obj().Name()]:
			return fmt.Errorf(
				"%w: %s: type parameter %s shadows %s the partial declarations refer to",
				ErrConflict, p.position(tp.Obj().Pos()), tp.Obj().Name(), tp.Obj().Name(),
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

// checkPredeclared reports package-level declarations of pkg named like a
// predeclared identifier the generated files contents spell, which the
// declaration would shadow there. See base.ShadowedPredeclared for what
// counts as spelled.
func checkPredeclared(pkg *packages.Package, contents [][]byte) error {
	objs, err := base.ShadowedPredeclared(pkg, contents)
	if err != nil {
		return err
	}

	shadowErr := func(obj types.Object) error {
		return fmt.Errorf(
			"%w: %s: %s.%s shadows the predeclared %s the generated code spells",
			ErrConflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, obj.Name(), obj.Name(),
		)
	}

	return errors.Join(
		opiter.WrapSeq(slices.Values(objs)).
			Map(shadowErr).
			CollectSlice()...,
	)
}

func partialName(typeName string) string { return typeName + "Partial" }
