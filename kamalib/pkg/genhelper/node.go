package genhelper

import (
	"go/ast"
	"go/token"

	"github.com/dave/dst"
)

// The text a chain carries has no kind of its own. The function converting
// it names the kind: an expression, a list of statements or a list of
// declarations.

// ParseDstExpr parses b as one expression.
func ParseDstExpr(b []byte) (dst.Expr, error) { panic("unimplemented") }

// ParseDstStmts parses b as the statements of a function body.
func ParseDstStmts(b []byte) ([]dst.Stmt, error) { panic("unimplemented") }

// ParseDstDecls parses b as top-level declarations.
func ParseDstDecls(b []byte) ([]dst.Decl, error) { panic("unimplemented") }

// PrintDstExpr prints e as partial text.
func PrintDstExpr(e dst.Expr) ([]byte, error) { panic("unimplemented") }

// PrintDstStmts prints stmts as partial text, one statement per line.
func PrintDstStmts(stmts []dst.Stmt) ([]byte, error) { panic("unimplemented") }

// PrintDstDecls prints decls as partial text.
func PrintDstDecls(decls []dst.Decl) ([]byte, error) { panic("unimplemented") }

// ParseAstExpr parses b as one expression. Positions refer to fset.
func ParseAstExpr(fset *token.FileSet, b []byte) (ast.Expr, error) { panic("unimplemented") }

// ParseAstStmts parses b as the statements of a function body.
func ParseAstStmts(fset *token.FileSet, b []byte) ([]ast.Stmt, error) { panic("unimplemented") }

// ParseAstDecls parses b as top-level declarations.
func ParseAstDecls(fset *token.FileSet, b []byte) ([]ast.Decl, error) { panic("unimplemented") }

// PrintAst prints n, an expression, a statement or a declaration, as
// partial text.
func PrintAst(fset *token.FileSet, n ast.Node) ([]byte, error) { panic("unimplemented") }

// DstExpr is a step editing the text before it as one dst expression. The
// step parses the text, calls f and prints the expression f returns.
func DstExpr[C Context[C]](f func(c C, e dst.Expr) (C, dst.Expr, error)) Step[C] {
	panic("unimplemented")
}

// DstStmts is a step editing the text before it as dst statements.
func DstStmts[C Context[C]](f func(c C, stmts []dst.Stmt) (C, []dst.Stmt, error)) Step[C] {
	panic("unimplemented")
}

// DstDecls is a step editing the text before it as dst declarations.
func DstDecls[C Context[C]](f func(c C, decls []dst.Decl) (C, []dst.Decl, error)) Step[C] {
	panic("unimplemented")
}

// AstExpr is a step editing the text before it as one ast expression.
func AstExpr[C Context[C]](
	f func(c C, fset *token.FileSet, e ast.Expr) (C, ast.Expr, error),
) Step[C] {
	panic("unimplemented")
}

// AstStmts is a step editing the text before it as ast statements.
func AstStmts[C Context[C]](
	f func(c C, fset *token.FileSet, stmts []ast.Stmt) (C, []ast.Stmt, error),
) Step[C] {
	panic("unimplemented")
}

// AstDecls is a step editing the text before it as ast declarations.
func AstDecls[C Context[C]](
	f func(c C, fset *token.FileSet, decls []ast.Decl) (C, []ast.Decl, error),
) Step[C] {
	panic("unimplemented")
}
