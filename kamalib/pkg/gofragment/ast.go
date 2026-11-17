package gofragment

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
)

func parseAstWrapped(fset *token.FileSet, k partialKind, b []byte) (*ast.File, error) {
	if fset == nil {
		return nil, fmt.Errorf("gofragment: parse %s: %w", k, ErrNoFileSet)
	}

	return parseWrapped(fset, k, b)
}

// ParseAstExpr parses b as one expression or type expression. Positions
// refer to the file wrapping b, which ParseAstExpr adds to fset.
//
// ParseAstExpr returns an error wrapping [ErrNoFileSet] when fset is nil
// and the errors [ParseDstExpr] returns.
func ParseAstExpr(fset *token.FileSet, b []byte) (ast.Expr, error) {
	f, err := parseAstWrapped(fset, exprKind, b)
	if err != nil {
		return nil, err
	}

	return f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Values[0], nil
}

// ParseAstStmts parses b as the statements of a function body. Positions
// refer to the file wrapping b, which ParseAstStmts adds to fset. Empty
// text gives an empty slice.
//
// ParseAstStmts returns an error wrapping [ErrNoFileSet] when fset is nil
// and the errors [ParseDstStmts] returns.
func ParseAstStmts(fset *token.FileSet, b []byte) ([]ast.Stmt, error) {
	f, err := parseAstWrapped(fset, stmtsKind, b)
	if err != nil {
		return nil, err
	}

	stmts := f.Decls[0].(*ast.FuncDecl).Body.List
	if stmts == nil {
		stmts = []ast.Stmt{}
	}

	return stmts, nil
}

// ParseAstDecls parses b as top-level declarations. Positions refer to the
// file wrapping b, which ParseAstDecls adds to fset. Empty text gives an
// empty slice.
//
// ParseAstDecls returns an error wrapping [ErrNoFileSet] when fset is nil
// and the errors [ParseDstDecls] returns.
func ParseAstDecls(fset *token.FileSet, b []byte) ([]ast.Decl, error) {
	f, err := parseAstWrapped(fset, declsKind, b)
	if err != nil {
		return nil, err
	}

	decls := f.Decls
	if decls == nil {
		decls = []ast.Decl{}
	}

	return decls, nil
}

// PrintAst prints n, an expression, a statement or a declaration, as
// partial text. A statement or a declaration ends with a newline.
// [PrintAstStmts] and [PrintAstDecls] print lists.
//
// PrintAst returns an error wrapping [ErrNoFileSet] when fset is nil.
func PrintAst(fset *token.FileSet, n ast.Node) ([]byte, error) {
	if fset == nil {
		return nil, fmt.Errorf("gofragment: print: %w", ErrNoFileSet)
	}

	switch n := n.(type) {
	case ast.Expr:
		var buf bytes.Buffer
		if err := printConfig(0).Fprint(&buf, fset, n); err != nil {
			return nil, fmt.Errorf("gofragment: print expression: %w", err)
		}

		return buf.Bytes(), nil
	case ast.Stmt:
		return PrintAstStmts(fset, []ast.Stmt{n})
	case ast.Decl:
		return PrintAstDecls(fset, []ast.Decl{n})
	default:
		return nil, fmt.Errorf("gofragment: print: unsupported node %T", n)
	}
}

// PrintAstStmts prints stmts as partial text, one statement per line. The
// text ends with a newline unless stmts is empty.
//
// PrintAstStmts returns an error wrapping [ErrNoFileSet] when fset is nil.
func PrintAstStmts(fset *token.FileSet, stmts []ast.Stmt) ([]byte, error) {
	if fset == nil {
		return nil, fmt.Errorf("gofragment: print statements: %w", ErrNoFileSet)
	}

	if len(stmts) == 0 {
		return []byte{}, nil
	}

	b, err := printBlock(fset, &ast.BlockStmt{List: stmts}, nil)
	if err != nil {
		return nil, fmt.Errorf("gofragment: print statements: %w", err)
	}

	return b, nil
}

// PrintAstDecls prints decls as partial text. The text ends with a newline
// unless decls is empty.
//
// PrintAstDecls returns an error wrapping [ErrNoFileSet] when fset is nil.
func PrintAstDecls(fset *token.FileSet, decls []ast.Decl) ([]byte, error) {
	if fset == nil {
		return nil, fmt.Errorf("gofragment: print declarations: %w", ErrNoFileSet)
	}

	if len(decls) == 0 {
		return []byte{}, nil
	}

	var buf bytes.Buffer
	if err := printConfig(0).Fprint(&buf, fset, decls); err != nil {
		return nil, fmt.Errorf("gofragment: print declarations: %w", err)
	}

	return trimLines(buf.Bytes()), nil
}
