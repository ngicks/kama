package partial

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs/suffix"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// GeneratorName is the name the generator marks its declarations with.
const GeneratorName = "kama-partial"

// FileSuffix is inserted into the name of a source file to name the file
// generated from it.
const FileSuffix = ".kama_partial"

// HelpersFile is the name of the file declaring the helpers the partial
// files of a package call.
const HelpersFile = "kama_partial_helpers.go"

// HelpersSuffix is inserted into the name of a source file that has a
// build constraint to name the file declaring the helpers only files with
// a build constraint call.
const HelpersSuffix = ".kama_partial_helpers"

// generatorPath is the import path of the generator, named by the first
// line of the header of every generated file.
const generatorPath = "github.com/ngicks/kama/kamalib/generator/partial"

func init() {
	if err := genmark.DefaultRegistry.Register(GeneratorName); err != nil {
		panic(err)
	}
}

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
	// declaration the generator did not write in the flag set file, two
	// partial fields with one name or a partial field named like a method,
	// more helpers sharing one hashed name than suffixes tell apart, a type
	// parameter named like a predeclared identifier or like a name the
	// declarations refer to, and a package-level declaration named like a
	// predeclared identifier the generated code spells.
	ErrConflict = errors.New("partial: name conflict")
	// ErrOption reports an invalid Options value.
	ErrOption = errors.New("partial: invalid option")
	// ErrOptionFile reports an option file that does not decode, or whose
	// entries name packages, types or fields that are not loaded.
	ErrOptionFile = errors.New("partial: invalid option file")
	// ErrStale reports a file of a loaded package that carries the marker of
	// the generator but that the run did not produce. It is a warning.
	ErrStale = errors.New("partial: stale generated file")
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
	return nil
}

// File is a generated file.
type File struct {
	// Path is the absolute path of the file, next to the source file it
	// derives from.
	Path    string
	Content []byte
}

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
	hdr, err := fileHeader(opts, opts.Now())
	if err != nil {
		return nil, nil, err
	}

	srcPkgs := sourcePackages(pkgs)
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
		base := filepath.Base(filename)
		out := suffix.SuffixFilename(base, FileSuffix)
		buildLine, constrained := buildConstraint(nodes[0].Detail.File)
		pf.files = append(pf.files, &partialFile{
			path:  filepath.Join(filepath.Dir(filename), out),
			nodes: nodes,
			// The suffix moves after a constraint of the file name only.
			constrained: constrained ||
				out != strings.TrimSuffix(base, ".go")+FileSuffix+".go",
			helpersPath: filepath.Join(
				filepath.Dir(filename), suffix.SuffixFilename(base, HelpersSuffix),
			),
			buildLine: buildLine,
		})
	}

	var files []File
	for _, pkgPath := range slices.Sorted(maps.Keys(byPkg)) {
		pf := byPkg[pkgPath]
		slices.SortFunc(
			pf.files,
			func(a, b *partialFile) int { return cmp.Compare(a.path, b.path) },
		)
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
		contents := make([][]byte, 0, len(out))
		for _, f := range out {
			contents = append(contents, f.Content)
		}
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
	return files, staleFiles(srcPkgs, opts.Registry, files, generated), nil
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
	for _, hf := range hostHelpers(filepath.Join(dir, HelpersFile), files) {
		content, err := renderHelpers(sel, hdr, pkg, hf, helpers)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: hf.path, Content: content})
	}
	return out, nil
}

