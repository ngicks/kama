package base

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// Generated reports whether the declaration of obj, declared in a package
// of pkgs, carries the marker of a generator reg registers.
func Generated(pkgs []*packages.Package, reg *genmark.Registry, obj types.Object) bool {
	if obj.Pkg() == nil {
		return false
	}

	for _, pkg := range pkgs {
		if pkg.Types != obj.Pkg() {
			continue
		}

		return slices.ContainsFunc(DeclDocs(pkg, obj), func(doc *ast.CommentGroup) bool {
			_, ok := reg.Generated(doc)
			return ok
		})
	}

	return false
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
