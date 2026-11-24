package genhelper

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

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

// Type spells t on one line, as gofmt lays out a type expression written
// on one line.
//
// The error wraps one of the typeprint errors:
//
//   - [typeprint.ErrUnknownPackage]: Imports is nil or does not know the
//     package of a type t refers to.
//   - [typeprint.ErrUnexported]: t refers to an unexported type, field or
//     method of a package other than Path.
//   - [typeprint.ErrUnsupported]: no type expression spells t, such as an
//     untyped constant type or a type declared inside a function.
func (p *Package) Type(t types.Type) (string, error) {
	e, err := p.TypeAst(t)
	if err != nil {
		return "", err
	}

	// go/printer lays out a struct or an interface over several lines
	// unless its braces sit on one line. Placing every brace at one
	// position keeps the expression on one line.
	fset := token.NewFileSet()
	pos := fset.AddFile("", -1, 1).Pos(0)

	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.StructType:
			n.Fields.Opening, n.Fields.Closing = pos, pos
		case *ast.InterfaceType:
			n.Methods.Opening, n.Methods.Closing = pos, pos
		}

		return true
	})

	b, err := gofragment.PrintAst(fset, e)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// TypeAst returns the ast expression spelling t. It returns the errors
// [Package.Type] returns.
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
