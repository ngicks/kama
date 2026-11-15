package gofragment

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
)

// The expression wrapper places the text after "=" so that the parser
// accepts type expressions too: the parser checks syntax only, and a type is
// a valid operand there.
const (
	wrapperPkg   = "p"
	exprPrefix   = "package " + wrapperPkg + "\n\nvar _ = "
	stmtsPrefix  = "package " + wrapperPkg + "\n\nfunc _() {\n"
	stmtsSuffix  = "}\n"
	declsPrefix  = "package " + wrapperPkg + "\n\n"
	printedFile  = "package " + wrapperPkg + "\n"
	printedOpen  = "{\n"
	printedClose = "\n}"
)

type partialKind int

const (
	exprKind partialKind = iota
	stmtsKind
	declsKind
)

func (k partialKind) String() string {
	switch k {
	case exprKind:
		return "expression"
	case stmtsKind:
		return "statements"
	default:
		return "declarations"
	}
}

func (k partialKind) wrap(b []byte) []byte {
	var prefix, suffix string
	switch k {
	case exprKind:
		prefix = exprPrefix
	case stmtsKind:
		prefix, suffix = stmtsPrefix, stmtsSuffix
	default:
		prefix = declsPrefix
	}
	src := make([]byte, 0, len(prefix)+len(b)+1+len(suffix))
	src = append(src, prefix...)
	src = append(src, b...)
	// The newline ends a trailing line comment of b. Adding it only when b
	// lacks one keeps the last node of b from gaining an empty line after
	// it.
	if !bytes.HasSuffix(b, []byte("\n")) {
		src = append(src, '\n')
	}
	return append(src, suffix...)
}

// parseWrapped parses b wrapped for k into fset and checks the shape of
// the wrapper.
func parseWrapped(fset *token.FileSet, k partialKind, b []byte) (*ast.File, error) {
	src := k.wrap(b)
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("gofragment: parse %s: %w", k, err)
	}
	if err := checkShape(fset, k, f, src); err != nil {
		return nil, fmt.Errorf("gofragment: parse %s: %w", k, err)
	}
	return f, nil
}

func checkShape(fset *token.FileSet, k partialKind, f *ast.File, src []byte) error {
	switch k {
	case exprKind:
		if len(f.Decls) != 1 {
			return fmt.Errorf("%w: text adds %d declarations", ErrNotPartial, len(f.Decls)-1)
		}
		gen, ok := f.Decls[0].(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
			return fmt.Errorf("%w: text is not one expression", ErrNotPartial)
		}
		spec, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Type != nil || len(spec.Values) != 1 {
			return fmt.Errorf("%w: text is not one expression", ErrNotPartial)
		}
		return checkTrailing(fset, spec.Values[0].End(), src)
	case stmtsKind:
		if len(f.Decls) != 1 {
			return fmt.Errorf("%w: text adds %d declarations", ErrNotPartial, len(f.Decls)-1)
		}
		fn, ok := f.Decls[0].(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "_" || fn.Type.TypeParams != nil ||
			fn.Type.Params.NumFields() != 0 || fn.Type.Results != nil || fn.Body == nil {
			return fmt.Errorf("%w: text is not statements of a function body", ErrNotPartial)
		}
		return nil
	default:
		for _, d := range f.Decls {
			if gen, ok := d.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
				return fmt.Errorf(
					"%w: text declares imports, which the caller adds to the file",
					ErrNotPartial,
				)
			}
		}
		return nil
	}
}

// checkTrailing rejects tokens after the expression ending at end. The
// parser drops an explicit semicolon, so the shape of the file alone does
// not show text like "x;".
func checkTrailing(fset *token.FileSet, end token.Pos, src []byte) error {
	rest := src[fset.Position(end).Offset:]
	var s scanner.Scanner
	s.Init(token.NewFileSet().AddFile("", -1, len(rest)), rest, nil, 0)
	for {
		_, tok, lit := s.Scan()
		switch {
		case tok == token.EOF:
			return nil
		case tok == token.SEMICOLON && lit == "\n":
		default:
			return fmt.Errorf("%w: text continues after the expression with %s", ErrNotPartial, tok)
		}
	}
}

func printConfig(indent int) *printer.Config {
	return &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8, Indent: indent}
}

// printBlock prints the statements of block at indentation 0.
//
// The printer cannot print comments with a statement list that is not a
// node, and it indents a list holding a labeled statement. Printing the
// block at indentation -1 places its statements at 0, labels included, and
// leaves the content of every line as the printer wrote it. Only the brace
// lines are cut off.
func printBlock(
	fset *token.FileSet,
	block *ast.BlockStmt,
	comments []*ast.CommentGroup,
) ([]byte, error) {
	var node any = block
	if comments != nil {
		node = &printer.CommentedNode{Node: block, Comments: comments}
	}
	var buf bytes.Buffer
	if err := printConfig(-1).Fprint(&buf, fset, node); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) < len(printedOpen)+len(printedClose) ||
		!bytes.HasPrefix(out, []byte(printedOpen)) || !bytes.HasSuffix(out, []byte(printedClose)) {
		return nil, fmt.Errorf("unexpected block %q", out)
	}
	return trimLines(out[len(printedOpen) : len(out)-len(printedClose)]), nil
}

// trimLines trims the empty lines around b and ends it with one newline.
// Neither a raw string literal nor a comment starts or ends b, so the
// newlines trimmed are line breaks of the printer.
func trimLines(b []byte) []byte {
	b = bytes.Trim(b, "\n")
	if len(b) == 0 {
		return []byte{}
	}
	return append(b, '\n')
}
