package genhelper

import (
	"fmt"
	"go/ast"
	"go/types"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/typeprint"
)

// Dest is the file the generated text lands in. It spells names and types
// the way that file refers to them.
type Dest struct {
	// Imports resolves package paths to the identifiers the file accesses
	// them through, recording the imports the file misses.
	Imports *importdecl.Map
	// PkgPath is the package of the file. Its names are spelled
	// unqualified.
	PkgPath string
}

// Qual spells name of the package at pkgPath.
//
// Qual returns an error wrapping [importdecl.ErrUnknownImport] when Imports
// does not know the package.
func (d *Dest) Qual(pkgPath, name string) (string, error) {
	if pkgPath == d.PkgPath {
		return name, nil
	}
	ident, ok := d.Imports.Ident(pkgPath)
	if !ok {
		return "", fmt.Errorf("genhelper: %w: %s", importdecl.ErrUnknownImport, pkgPath)
	}
	return ident + "." + name, nil
}

// Type spells t. It returns the errors [Dest.TypeDst] returns.
func (d *Dest) Type(t types.Type) (string, error) {
	panic("unimplemented")
}

// TypeDst returns the dst expression spelling t.
//
// The error wraps one of the typeprint errors:
//
//   - [typeprint.ErrUnknownPackage]: Imports does not know the package of
//     a type t refers to.
//   - [typeprint.ErrUnexported]: t refers to an unexported type, field or
//     method of a package other than PkgPath.
//   - [typeprint.ErrUnsupported]: no type expression spells t, such as an
//     untyped constant type or a type declared inside a function.
func (d *Dest) TypeDst(t types.Type) (dst.Expr, error) {
	return typeprint.Dst(t, d.Imports, d.PkgPath)
}

// TypeAst returns the ast expression spelling t. It returns the errors
// [Dest.TypeDst] returns.
func (d *Dest) TypeAst(t types.Type) (ast.Expr, error) {
	return typeprint.Ast(t, d.Imports, d.PkgPath)
}
