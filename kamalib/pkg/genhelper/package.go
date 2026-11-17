package genhelper

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/pkg/gofragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
)

// Package is the package the generated text lands in, seen from the file
// receiving the text. It spells names and types the way that file refers
// to them.
type Package struct {
	// Imports resolves package paths to the identifiers the file accesses
	// them through, recording the imports the file misses. A nil Imports
	// knows no package, so only names and types of Path and the
	// predeclared types are spelled.
	Imports *importdecl.Map
	// Path is the package of the file. Its names are spelled
	// unqualified.
	Path string
	// Fset holds the positions of the ast nodes the ast steps parse and
	// print. Only those steps need it.
	Fset *token.FileSet
}

// Qual spells name of the package at pkgPath.
//
// Qual returns an error wrapping [importdecl.ErrUnknownImport] when Imports
// is nil or does not know the package.
func (p *Package) Qual(pkgPath, name string) (string, error) {
	if pkgPath == p.Path {
		return name, nil
	}

	ident, ok := "", false
	if p.Imports != nil {
		ident, ok = p.Imports.Ident(pkgPath)
	}

	if !ok {
		return "", fmt.Errorf("genhelper: %w: %s", importdecl.ErrUnknownImport, pkgPath)
	}

	return ident + "." + name, nil
}

// Type spells t. It returns the errors [Package.TypeDst] returns.
func (p *Package) Type(t types.Type) (string, error) {
	e, err := p.TypeDst(t)
	if err != nil {
		return "", err
	}

	b, err := gofragment.PrintDstExpr(e)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// TypeDst returns the dst expression spelling t.
//
// The error wraps one of the typeprint errors:
//
//   - [typeprint.ErrUnknownPackage]: Imports is nil or does not know the
//     package of a type t refers to.
//   - [typeprint.ErrUnexported]: t refers to an unexported type, field or
//     method of a package other than Path.
//   - [typeprint.ErrUnsupported]: no type expression spells t, such as an
//     untyped constant type or a type declared inside a function.
func (p *Package) TypeDst(t types.Type) (dst.Expr, error) {
	return typeprint.Dst(t, p.imports(), p.Path)
}

// TypeAst returns the ast expression spelling t. It returns the errors
// [Package.TypeDst] returns.
func (p *Package) TypeAst(t types.Type) (ast.Expr, error) {
	return typeprint.Ast(t, p.imports(), p.Path)
}

// imports returns Imports, or an empty map in place of a nil one.
// typeprint dereferences the map for every package other than Path, and a
// zero map reports each of them as unknown as an empty Imports does.
func (p *Package) imports() *importdecl.Map {
	if p.Imports == nil {
		return &importdecl.Map{}
	}

	return p.Imports
}
