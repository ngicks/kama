// Package kamaopt collects the raw options of a kama generator, keyed by
// the package, file, type or field they apply to.
//
// Options come from directive comments //tool:directive args in the source
// or from an option file using the same vocabulary. The package does not
// interpret directives or args; generators decode Raw values into their
// own options.
package kamaopt

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ngicks/opiter"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// ErrConflict reports options that cannot apply together.
var ErrConflict = errors.New("kamaopt: conflicting options")

// Source is where a Raw option comes from.
type Source int

const (
	SourceDirective Source = iota + 1
	SourceFile
)

// Origin locates a Raw option for error messages.
type Origin struct {
	Source Source
	// Pos is the position of the directive comment for SourceDirective.
	// For SourceFile only Pos.Filename, the option file name, is set.
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
	raws := opiter.WrapSeq(slices.Values(comments)).Disassemble(toRaw).Filter(isRaw).OmitL()
	c.out = slices.AppendSeq(c.out, raws.Iter())
}

func isRaw(_ Raw, ok bool) bool { return ok }

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
		return opiter.WrapSeq(slices.Values(f.Names)).Map((*ast.Ident).String).Collect()
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

// ParseArgs parses args as space-separated name=value pairs.
//
// The grammar is:
//
//	args  = { space } [ pair { space { space } pair } ] { space } .
//	pair  = name [ "=" value ] .
//	name  = non-empty run of characters other than space, `=` and `"` .
//	value = quoted | bare .
//	quoted = Go double-quoted string literal, unquoted with strconv.Unquote .
//	bare  = run of characters other than space and `"`, possibly empty .
//
// space is a character for which unicode.IsSpace reports true. A pair
// without "=" maps name to "", as for a flag word like "append". A name
// given twice is an error.
func ParseArgs(args string) (map[string]string, error) {
	out := map[string]string{}
	rest := args
	for {
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if rest == "" {
			return out, nil
		}

		i := strings.IndexFunc(rest, func(r rune) bool {
			return r == '=' || r == '"' || unicode.IsSpace(r)
		})
		if i < 0 {
			i = len(rest)
		}
		name := rest[:i]
		if name == "" {
			return nil, fmt.Errorf("kamaopt: missing name at %q", rest)
		}
		if _, ok := out[name]; ok {
			return nil, fmt.Errorf("kamaopt: duplicate name %q", name)
		}
		rest = rest[i:]

		switch {
		case rest == "" || startsWithSpace(rest):
			out[name] = ""
			continue
		case rest[0] == '"':
			return nil, fmt.Errorf("kamaopt: unexpected quote after name %q", name)
		}
		rest = rest[1:] // "="

		var (
			value string
			err   error
		)
		if strings.HasPrefix(rest, `"`) {
			value, rest, err = cutQuoted(rest)
			if err != nil {
				return nil, fmt.Errorf("kamaopt: value of %q: %w", name, err)
			}
			if rest != "" && !startsWithSpace(rest) {
				return nil, fmt.Errorf("kamaopt: value of %q: text after closing quote", name)
			}
		} else {
			end := strings.IndexFunc(rest, unicode.IsSpace)
			if end < 0 {
				end = len(rest)
			}
			value, rest = rest[:end], rest[end:]
			if strings.Contains(value, `"`) {
				return nil, fmt.Errorf("kamaopt: value of %q: quote inside unquoted value", name)
			}
		}
		out[name] = value
	}
}

func startsWithSpace(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

// cutQuoted unquotes the Go string literal at the start of s and returns
// the text after it.
func cutQuoted(s string) (value, rest string, err error) {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			value, err = strconv.Unquote(s[:i+1])
			return value, s[i+1:], err
		}
	}
	return "", "", errors.New("unterminated quoted string")
}
