package genopt

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// line is a directive comment of the tool, attached to its target.
type line struct {
	target Target
	// directive is the directive without the tool name, such as "partial"
	// or "partial:merge" for //kama:partial:merge.
	directive string
	args      string
	pos       token.Position
}

// walked holds the directive comments of a package in position order.
type walked struct {
	pkg   *packages.Package
	lines []line
}

// walk returns the directive comments of toolName in pkgs, ordered by file
// name and then by position within each package.
//
// pkgs must be loaded with packages.NeedName, packages.NeedCompiledGoFiles
// and packages.NeedSyntax; packages parses no file without
// NeedCompiledGoFiles.
//
// A directive applies to:
//   - a type, in the doc comment of a top-level type spec, or of its type
//     declaration when the declaration has a single spec.
//   - a type group, in the doc comment of a type declaration with several
//     specs.
//   - a field, in the doc comment or the line comment of a field of a
//     top-level struct type, parenthesized or not. A field with several
//     names gives one line per name, and an embedded field is named after
//     its type.
//   - a file, in a top-level comment group that is not the doc or line
//     comment of any declaration or spec. The package clause doc comment
//     is such a group too: a package doc usually lives in a single file,
//     so it marks that file like any other free-floating comment. A group
//     starting after a top-level declaration on the line the declaration
//     ends on is the line comment of the declaration, for every kind of
//     declaration, and marks no file.
//
// Directives anywhere else, such as in a function body, in a nested struct
// or on a spec of a grouped declaration other than a type spec, are
// ignored.
func walk(pkgs []*packages.Package, toolName string) ([]walked, error) {
	out := make([]walked, 0, len(pkgs))

	for _, pkg := range pkgs {
		if pkg.Fset == nil || len(pkg.Syntax) == 0 {
			return nil, fmt.Errorf("genopt: package %q is loaded without syntax", pkg.PkgPath)
		}

		c := collector{fset: pkg.Fset, pkgPath: pkg.PkgPath, toolName: toolName}
		for _, f := range pkg.Syntax {
			c.file(f)
		}

		slices.SortStableFunc(c.out, func(a, b line) int {
			return cmp.Or(
				cmp.Compare(a.pos.Filename, b.pos.Filename),
				cmp.Compare(a.pos.Offset, b.pos.Offset),
			)
		})
		out = append(out, walked{pkg: pkg, lines: c.out})
	}

	return out, nil
}

type collector struct {
	fset     *token.FileSet
	pkgPath  string
	toolName string
	out      []line
}

func (c *collector) file(f *ast.File) {
	attached := map[*ast.CommentGroup]bool{}
	filename := c.fset.Position(f.FileStart).Filename

	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			attached[decl.Doc] = true
		case *ast.GenDecl:
			attached[decl.Doc] = true
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.ImportSpec:
					attached[spec.Doc], attached[spec.Comment] = true, true
				case *ast.ValueSpec:
					attached[spec.Doc], attached[spec.Comment] = true, true
				case *ast.TypeSpec:
					attached[spec.Doc], attached[spec.Comment] = true, true
				}
			}

			if decl.Tok == token.TYPE {
				c.typeDecl(filename, decl)
			}
		}
	}

	fileTarget := Target{
		Kind:    TargetFreeFloating,
		PkgPath: c.pkgPath,
		File:    filename,
		Node:    f,
	}
	for _, cg := range f.Comments {
		if attached[cg] || insideDecl(f.Decls, cg) || c.declLineComment(f.Decls, cg) {
			continue
		}

		c.add(fileTarget, cg.List)
	}
}

func (c *collector) typeDecl(filename string, decl *ast.GenDecl) {
	if len(decl.Specs) > 1 && decl.Doc != nil {
		c.add(Target{
			Kind:    TargetTypeGroup,
			PkgPath: c.pkgPath,
			File:    filename,
			Node:    decl,
		}, decl.Doc.List)
	}

	for _, spec := range decl.Specs {
		ts := spec.(*ast.TypeSpec)

		target := Target{
			Kind:    TargetType,
			PkgPath: c.pkgPath,
			File:    filename,
			Type:    ts.Name.Name,
			Node:    ts,
		}
		if len(decl.Specs) == 1 && decl.Doc != nil {
			c.add(target, decl.Doc.List)
		}

		if ts.Doc != nil {
			c.add(target, ts.Doc.List)
		}

		st, ok := unparen(ts.Type).(*ast.StructType)
		if !ok {
			continue
		}

		for _, fl := range st.Fields.List {
			comments := fieldComments(fl)
			for _, name := range fieldNames(fl) {
				c.add(Target{
					Kind:    TargetField,
					PkgPath: c.pkgPath,
					File:    filename,
					Type:    ts.Name.Name,
					Field:   name,
					Node:    fl,
				}, comments)
			}
		}
	}
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}

		expr = p.X
	}
}

func (c *collector) add(target Target, comments []*ast.Comment) {
	toLine := func(cm *ast.Comment) (line, bool) {
		d, ok := dircmnt.Parse(cm.Text)
		if !ok || d.ToolName != c.toolName {
			return line{}, false
		}

		return line{
			target:    target,
			directive: d.Directive,
			args:      d.Args,
			pos:       c.fset.Position(cm.Slash),
		}, true
	}

	c.out = opiter.WrapSeq(slices.Values(comments)).
		Disassemble(toLine).
		Filter(func(_ line, ok bool) bool { return ok }).
		OmitL().
		AppendSeq(c.out)
}

// fieldComments returns the comments of f that may hold its directives:
// its doc comment followed by the first comment of its line comment.
func fieldComments(f *ast.Field) []*ast.Comment {
	var out []*ast.Comment
	if f.Doc != nil {
		out = append(out, f.Doc.List...)
	}

	if f.Comment != nil && len(f.Comment.List) > 0 {
		out = append(out, f.Comment.List[0])
	}

	return out
}

func fieldNames(f *ast.Field) []string {
	if len(f.Names) > 0 {
		return opiter.WrapSeq(slices.Values(f.Names)).
			Map((*ast.Ident).String).
			CollectSlice()
	}

	return []string{embeddedName(f.Type)}
}

func embeddedName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return embeddedName(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.IndexExpr:
		return embeddedName(e.X)
	case *ast.IndexListExpr:
		return embeddedName(e.X)
	case *ast.Ident:
		return e.Name
	}

	return ""
}

func insideDecl(decls []ast.Decl, cg *ast.CommentGroup) bool {
	_, found := slices.BinarySearchFunc(decls, cg.Pos(), func(d ast.Decl, p token.Pos) int {
		switch {
		case d.End() <= p:
			return -1
		case p < d.Pos():
			return 1
		}

		return 0
	})

	return found
}

// declLineComment reports whether cg starts after a top-level declaration
// on the line the declaration ends on.
//
// go/ast keeps such a group as the Comment of the last spec of a GenDecl
// without parentheses, but FuncDecl has no Comment field and a grouped
// GenDecl keeps none for its closing parenthesis. Checking the line for
// every kind of declaration keeps them all out of the file scope alike.
func (c *collector) declLineComment(decls []ast.Decl, cg *ast.CommentGroup) bool {
	// i is the number of declarations ending at or before cg.
	i, _ := slices.BinarySearchFunc(decls, cg.Pos(), func(d ast.Decl, p token.Pos) int {
		if d.End() <= p {
			return -1
		}

		return 1
	})
	if i == 0 {
		return false
	}

	return c.fset.Position(decls[i-1].End()).Line == c.fset.Position(cg.Pos()).Line
}