// renderHelpers prints hf, declaring the helpers it hosts of helpers.
func renderHelpers(
	sel *selection,
	hdr dst.Decorations,
	pkg *packages.Package,
	hf *helpersFile,
	helpers *helperSet,
) ([]byte, error) {
	parser := importdecl.NewParser(pkg.Types)
	parser.Reserve(types.Universe.Names()...)
	parser.Reserve(pkg.Types.Scope().Names()...)
	parser.Reserve(localNames...)
	require := map[string]bool{}
	for _, name := range hf.hosted {
		h := helpers.byName[name]
		for _, tp := range h.typeParams() {
			parser.Reserve(tp.Obj().Name())
		}
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
	if hf.buildLine != "" {
		file.Decs.Start = append(file.Decs.Start, hf.buildLine, "\n")
	}
	var src strings.Builder
	for _, name := range hf.hosted {
		text, err := p.helperDecl(name, helpers.byName[name])
		if err != nil {
			return nil, err
		}
		src.WriteString(text)
	}
	decls, err := gofragment.ParseDstDecls([]byte(src.String()))
	if err != nil {
		return nil, fmt.Errorf("partial: parse helpers of %s: %w", hf.path, err)
	}
	for _, d := range decls {
		d.Decorations().Before = dst.EmptyLine
	}
	file.Decls = decls
	m.AddMissingDst(file)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return nil, fmt.Errorf("partial: print %s: %w", hf.path, err)
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
	var errs []error
	for _, helper := range slices.Sorted(maps.Keys(names)) {
		if obj := pkg.Types.Scope().Lookup(helper); obj != nil && !sel.generated[obj] {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s.%s is already declared for a helper of the generated methods",
				ErrConflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, helper,
			))
		}
	}
	return errors.Join(errs...)
}

// fileHeader returns the comment lines every generated file starts with.
// The first line follows the convention of go help generate.
func fileHeader(opts Options, now time.Time) (dst.Decorations, error) {
	var optionFile any
	if opts.OptionFile != nil {
		optionFile = opts.OptionFile.Name
	}
	comment, err := genhelper.GeneratorParams{
		Name: generatorPath,
		Params: map[string]any{
			"merge":        string(opts.Merge),
			"merge_nested": string(opts.MergeNested),
			"option_file":  optionFile,
		},
		Generated: now,
	}.Comment()
	if err != nil {
		return nil, fmt.Errorf("partial: file header: %w", err)
	}
	return strings.Split(comment, "\n"), nil
}

// staleFiles returns a warning for each file of pkgs holding a declaration
// marked by the generator that is not among files. generated holds the
// packages files were generated into, keyed by package path.
func staleFiles(
	pkgs []*packages.Package,
	reg *genmark.Registry,
	files []File,
	generated map[string]bool,
) []error {
	produced := map[string]bool{}
	for _, f := range files {
		produced[filepath.Clean(f.Path)] = true
	}

	type stale struct{ path, name string }
	var found []stale
	for _, pkg := range pkgs {
		for _, f := range pkg.Syntax {
			tf := pkg.Fset.File(f.Pos())
			if tf == nil || produced[filepath.Clean(tf.Name())] || !markedFile(f, reg) {
				continue
			}
			// The flag set file has no build constraint and serves the
			// partial files of every build variant. A package the run
			// produced nothing for may still declare targets in files of a
			// variant not loaded, which keep needing it. A package the run
			// produced files for always gets it, so it cannot be stale there
			// and is left to the check above.
			if !generated[pkg.PkgPath] && filepath.Base(tf.Name()) == FlagSetFile {
				continue
			}
			shown := tf.Name()
			if pkg.Module != nil && pkg.Module.Dir != "" {
				if rel, err := filepath.Rel(pkg.Module.Dir, shown); err == nil &&
					rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					shown = rel
				}
			}
			found = append(found, stale{path: tf.Name(), name: shown})
		}
	}
	slices.SortFunc(found, func(a, b stale) int { return cmp.Compare(a.path, b.path) })

	var out []error
	for _, s := range found {
		out = append(out, fmt.Errorf(
			"%w: %s was generated by %s but not produced by this run; delete it if no longer needed",
			ErrStale,
			s.name,
			GeneratorName,
		))
	}
	return out
}

// markedFile reports whether a declaration of f carries the marker of the
// generator.
func markedFile(f *ast.File, reg *genmark.Registry) bool {
	marked := func(cg *ast.CommentGroup) bool {
		gen, ok := reg.Generated(cg)
		return ok && gen == GeneratorName
	}
	for _, node := range f.Decls {
		switch d := node.(type) {
		case *ast.FuncDecl:
			if marked(d.Doc) {
				return true
			}
		case *ast.GenDecl:
			if marked(d.Doc) {
				return true
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if marked(s.Doc) {
						return true
					}
				case *ast.ValueSpec:
					if marked(s.Doc) {
						return true
					}
				}
			}
		}
	}
	return false
}

