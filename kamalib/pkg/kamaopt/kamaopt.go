// Package kamaopt collects the raw options of a code generator, keyed by
// the package, file, type or field they apply to.
//
// Options come from directive comments //tool:directive args in the source,
// from an option file using the same vocabulary, or from the option file
// of a single package. The tool-wide directive //tool:option-file names the
// option file of each generator for a package, and a package without it
// may hold a file of a default name; LoadPackageFiles finds and decodes
// those files. The package does not interpret directives, args or option
// file entries; generators decode Raw values and documents into their own
// options.
package kamaopt

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// ErrConflict reports options that cannot apply together.
var ErrConflict = errors.New("kamaopt: conflicting options")

// Source is where a Raw option comes from.
type Source int

const (
	// SourceDirective is a directive comment.
	SourceDirective Source = iota + 1
	// SourceFile is an option file holding options of any package.
	SourceFile
	// SourcePackageFile is an option file of a single package, as
	// LoadPackageFiles finds it.
	SourcePackageFile
)

// Origin locates a Raw option for error messages.
type Origin struct {
	Source Source
	// Pos is the position of the directive comment for SourceDirective.
	// For SourceFile and SourcePackageFile only Pos.Filename, the option
	// file name, is set.
	Pos token.Position
}

// Key is the scope a Raw option applies to.
//
// A package scope sets only PkgPath. A file scope sets PkgPath and File, the
// file path as packages reports it. A type scope sets PkgPath and Type, and a
// field scope also sets Field.
type Key struct {
	PkgPath string
	File    string
	Type    string
	Field   string
}

// Compare orders k and other by PkgPath, File, Type and Field. It returns
// -1, 0 or +1 as cmp.Compare does.
func (k Key) Compare(other Key) int {
	return cmp.Or(
		cmp.Compare(k.PkgPath, other.PkgPath),
		cmp.Compare(k.File, other.File),
		cmp.Compare(k.Type, other.Type),
		cmp.Compare(k.Field, other.Field),
	)
}

// Raw is an option before a generator decodes it.
type Raw struct {
	Key Key
	// Directive is the directive without the tool name, such as "partial"
	// or "partial:merge" for //kama:partial:merge.
	Directive string
	Args      string
	// Value is the value read from an option file. It is nil for
	// directives.
	Value  any
	Origin Origin
}

// Collect returns the directive comments of toolName in pkgs, ordered by
// package, then by file name, then by position.
//
// pkgs must be loaded with packages.NeedName, packages.NeedCompiledGoFiles
// and packages.NeedSyntax; packages parses no file without
// NeedCompiledGoFiles.
//
// A directive applies to:
//   - a type, in the doc comment of a top-level type spec, or of its type
//     declaration when the declaration has a single spec.
//   - a field, in the doc comment or the line comment of a field of a
//     top-level struct type, parenthesized or not. A field with several
//     names gives one Raw per name, and an embedded field is named after
//     its type.
//   - a file, in a top-level comment group that is not the doc or line
//     comment of any declaration or spec. The package clause doc comment
//     is such a group too: a package doc usually lives in a single file,
//     so it marks that file like any other free-floating comment. A group
//     starting after a top-level declaration on the line the declaration
//     ends on is the line comment of the declaration, for every kind of
//     declaration, and marks no file.
//
// Collect returns no package scope; a generator gives a file-scoped
// directive package meaning by its args.
//
// Directives anywhere else, such as in a function body, in a nested struct
// or on a spec of a grouped declaration other than a type spec, are
// ignored.
func Collect(pkgs []*packages.Package, toolName string) ([]Raw, error) {
	var out []Raw

	for _, pkg := range pkgs {
		if pkg.Fset == nil || len(pkg.Syntax) == 0 {
			return nil, fmt.Errorf("kamaopt: package %q is loaded without syntax", pkg.PkgPath)
		}

		c := collector{fset: pkg.Fset, pkgPath: pkg.PkgPath, toolName: toolName}
		for _, f := range pkg.Syntax {
			c.file(f)
		}

		slices.SortStableFunc(c.out, func(a, b Raw) int {
			return cmp.Or(
				cmp.Compare(a.Origin.Pos.Filename, b.Origin.Pos.Filename),
				cmp.Compare(a.Origin.Pos.Offset, b.Origin.Pos.Offset),
			)
		})
		out = append(out, c.out...)
	}

	return out, nil
}

type collector struct {
	fset     *token.FileSet
	pkgPath  string
	toolName string
	out      []Raw
}

func (c *collector) file(f *ast.File) {
	attached := map[*ast.CommentGroup]bool{}

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
				c.typeDecl(decl)
			}
		}
	}

	fileKey := Key{PkgPath: c.pkgPath, File: c.fset.Position(f.FileStart).Filename}
	for _, cg := range f.Comments {
		if attached[cg] || insideDecl(f.Decls, cg) || c.declLineComment(f.Decls, cg) {
			continue
		}

		c.add(fileKey, cg.List)
	}
}

func (c *collector) typeDecl(decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		ts := spec.(*ast.TypeSpec)

		key := Key{PkgPath: c.pkgPath, Type: ts.Name.Name}
		if len(decl.Specs) == 1 && decl.Doc != nil {
			c.add(key, decl.Doc.List)
		}

		if ts.Doc != nil {
			c.add(key, ts.Doc.List)
		}

		st, ok := unparen(ts.Type).(*ast.StructType)
		if !ok {
			continue
		}

		for _, fl := range st.Fields.List {
			comments := fieldComments(fl)
			for _, name := range fieldNames(fl) {
				c.add(Key{PkgPath: c.pkgPath, Type: ts.Name.Name, Field: name}, comments)
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

func (c *collector) add(key Key, comments []*ast.Comment) {
	toRaw := func(cm *ast.Comment) (Raw, bool) {
		d, ok := dircmnt.Parse(cm.Text)
		if !ok || d.ToolName != c.toolName {
			return Raw{}, false
		}

		return Raw{
			Key:       key,
			Directive: d.Directive,
			Args:      d.Args,
			Origin:    Origin{Source: SourceDirective, Pos: c.fset.Position(cm.Slash)},
		}, true
	}

	c.out = slices.AppendSeq(
		c.out,
		opiter.WrapSeq(slices.Values(comments)).
			Disassemble(toRaw).
			Filter(func(_ Raw, ok bool) bool { return ok }).
			OmitL().
			Iter(),
	)
}

// fieldComments selects the same comments as dircmnt.AstField, which
// returns no positions.
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

// ParseArgs parses args as space-separated name=value pairs, following the
// grammar of [dircmnt.ParseArgsKeyValue].
//
// A pair without "=" maps name to "", as for a flag word like "append". A
// name given twice is an error.
func ParseArgs(args string) (map[string]string, error) {
	kvs, err := dircmnt.ParseArgsKeyValue(args)
	if err != nil {
		return nil, fmt.Errorf("kamaopt: %w", err)
	}

	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		if _, ok := out[kv.Key]; ok {
			return nil, fmt.Errorf("kamaopt: duplicate name %q", kv.Key)
		}

		out[kv.Key] = kv.Value
	}

	return out, nil
}
