// Package importdecl resolves the identifiers through which a Go file
// accesses imported packages, and adds the imports the file is missing.
//
// A Parser knows the packages a file may import, usually from type
// information. Parsing the import specs of a file gives a Map. Asking the
// Map for the identifier of a package the file does not import records it
// as missing, under a name that does not conflict with the file's imports.
//
// Lifted from github.com/ngicks/go-codegen/graft/pkg/importmap.
package importdecl

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

var ErrUnknownImport = errors.New("importdecl: unknown import")

// Package is a package a file can import.
type Package struct {
	Path string
	// Name is the package name, which may differ from the last element of
	// Path, as for "math/rand/v2". Empty means unknown.
	Name string
}

// Import is an import spec.
type Import struct {
	// Name is the explicit name of the spec, as in import name "path".
	Name    string
	Package Package
}

// Ident returns the identifier through which the file accesses the
// package.
func (i Import) Ident() string {
	switch {
	case i.Name != "":
		return i.Name
	case i.Package.Name != "":
		return i.Package.Name
	default:
		return inferName(i.Package.Path)
	}
}

// inferName guesses the package name from its path, skipping a major
// version suffix like v2.
func inferName(pkgPath string) string {
	base := path.Base(pkgPath)
	if v, ok := strings.CutPrefix(base, "v"); ok && v != "" &&
		strings.Trim(v, "0123456789") == "" {
		return path.Base(path.Dir(pkgPath))
	}
	return base
}

func compareImport(l, r Import) int {
	return cmp.Or(
		cmp.Compare(l.Package.Path, r.Package.Path),
		cmp.Compare(l.Name, r.Name),
	)
}

// QualifiedType names a type declared in a package.
type QualifiedType struct {
	PackagePath string
	TypeName    string
}

// Is reports whether ty is the named type t names.
func (t QualifiedType) Is(ty types.Type) bool {
	named, ok := ty.(*types.Named)
	if !ok || named.Obj() == nil {
		return false
	}

	var pkgPath string
	// Pkg is nil for predeclared types such as error.
	if pkg := named.Obj().Pkg(); pkg != nil {
		pkgPath = pkg.Path()
	}
	return t.PackagePath == pkgPath && t.TypeName == named.Obj().Name()
}

// Parser parses the import specs of files.
type Parser struct {
	known    map[string]Package
	required []Import
}

// NewParser returns a Parser that knows pkg and every package it imports,
// directly or not.
func NewParser(pkg *types.Package) *Parser {
	p := &Parser{known: map[string]Package{}}

	// Breadth first, since Go forbids import cycles and depth-first
	// recursion visits shared imports again and again.
	layer := []*types.Package{pkg}
	for len(layer) > 0 {
		var next []*types.Package
		for _, pkg := range layer {
			if _, ok := p.known[pkg.Path()]; ok {
				continue
			}
			p.known[pkg.Path()] = Package{Path: pkg.Path(), Name: pkg.Name()}
			next = append(next, pkg.Imports()...)
		}
		layer = next
	}

	return p
}

// NewParserFromPackages returns a Parser that knows pkgs and every package
// they import, directly or not. pkgs must be loaded with type information.
func NewParserFromPackages(pkgs []*packages.Package) *Parser {
	p := &Parser{known: map[string]Package{}}
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		p.known[pkg.PkgPath] = Package{Path: pkg.PkgPath, Name: pkg.Name}
	})
	return p
}

// Require adds imports every Map from p reports as missing, unless the
// parsed file already imports their packages.
func (p *Parser) Require(imports ...Import) {
	p.required = append(p.required, imports...)
	slices.SortFunc(p.required, compareImport)
	p.required = slices.Compact(p.required)
}

// ParseAst parses the import specs of a file, as ast.File.Imports holds
// them.
func (p *Parser) ParseAst(specs []*ast.ImportSpec) (*Map, error) {
	return p.parse(func(yield func(string, string) bool) {
		for _, s := range specs {
			var name string
			if s.Name != nil {
				name = s.Name.Name
			}
			if !yield(name, s.Path.Value) {
				return
			}
		}
	})
}

// ParseDst parses the import specs of a file, as dst.File.Imports holds
// them.
func (p *Parser) ParseDst(specs []*dst.ImportSpec) (*Map, error) {
	return p.parse(func(yield func(string, string) bool) {
		for _, s := range specs {
			var name string
			if s.Name != nil {
				name = s.Name.Name
			}
			if !yield(name, s.Path.Value) {
				return
			}
		}
	})
}

func (p *Parser) parse(specs iter.Seq2[string, string]) (*Map, error) {
	m := &Map{
		known:   p.known,
		idents:  map[string]bool{},
		imports: map[string]Import{},
	}

	for name, lit := range specs {
		pkgPath, err := strconv.Unquote(lit)
		if err != nil {
			return nil, fmt.Errorf("importdecl: import path %s: %w", lit, err)
		}

		// Blank and dot imports give no identifier to access the package.
		if name == "_" || name == "." {
			continue
		}

		imp := Import{Name: name, Package: m.lookup(pkgPath, p.required)}
		if name == "" && imp.Package.Name == "" {
			return nil, fmt.Errorf("%w: %q", ErrUnknownImport, pkgPath)
		}

		m.idents[imp.Ident()] = true
		if _, ok := m.imports[pkgPath]; !ok {
			m.imports[pkgPath] = imp
		}
	}

	for _, imp := range p.required {
		if _, ok := m.imports[imp.Package.Path]; !ok {
			m.addMissing(imp)
		}
	}

	return m, nil
}

