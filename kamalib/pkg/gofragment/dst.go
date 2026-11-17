package gofragment

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"slices"

	"github.com/ngicks/opiter"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

func parseDstWrapped(k partialKind, b []byte) (*dst.File, error) {
	fset := token.NewFileSet()

	f, err := parseWrapped(fset, k, b)
	if err != nil {
		return nil, err
	}

	df, err := decorator.NewDecorator(fset).DecorateFile(f)
	if err != nil {
		return nil, fmt.Errorf("gofragment: parse %s: %w", k, err)
	}

	return df, nil
}

// ParseDstExpr parses b as one expression or type expression.
//
// ParseDstExpr returns an error wrapping [ErrNotPartial] when b holds more
// than one expression, such as "a, b" or "a; var x = 1". Empty text is a
// syntax error.
func ParseDstExpr(b []byte) (dst.Expr, error) {
	f, err := parseDstWrapped(exprKind, b)
	if err != nil {
		return nil, err
	}

	return f.Decls[0].(*dst.GenDecl).Specs[0].(*dst.ValueSpec).Values[0], nil
}

// ParseDstStmts parses b as the statements of a function body. Empty text
// gives an empty slice.
//
// ParseDstStmts returns an error wrapping [ErrNotPartial] when b closes the
// function body it is parsed in.
func ParseDstStmts(b []byte) ([]dst.Stmt, error) {
	f, err := parseDstWrapped(stmtsKind, b)
	if err != nil {
		return nil, err
	}

	stmts := f.Decls[0].(*dst.FuncDecl).Body.List
	if stmts == nil {
		stmts = []dst.Stmt{}
	}

	return stmts, nil
}

// ParseDstDecls parses b as top-level declarations. Empty text gives an
// empty slice.
//
// ParseDstDecls returns an error wrapping [ErrNotPartial] when b holds an
// import declaration. The caller adds imports to the file itself.
func ParseDstDecls(b []byte) ([]dst.Decl, error) {
	f, err := parseDstWrapped(declsKind, b)
	if err != nil {
		return nil, err
	}

	decls := f.Decls
	if decls == nil {
		decls = []dst.Decl{}
	}

	return decls, nil
}

// checkNoPath rejects identifiers carrying a package path. The restorer
// without import management panics on them.
func checkNoPath(n dst.Node) error {
	var found *dst.Ident

	dst.Inspect(n, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok && id.Path != "" {
			found = id
		}

		return found == nil
	})

	if found != nil {
		return fmt.Errorf("identifier %s carries the package path %q", found.Name, found.Path)
	}

	return nil
}

func restore(f *dst.File) (*ast.File, *token.FileSet, error) {
	if err := checkNoPath(f); err != nil {
		return nil, nil, err
	}

	r := decorator.NewRestorer()

	af, err := r.RestoreFile(f)
	if err != nil {
		return nil, nil, err
	}

	return af, r.Fset, nil
}

// PrintDstExpr prints e as partial text with no trailing newline.
//
// The comments inside e are printed. The comments decorating e itself,
// before its start or after its end, are dropped.
//
// PrintDstExpr returns an error when an identifier of e carries a package
// path, since import management is left to the caller.
func PrintDstExpr(e dst.Expr) ([]byte, error) {
	if e == nil {
		return nil, errors.New("gofragment: print expression: nil expression")
	}
	// A dst node belongs to one tree. Placing the clone in the wrapper keeps
	// e where the caller put it.
	f := &dst.File{
		Name: dst.NewIdent(wrapperPkg),
		Decls: []dst.Decl{&dst.GenDecl{
			Tok: token.VAR,
			Specs: []dst.Spec{&dst.ValueSpec{
				Names:  []*dst.Ident{dst.NewIdent("_")},
				Values: []dst.Expr{dst.Clone(e).(dst.Expr)},
			}},
		}},
	}

	af, fset, err := restore(f)
	if err != nil {
		return nil, fmt.Errorf("gofragment: print expression: %w", err)
	}

	ae := af.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Values[0]

	var buf bytes.Buffer

	err = printConfig(0).Fprint(&buf, fset, &printer.CommentedNode{Node: ae, Comments: af.Comments})
	if err != nil {
		return nil, fmt.Errorf("gofragment: print expression: %w", err)
	}

	return buf.Bytes(), nil
}

// PrintDstStmts prints stmts as partial text, one statement per line. The
// text ends with a newline unless stmts is empty.
//
// PrintDstStmts returns an error when an identifier of stmts carries a
// package path, since import management is left to the caller.
func PrintDstStmts(stmts []dst.Stmt) ([]byte, error) {
	if len(stmts) == 0 {
		return []byte{}, nil
	}
	// A dst node belongs to one tree. Placing the clones in the wrapper
	// keeps stmts where the caller put them.
	body := dst.Clone(&dst.BlockStmt{List: stmts}).(*dst.BlockStmt)
	// A comment leading the first statement would otherwise follow "{" on
	// its line.
	body.List[0].Decorations().Before = dst.NewLine
	f := &dst.File{
		Name: dst.NewIdent(wrapperPkg),
		Decls: []dst.Decl{&dst.FuncDecl{
			Name: dst.NewIdent("_"),
			Type: &dst.FuncType{},
			Body: body,
		}},
	}

	af, fset, err := restore(f)
	if err != nil {
		return nil, fmt.Errorf("gofragment: print statements: %w", err)
	}

	b, err := printBlock(fset, af.Decls[0].(*ast.FuncDecl).Body, af.Comments)
	if err != nil {
		return nil, fmt.Errorf("gofragment: print statements: %w", err)
	}

	return b, nil
}

// PrintDstDecls prints decls as partial text. The text ends with a newline
// unless decls is empty.
//
// PrintDstDecls returns an error when an identifier of decls carries a
// package path, since import management is left to the caller.
func PrintDstDecls(decls []dst.Decl) ([]byte, error) {
	if len(decls) == 0 {
		return []byte{}, nil
	}

	// A dst node belongs to one tree. Placing the clones in the wrapper
	// keeps decls where the caller put them.
	cloned := opiter.WrapSeq(
		slices.Values(decls),
	).
		Map(
			func(d dst.Decl) dst.Decl { return dst.Clone(d).(dst.Decl) },
		).
		Collect()

	// A comment leading the first declaration would otherwise follow the
	// package clause on its line.
	cloned[0].Decorations().Before = dst.EmptyLine

	af, fset, err := restore(&dst.File{Name: dst.NewIdent(wrapperPkg), Decls: cloned})
	if err != nil {
		return nil, fmt.Errorf("gofragment: print declarations: %w", err)
	}
	// The printer cannot print comments with a declaration list that is not
	// a node. The whole file carries them, and only its package clause is
	// cut off.
	var buf bytes.Buffer
	if err := printConfig(0).Fprint(&buf, fset, af); err != nil {
		return nil, fmt.Errorf("gofragment: print declarations: %w", err)
	}

	out := buf.Bytes()
	if !bytes.HasPrefix(out, []byte(printedFile)) {
		return nil, fmt.Errorf("gofragment: print declarations: unexpected file %q", out)
	}

	return trimLines(out[len(printedFile):]), nil
}
