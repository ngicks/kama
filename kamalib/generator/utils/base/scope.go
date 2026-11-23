package base

import (
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"

	"github.com/dave/dst"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
)

// FileScope is what a generator spells packages and types through in one
// generated file: the package the file belongs to and the imports of the
// file.
//
// The imports start empty. Spelling a package or a type records the
// package as used. [importdecl.Map.AddMissingDst] of Imports writes the
// import declarations of the packages used.
type FileScope struct {
	// Pkg is the package the file belongs to.
	Pkg *packages.Package
	// Imports are the imports of the file.
	Imports *importdecl.Map

	parser *importdecl.Parser
}

// NewFileScope returns the scope of a new file of pkg.
//
// An import name avoids the predeclared identifiers, the package-level
// identifiers of pkg and reserve, which are the identifiers the generated
// declarations declare themselves: an import of such a name would shadow,
// or be shadowed by, what the declarations spell.
//
// require are the paths of packages the file spells by path rather than
// through a type of pkg. A package pkg does not import, directly or not,
// is unknown otherwise.
func NewFileScope(pkg *packages.Package, reserve, require []string) (*FileScope, error) {
	parser := importdecl.NewParser(pkg.Types)
	parser.Reserve(types.Universe.Names()...)
	parser.Reserve(pkg.Types.Scope().Names()...)
	parser.Reserve(reserve...)

	for _, path := range require {
		err := parser.Require(importdecl.Import{
			Package: importdecl.Package{Path: path, Name: filepath.Base(path)},
		})
		if err != nil {
			return nil, err
		}
	}

	s := &FileScope{Pkg: pkg, parser: parser}

	return s.Fresh()
}

// Fresh returns a scope of another file of the same package with the same
// reserved identifiers and required packages, and with no package used.
func (s *FileScope) Fresh() (*FileScope, error) {
	m, err := s.parser.ParseDst(nil)
	if err != nil {
		return nil, err
	}

	return &FileScope{Pkg: s.Pkg, Imports: m, parser: s.parser}, nil
}

// Ident returns the identifier the file refers to the package at path by,
// and records the package as used. It reports false for a package the
// scope does not know.
func (s *FileScope) Ident(path string) (string, bool) { return s.Imports.Ident(path) }

// TypeExpr spells t as a type expression of the file.
func (s *FileScope) TypeExpr(t types.Type) (dst.Expr, error) {
	return typeprint.Dst(t, s.Imports, s.Pkg.PkgPath)
}

// TypeString spells t as the source text of a type expression of the file.
func (s *FileScope) TypeString(t types.Type) (string, error) {
	e, err := s.TypeExpr(t)
	if err != nil {
		return "", err
	}

	b, err := gofragment.PrintDstExpr(e)
	if err != nil {
		return "", fmt.Errorf("base: %w", err)
	}

	return string(b), nil
}

// Position returns the position of pos in the files of the package.
func (s *FileScope) Position(pos token.Pos) token.Position { return s.Pkg.Fset.Position(pos) }

// FuncContext returns a context positioned at a new root scope for one
// generated function, which spells the type parameters tparams. The scope
// avoids the predeclared identifiers and the package-level identifiers of
// the package, both of which the function may spell, and the identifiers
// of the imports of the file.
func (s *FileScope) FuncContext(tparams []string) genhelper.BaseContext {
	sc := genhelper.NewScope()
	sc.ReserveUniverse()
	sc.ReserveTypes(s.Pkg.Types.Scope())
	sc.Reserve(tparams...)

	return genhelper.NewBaseContext(sc, &genhelper.Package{Imports: s.Imports, Path: s.Pkg.PkgPath})
}
