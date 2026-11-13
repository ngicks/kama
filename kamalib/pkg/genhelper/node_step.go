package genhelper

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/dave/dst"
)

// nodeStep builds the steps editing the text as nodes. The Parse and Print
// functions wrap their errors with context, and f's error is returned as
// it is.
func nodeStep[C Context[C], N any](
	parse func(c C, b []byte) (N, error),
	f func(c C, n N) (C, N, error),
	printN func(c C, n N) ([]byte, error),
) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		n, err := parse(c, b)
		if err != nil {
			return c, nil, err
		}
		c, n, err = f(c, n)
		if err != nil {
			return c, nil, err
		}
		out, err := printN(c, n)
		if err != nil {
			return c, nil, err
		}
		return c, out, nil
	})
}

func dstStep[C Context[C], N any](
	parse func(b []byte) (N, error),
	f func(c C, n N) (C, N, error),
	printN func(n N) ([]byte, error),
) Step[C] {
	return nodeStep(
		func(_ C, b []byte) (N, error) { return parse(b) },
		f,
		func(_ C, n N) ([]byte, error) { return printN(n) },
	)
}

func astStep[C Context[C], N any](
	parse func(fset *token.FileSet, b []byte) (N, error),
	f func(c C, n N) (C, N, error),
	printN func(fset *token.FileSet, n N) ([]byte, error),
) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		pkg := c.Package()
		if pkg == nil || pkg.Fset == nil {
			return c, nil, fmt.Errorf("%w: the context has no Package.Fset", ErrNoFileSet)
		}
		// The nodes hold positions of the file set they were parsed into,
		// so printing uses it too, whatever context f returns.
		fset := pkg.Fset
		return nodeStep(
			func(_ C, b []byte) (N, error) { return parse(fset, b) },
			f,
			func(_ C, n N) ([]byte, error) { return printN(fset, n) },
		).Render(c, b)
	})
}

// DstExpr is a step editing the text before it as one dst expression. The
// step parses the text, calls f and prints the expression f returns.
//
// The step returns the errors of [ParseDstExpr] and [PrintDstExpr], and
// the error of f as it is.
func DstExpr[C Context[C]](f func(c C, e dst.Expr) (C, dst.Expr, error)) Step[C] {
	return dstStep(ParseDstExpr, f, PrintDstExpr)
}

// DstStmts is a step editing the text before it as dst statements.
//
// The step returns the errors of [ParseDstStmts] and [PrintDstStmts], and
// the error of f as it is.
func DstStmts[C Context[C]](f func(c C, stmts []dst.Stmt) (C, []dst.Stmt, error)) Step[C] {
	return dstStep(ParseDstStmts, f, PrintDstStmts)
}

// DstDecls is a step editing the text before it as dst declarations.
//
// The step returns the errors of [ParseDstDecls] and [PrintDstDecls], and
// the error of f as it is.
func DstDecls[C Context[C]](f func(c C, decls []dst.Decl) (C, []dst.Decl, error)) Step[C] {
	return dstStep(ParseDstDecls, f, PrintDstDecls)
}

// AstExpr is a step editing the text before it as one ast expression.
// Positions refer to the Fset of [Context.Package].
//
// The step returns an error wrapping [ErrNoFileSet] when the context has
// no Package or no Fset, the errors of [ParseAstExpr] and [PrintAst], and
// the error of f as it is.
func AstExpr[C Context[C]](f func(c C, e ast.Expr) (C, ast.Expr, error)) Step[C] {
	return astStep(ParseAstExpr, f, func(fset *token.FileSet, e ast.Expr) ([]byte, error) {
		return PrintAst(fset, e)
	})
}

// AstStmts is a step editing the text before it as ast statements.
// Positions refer to the Fset of [Context.Package].
//
// The step returns an error wrapping [ErrNoFileSet] when the context has
// no Package or no Fset, the errors of [ParseAstStmts] and [PrintAstStmts], and
// the error of f as it is.
func AstStmts[C Context[C]](f func(c C, stmts []ast.Stmt) (C, []ast.Stmt, error)) Step[C] {
	return astStep(ParseAstStmts, f, PrintAstStmts)
}

// AstDecls is a step editing the text before it as ast declarations.
// Positions refer to the Fset of [Context.Package].
//
// The step returns an error wrapping [ErrNoFileSet] when the context has
// no Package or no Fset, the errors of [ParseAstDecls] and [PrintAstDecls], and
// the error of f as it is.
func AstDecls[C Context[C]](f func(c C, decls []ast.Decl) (C, []ast.Decl, error)) Step[C] {
	return astStep(ParseAstDecls, f, PrintAstDecls)
}