// sourcePackages drops test variants, which repeat the declarations of the
// package they test, and the generated test main packages.
func sourcePackages(pkgs []*packages.Package) []*packages.Package {
	seen := map[string]bool{}
	var out []*packages.Package
	for _, p := range pkgs {
		if p.ID != p.PkgPath || seen[p.PkgPath] {
			continue
		}
		seen[p.PkgPath] = true
		out = append(out, p)
	}
	return out
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
		for tp := range n.Detail.Type.TypeParams().TypeParams() {
			parser.Reserve(tp.Obj().Name())
		}
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
	if line, ok := buildConstraint(nodes[0].Detail.File); ok {
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
		return nil, nil, fmt.Errorf("partial: print %s: %w", nodeName(nodes[0]), err)
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
		var key dst.Expr
		var value mapped
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
		return nil, fmt.Errorf("%s (%s): %w", nodeName(n), p.position(named.Obj().Pos()), err)
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
	var errs []error
	for tp := range named.TypeParams().TypeParams() {
		// The methods spell predeclared identifiers such as error, nil and
		// false, which a type parameter of that name would denote instead.
		if types.Universe.Lookup(tp.Obj().Name()) != nil {
			errs = append(errs, fmt.Errorf(
				"%w: %s: type parameter %s of %s shadows the predeclared %s the generated code spells",
				ErrConflict,
				p.position(tp.Obj().Pos()),
				tp.Obj().Name(),
				named.Obj().Name(),
				tp.Obj().Name(),
			))
			continue
		}
		if refs[tp.Obj().Name()] {
			errs = append(errs, fmt.Errorf(
				"%w: %s: type parameter %s shadows %s the partial declarations refer to",
				ErrConflict, p.position(tp.Obj().Pos()), tp.Obj().Name(), tp.Obj().Name(),
			))
		}
	}
	return errors.Join(errs...)
}

// checkPredeclared reports package-level declarations of pkg named like a
// predeclared identifier the generated files contents spell, which the
// declaration would shadow there.
//
// Every identifier the files use other than selected names, field and
// parameter names and declared names is taken as a use, so a predeclared
// name spelled for a type of pkg itself, as und.Und[string] for a field of
// a package-level type string, is reported too.
func checkPredeclared(pkg *packages.Package, contents [][]byte) error {
	used := map[string]bool{}
	for _, c := range contents {
		f, err := parser.ParseFile(token.NewFileSet(), "", c, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("partial: parse generated file of %s: %w", pkg.PkgPath, err)
		}
		declared := map[*ast.Ident]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				declared[n.Sel] = true
			case *ast.Field:
				for _, ident := range n.Names {
					declared[ident] = true
				}
			case *ast.FuncDecl:
				declared[n.Name] = true
			case *ast.TypeSpec:
				declared[n.Name] = true
			case *ast.ImportSpec:
				return false
			case *ast.Ident:
				if !declared[n] {
					used[n.Name] = true
				}
			}
			return true
		})
	}

	var errs []error
	for _, ident := range slices.Sorted(maps.Keys(used)) {
		if types.Universe.Lookup(ident) == nil {
			continue
		}
		if obj := pkg.Types.Scope().Lookup(ident); obj != nil {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s.%s shadows the predeclared %s the generated code spells",
				ErrConflict, pkg.Fset.Position(obj.Pos()), pkg.PkgPath, ident, ident,
			))
		}
	}
	return errors.Join(errs...)
}

// buildConstraint returns the //go:build line of f.
func buildConstraint(f *ast.File) (string, bool) {
	for _, cg := range f.Comments {
		if cg.Pos() >= f.Package {
			break
		}
		for _, c := range cg.List {
			if constraint.IsGoBuild(c.Text) {
				return c.Text, true
			}
		}
	}
	return "", false
}

func partialName(typeName string) string { return typeName + "Partial" }

func nodeName(n *typegraph.Node) string {
	return n.Id.PackagePath + "." + n.Id.TypeName
}