// Map maps package paths to the identifiers a file accesses them through.
type Map struct {
	known map[string]Package
	// idents are the identifiers taken by imports.
	idents map[string]bool
	// imports are the imports the file has or misses, keyed by path.
	imports map[string]Import
	// missing are the imports the file misses, sorted by path.
	missing []Import
}

func (m *Map) lookup(pkgPath string, required []Import) Package {
	if pkg, ok := m.known[pkgPath]; ok {
		return pkg
	}
	if i := slices.IndexFunc(required, func(imp Import) bool {
		return imp.Package.Path == pkgPath
	}); i >= 0 {
		return required[i].Package
	}
	return Package{Path: pkgPath}
}

// addMissing records imp as missing under an identifier no other import
// takes, and returns the recorded import.
func (m *Map) addMissing(imp Import) Import {
	ident := imp.Ident()
	for i := 1; m.idents[ident]; i++ {
		ident = imp.Ident() + "_" + strconv.Itoa(i)
	}
	m.idents[ident] = true

	// An explicit name keeps the identifier right when the package name is
	// unknown or the path does not tell it.
	pkg := imp.Package
	if pkg.Name == "" || ident != pkg.Name || pkg.Name != inferName(pkg.Path) {
		imp.Name = ident
	} else {
		imp.Name = ""
	}

	m.imports[pkg.Path] = imp
	i, _ := slices.BinarySearchFunc(m.missing, imp, compareImport)
	m.missing = slices.Insert(m.missing, i, imp)
	return imp
}

// Ident returns the identifier through which the file accesses the package
// at pkgPath. A known package the file does not import is recorded as
// missing. ok is false for an unknown package.
func (m *Map) Ident(pkgPath string) (ident string, ok bool) {
	if imp, ok := m.imports[pkgPath]; ok {
		return imp.Ident(), true
	}

	pkg, ok := m.known[pkgPath]
	if !ok {
		return "", false
	}
	return m.addMissing(Import{Package: pkg}).Ident(), true
}

// AstExpr returns the expression referring to ty, or nil if its package is
// unknown.
func (m *Map) AstExpr(ty QualifiedType) *ast.SelectorExpr {
	ident, ok := m.Ident(ty.PackagePath)
	if !ok {
		return nil
	}
	return &ast.SelectorExpr{X: ast.NewIdent(ident), Sel: ast.NewIdent(ty.TypeName)}
}

// DstExpr returns the expression referring to ty, or nil if its package is
// unknown.
func (m *Map) DstExpr(ty QualifiedType) *dst.SelectorExpr {
	ident, ok := m.Ident(ty.PackagePath)
	if !ok {
		return nil
	}
	return &dst.SelectorExpr{X: dst.NewIdent(ident), Sel: dst.NewIdent(ty.TypeName)}
}

// Qualifier returns a types.Qualifier that qualifies members of every
// package other than currentPkgPath with their identifiers.
func (m *Map) Qualifier(currentPkgPath string) types.Qualifier {
	return func(p *types.Package) string {
		if p.Path() == currentPkgPath {
			return ""
		}
		ident, _ := m.Ident(p.Path())
		return ident
	}
}

// Missing returns the imports the file misses, sorted by path. Name is set
// only where the import needs an explicit name.
func (m *Map) Missing() iter.Seq[Import] {
	return slices.Values(slices.Clone(m.missing))
}

// AddMissingAst adds the missing imports to f. Imports f already has are
// skipped, so calling it again is safe.
func (m *Map) AddMissingAst(fset *token.FileSet, f *ast.File) {
	for imp := range m.Missing() {
		astutil.AddNamedImport(fset, f, imp.Name, imp.Package.Path)
	}
}

// AddMissingDst adds the missing imports to the first import declaration
// of f, creating one if f has none, and to f.Imports. Imports f already has
// are skipped, so calling it again is safe.
func (m *Map) AddMissingDst(f *dst.File) {
	type spec struct{ name, path string }
	has := map[spec]bool{}
	for _, s := range f.Imports {
		var name string
		if s.Name != nil {
			name = s.Name.Name
		}
		pkgPath, err := strconv.Unquote(s.Path.Value)
		if err != nil {
			continue
		}
		has[spec{name, pkgPath}] = true
	}

	var decl *dst.GenDecl
	for _, d := range f.Decls {
		if g, ok := d.(*dst.GenDecl); ok && g.Tok == token.IMPORT {
			decl = g
			break
		}
	}

	for imp := range m.Missing() {
		if has[spec{imp.Name, imp.Package.Path}] {
			continue
		}

		if decl == nil {
			decl = &dst.GenDecl{Tok: token.IMPORT}
			f.Decls = slices.Insert(f.Decls, 0, dst.Decl(decl))
		}

		s := &dst.ImportSpec{
			Path: &dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(imp.Package.Path)},
		}
		if imp.Name != "" {
			s.Name = dst.NewIdent(imp.Name)
		}
		decl.Specs = append(decl.Specs, s)
		f.Imports = append(f.Imports, s)
	}

	if decl != nil && len(decl.Specs) > 1 {
		decl.Lparen = true
	}
}
