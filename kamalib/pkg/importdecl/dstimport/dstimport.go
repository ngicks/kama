// Package dstimport applies the imports of an importdecl.Map to dst
// syntax trees. Package importdecl itself knows go/ast only, so that a
// program spelling Go source as text does not depend on dst.
package dstimport

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"

	"github.com/dave/dst"
	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
)

// Parse parses the import specs of a file, as dst.File.Imports holds them,
// as importdecl.Parser.ParseAst does.
func Parse(p *importdecl.Parser, specs []*dst.ImportSpec) (*importdecl.Map, error) {
	return p.ParseAst(
		opiter.WrapSeq(slices.Values(specs)).
			Map(astSpec).
			CollectSlice(),
	)
}

// Expr returns the expression referring to ty, or nil if its package is
// unknown to m.
func Expr(m *importdecl.Map, ty importdecl.QualifiedType) *dst.SelectorExpr {
	ident, ok := m.Ident(ty.PackagePath)
	if !ok {
		return nil
	}

	return &dst.SelectorExpr{X: dst.NewIdent(ident), Sel: dst.NewIdent(ty.TypeName)}
}

// AddMissing adds the imports m misses to the first import declaration of
// f, creating one if f has none, and to f.Imports. Imports f already has
// are skipped, so calling it again is safe.
func AddMissing(m *importdecl.Map, f *dst.File) {
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

	decl := opiter.WrapSeq(slices.Values(f.Decls)).
		Map(asGenDecl).
		Filter(func(g *dst.GenDecl) bool { return g != nil && g.Tok == token.IMPORT }).
		First().
		UnwrapOrDefault()

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

func asGenDecl(d dst.Decl) *dst.GenDecl {
	g, _ := d.(*dst.GenDecl)
	return g
}

// astSpec returns the go/ast spec of s, with the name and the path
// literal, which is all importdecl.Parser.ParseAst reads.
func astSpec(s *dst.ImportSpec) *ast.ImportSpec {
	out := &ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: s.Path.Value}}
	if s.Name != nil {
		out.Name = ast.NewIdent(s.Name.Name)
	}

	return out
}
