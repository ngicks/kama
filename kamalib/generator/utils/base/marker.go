package base

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"slices"
	"strings"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// Generated reports whether the declaration of obj, declared in a package
// of pkgs, carries the marker of a generator reg registers.
func Generated(pkgs []*packages.Package, reg *genmark.Registry, obj types.Object) bool {
	pkg := DeclaringPackage(pkgs, obj)
	if pkg == nil {
		return false
	}

	return slices.ContainsFunc(DeclDocs(pkg, obj), func(doc *ast.CommentGroup) bool {
		_, ok := reg.Generated(doc)
		return ok
	})
}

// ExplainConflict returns err, an error reporting that a declaration
// stands where the generator writes one, with the reason added when one of
// docs, the doc comments of that declaration, carries a generated mark
// naming no generator reg registers. Such a mark makes the declaration
// look written by hand. A file generated before the marks took the form
// //kama:generated <namespace>:<name>, as with //kama:generated
// kama-partial, carries one, and the added reason tells to delete the
// file. Otherwise ExplainConflict returns err as it is.
func ExplainConflict(err error, reg *genmark.Registry, docs ...*ast.CommentGroup) error {
	for _, doc := range docs {
		if name, ok := reg.Unregistered(doc); ok {
			return fmt.Errorf("%w; %s", err, unknownMark(reg, name))
		}
	}

	return err
}

// unknownMark explains the generated mark naming name, which reg does not
// register.
func unknownMark(reg *genmark.Registry, name string) string {
	marker := genmark.Marker(ToolName, name)

	if gen, ok := strings.CutPrefix(name, ToolName+"-"); ok {
		renamed := Id{Namespace: ToolName, Name: gen}
		if reg.Registered(renamed.String()) {
			return fmt.Sprintf(
				"it carries %s, generated before the mark was renamed to %s;"+
					" delete the file holding it and run again",
				marker, renamed.Marker(),
			)
		}
	}

	return fmt.Sprintf(
		"it carries %s, which names no registered generator;"+
			" delete the file holding it if a generator no longer in use wrote it, and run again",
		marker,
	)
}

// fileDocs returns the doc comments of every top-level declaration of f
// and of their specs.
func fileDocs(f *ast.File) []*ast.CommentGroup {
	return opiter.SlicesValues(f.Decls).
		FlatMap(func(d ast.Decl) iter.Seq[*ast.CommentGroup] { return slices.Values(AstDeclDocs(d)) }).
		CollectSlice()
}

// AstDeclDocs returns the doc comments of d, a top-level declaration: the
// doc of d and the docs of its specs.
func AstDeclDocs(d ast.Decl) []*ast.CommentGroup {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return []*ast.CommentGroup{d.Doc}
	case *ast.GenDecl:
		docs := []*ast.CommentGroup{d.Doc}

		for _, spec := range d.Specs {
			switch sp := spec.(type) {
			case *ast.TypeSpec:
				docs = append(docs, sp.Doc)
			case *ast.ValueSpec:
				docs = append(docs, sp.Doc)
			}
		}

		return docs
	}

	return nil
}

// UnownedDecls returns the package-level objects of pkg named by names
// whose declarations carry no marker of a generator reg registers, in the
// order of names. A generator reports them as conflicts with the
// declarations it writes under those names.
func UnownedDecls(pkg *packages.Package, reg *genmark.Registry, names []string) []types.Object {
	return opiter.SlicesValues(names).
		Map(pkg.Types.Scope().Lookup).
		Filter(func(obj types.Object) bool {
			return obj != nil && !Generated([]*packages.Package{pkg}, reg, obj)
		}).
		CollectSlice()
}

// DeclDocs returns the doc comments of the declaration of obj, a type, a
// function, a method, a variable or a constant declared in pkg at the top
// level: the doc of the declaration and the doc of its spec. It returns nil
// for an object pkg does not declare at the top level.
func DeclDocs(pkg *packages.Package, obj types.Object) []*ast.CommentGroup {
	pos := obj.Pos()

	for _, f := range pkg.Syntax {
		if pos < f.FileStart || pos >= f.FileEnd {
			continue
		}

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.Pos() == pos {
					return []*ast.CommentGroup{d.Doc}
				}
			case *ast.GenDecl:
				if docs := genDeclDocs(d, pos); docs != nil {
					return docs
				}
			}
		}
	}

	return nil
}

// genDeclDocs returns the doc comments of the spec of d declaring the name
// at pos, or nil.
func genDeclDocs(d *ast.GenDecl, pos token.Pos) []*ast.CommentGroup {
	for _, spec := range d.Specs {
		switch sp := spec.(type) {
		case *ast.TypeSpec:
			if sp.Name.Pos() == pos {
				return []*ast.CommentGroup{d.Doc, sp.Doc}
			}
		case *ast.ValueSpec:
			declares := func(name *ast.Ident) bool { return name.Pos() == pos }
			if slices.ContainsFunc(sp.Names, declares) {
				return []*ast.CommentGroup{d.Doc, sp.Doc}
			}
		}
	}

	return nil
}
