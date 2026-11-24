// Package dstgen holds the parts of genhelper that work on dst nodes:
// steps editing the text as dst nodes, the dst expression of a type, and
// the declarations of an existing dst file. It is a package of its own so
// that genhelper does not make dst a dependency of its importers.
package dstgen

import (
	"go/types"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
	"github.com/ngicks/kama/kamalib/pkg/gofragment/dstfragment"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/importdecl/dstimport"
	"github.com/ngicks/kama/kamalib/pkg/typeprint/dsttype"
)

// DstExpr is a step editing the text before it as one dst expression. The
// step parses the text, calls f and prints the expression f returns.
//
// The step returns the errors of [dstfragment.ParseDstExpr] and
// [dstfragment.PrintDstExpr], and the error of f as it is.
func DstExpr[C genhelper.Context[C]](
	f func(c C, e dst.Expr) (C, dst.Expr, error),
) genhelper.Step[C] {
	return step(dstfragment.ParseDstExpr, f, dstfragment.PrintDstExpr)
}

// DstStmts is a step editing the text before it as dst statements.
//
// The step returns the errors of [dstfragment.ParseDstStmts] and
// [dstfragment.PrintDstStmts], and the error of f as it is.
func DstStmts[C genhelper.Context[C]](
	f func(c C, stmts []dst.Stmt) (C, []dst.Stmt, error),
) genhelper.Step[C] {
	return step(dstfragment.ParseDstStmts, f, dstfragment.PrintDstStmts)
}

// DstDecls is a step editing the text before it as dst declarations.
//
// The step returns the errors of [dstfragment.ParseDstDecls] and
// [dstfragment.PrintDstDecls], and the error of f as it is.
func DstDecls[C genhelper.Context[C]](
	f func(c C, decls []dst.Decl) (C, []dst.Decl, error),
) genhelper.Step[C] {
	return step(dstfragment.ParseDstDecls, f, dstfragment.PrintDstDecls)
}

// TypeDst returns the dst expression spelling t in the file of pkg. It
// returns the errors [genhelper.Package.Type] returns.
func TypeDst(pkg *genhelper.Package, t types.Type) (dst.Expr, error) {
	// typeprint dereferences the map for every package other than Path, and
	// a zero map reports each of them as unknown as a nil Imports does.
	m := pkg.Imports
	if m == nil {
		m = &importdecl.Map{}
	}

	return dsttype.Dst(t, m, pkg.Path)
}

// ParseDecls parses src into declarations, each after an empty line.
//
// ParseDecls returns the errors [dstfragment.ParseDstDecls] returns as they
// are.
func ParseDecls(src []byte) ([]dst.Decl, error) {
	decls, err := dstfragment.ParseDstDecls(src)
	if err != nil {
		return nil, err
	}

	for _, d := range decls {
		d.Decorations().Before = dst.EmptyLine
	}

	return decls, nil
}

// AppendDecls parses decls and appends them to the existing file f, each
// after an empty line, then adds the imports pkg.Imports records as
// missing. A nil pkg or a nil pkg.Imports adds no import.
//
// AppendDecls returns the errors [dstfragment.ParseDstDecls] returns, such
// as one wrapping gofragment.ErrNotPartial for an import declaration, and
// leaves f unchanged then.
func AppendDecls(f *dst.File, pkg *genhelper.Package, decls []byte) error {
	parsed, err := ParseDecls(decls)
	if err != nil {
		return err
	}

	f.Decls = append(f.Decls, parsed...)
	if pkg != nil && pkg.Imports != nil {
		dstimport.AddMissing(pkg.Imports, f)
	}

	return nil
}

// step builds a step parsing the text into nodes, calling f on them and
// printing what f returns. The Parse and Print functions of dstfragment
// wrap their errors with context, and the error of f is returned as it is.
func step[C genhelper.Context[C], N any](
	parse func(b []byte) (N, error),
	f func(c C, n N) (C, N, error),
	printN func(n N) ([]byte, error),
) genhelper.Step[C] {
	return genhelper.FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		n, err := parse(b)
		if err != nil {
			return c, nil, err
		}

		c, n, err = f(c, n)
		if err != nil {
			return c, nil, err
		}

		out, err := printN(n)
		if err != nil {
			return c, nil, err
		}

		return c, out, nil
	})
}
