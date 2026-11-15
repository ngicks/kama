// Package importdecl resolves the identifiers through which a Go file
// accesses imported packages, and adds the imports the file is missing.
//
// A Parser knows the packages a file may import, usually from type
// information. Parsing the import specs of a file gives a Map. Asking the
// Map for the identifier of a package the file does not import records it
// as missing, under a name that does not conflict with the file's imports.
//
// Names the file declares itself can be reserved, before parsing with
// Parser.Reserve or afterwards with Map.Reserve, so that missing imports
// avoid them. Map.HasIdent tells such a caller which names the imports take.
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
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/dave/dst"
	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

var (
	ErrUnknownImport = errors.New("importdecl: unknown import")
	ErrInvalidName   = errors.New("importdecl: invalid name")
)

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

// inferName guesses the package name from its path. It skips a major
// version element like v2 and drops every character an identifier cannot
// hold. A guess starting with a digit or being a keyword gets a leading _.
// Nothing left, or only _, gives pkg.
func inferName(pkgPath string) string {
	base := path.Base(pkgPath)
	if v, ok := strings.CutPrefix(base, "v"); ok && v != "" &&
		strings.Trim(v, "0123456789") == "" {
		base = path.Base(path.Dir(pkgPath))
	}

	base = strings.Map(func(r rune) rune {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, base)

	switch {
	// The blank identifier cannot refer to a package.
	case base == "" || base == "_":
		return "pkg"
	// Only a leading digit or a keyword keeps identifier characters from
	// forming an identifier.
	case !token.IsIdentifier(base):
		return "_" + base
	default:
		return base
	}
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
	reserved map[string]bool
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
// parsed file already imports their packages. A non-empty Name or
// Package.Name must be an identifier other than _.
func (p *Parser) Require(imports ...Import) error {
	for _, imp := range imports {
		for _, name := range []string{imp.Name, imp.Package.Name} {
			if name != "" && (!token.IsIdentifier(name) || name == "_") {
				return fmt.Errorf(
					"%w: %q for %q is not an identifier",
					ErrInvalidName, name, imp.Package.Path,
				)
			}
		}
	}

	p.required = append(p.required, imports...)
	slices.SortFunc(p.required, compareImport)
	p.required = slices.Compact(p.required)
	return nil
}

// Reserve marks idents as taken in every Map p parses, so that no missing
// import gets one of them as its name. Callers reserve the identifiers the
// file declares or uses unqualified. A missing import whose name is
// reserved gets the next free name, as name_2. Imports the parsed file
// already has keep their names.
func (p *Parser) Reserve(idents ...string) {
	if p.reserved == nil {
		p.reserved = map[string]bool{}
	}
	for _, ident := range idents {
		p.reserved[ident] = true
	}
}

// ParseAst parses the import specs of a file, as ast.File.Imports holds
// them.
func (p *Parser) ParseAst(specs []*ast.ImportSpec) (*Map, error) {
	return p.parse(opiter.Divide(astSpecNamePath, slices.Values(specs)))
}

func astSpecNamePath(s *ast.ImportSpec) (name, pathLit string) {
	if s.Name != nil {
		name = s.Name.Name
	}
	return name, s.Path.Value
}

// ParseDst parses the import specs of a file, as dst.File.Imports holds
// them.
func (p *Parser) ParseDst(specs []*dst.ImportSpec) (*Map, error) {
	return p.parse(opiter.Divide(dstSpecNamePath, slices.Values(specs)))
}

func dstSpecNamePath(s *dst.ImportSpec) (name, pathLit string) {
	if s.Name != nil {
		name = s.Name.Name
	}
	return name, s.Path.Value
}

func (p *Parser) parse(specs iter.Seq2[string, string]) (*Map, error) {
	m := &Map{
		known:   p.known,
		idents:  maps.Clone(p.reserved),
		imports: map[string]Import{},
		held:    map[string]bool{},
	}
	if m.idents == nil {
		m.idents = map[string]bool{}
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
		m.held[imp.Ident()] = true
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

func asGenDecl(d dst.Decl) *dst.GenDecl {
	g, _ := d.(*dst.GenDecl)
	return g
}

func isImportDecl(g *dst.GenDecl) bool { return g != nil && g.Tok == token.IMPORT }

// Map maps package paths to the identifiers a file accesses them through.
type Map struct {
	known map[string]Package
	// idents are the identifiers taken by imports and those reserved by
	// Parser.Reserve or Map.Reserve.
	idents map[string]bool
	// held are the identifiers taken by imports only. A caller reserving its
	// own names asks HasIdent which names the imports take, and reserved
	// names must not come back to it as import identifiers.
	held map[string]bool
	// imports are the imports the file has or misses, keyed by path.
	imports map[string]Import
	// missing are the imports the file misses, sorted by path.
	missing []Import
}

func (m *Map) lookup(pkgPath string, required []Import) Package {
	if pkg, ok := m.known[pkgPath]; ok {
		return pkg
	}
	hasPath := func(imp Import) bool { return imp.Package.Path == pkgPath }
	if imp := opiter.First(opiter.Filter(hasPath, slices.Values(required))); imp.IsSome() {
		return imp.Value().Package
	}
	return Package{Path: pkgPath}
}

// addMissing records imp as missing under an identifier no other import
// takes, and returns the recorded import.
func (m *Map) addMissing(imp Import) Import {
	// The second import of a name is name_2, the first being name itself.
	ident := imp.Ident()
	for i := 2; m.idents[ident]; i++ {
		ident = imp.Ident() + "_" + strconv.Itoa(i)
	}
	m.idents[ident] = true
	m.held[ident] = true

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

// Reserve marks idents as taken, so that no import recorded as missing
// afterwards gets one of them as its name. A missing import whose name is
// reserved gets the next free name, as name_2. Imports m already holds keep
// their identifiers.
func (m *Map) Reserve(idents ...string) {
	for _, ident := range idents {
		m.idents[ident] = true
	}
}

// HasIdent reports whether the file accesses an import through ident, one
// the file has or one recorded as missing. Names only reserved by
// Parser.Reserve or Map.Reserve do not count.
func (m *Map) HasIdent(ident string) bool {
	return m.held[ident]
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

// Imports returns every import the file accesses a package through, those
// it has and those it misses, sorted by path. Name is set where the spec
// names the import and where a missing import needs an explicit name.
//
// Blank and dot imports give no identifier, so Imports leaves them out. A
// package the file imports twice appears once, under its first name.
func (m *Map) Imports() iter.Seq[Import] {
	return slices.Values(slices.SortedFunc(maps.Values(m.imports), compareImport))
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

	genDecls := opiter.Map(asGenDecl, slices.Values(f.Decls))
	decl := opiter.First(opiter.Filter(isImportDecl, genDecls)).UnwrapOrDefault()

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
