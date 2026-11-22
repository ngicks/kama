package partial

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"
)

// declDocs returns the doc comments of the declaration of obj, a type, a
// function or a method declared in pkg: the doc of the declaration and,
// for a type, the doc of its spec. It returns nil for an object pkg does
// not declare at the top level.
func declDocs(pkg *packages.Package, obj types.Object) []*ast.CommentGroup {
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
				if d.Tok != token.TYPE {
					continue
				}

				for _, spec := range d.Specs {
					if ts := spec.(*ast.TypeSpec); ts.Name.Pos() == pos {
						return []*ast.CommentGroup{d.Doc, ts.Doc}
					}
				}
			}
		}
	}

	return nil
}

// generated reports whether the declaration of obj, declared in a loaded
// package, carries the marker of a registered generator. Such a
// declaration is dropped and generated again.
func (s *selection) generated(obj types.Object) bool {
	if obj.Pkg() == nil {
		return false
	}

	for _, pkg := range s.pkgs {
		if pkg.Types != obj.Pkg() {
			continue
		}

		return slices.ContainsFunc(declDocs(pkg, obj), func(doc *ast.CommentGroup) bool {
			_, ok := s.opts.Registry.Generated(doc)
			return ok
		})
	}

	return false
}

// implementor reports the implementor named type t refers to, through
// aliases: a type that already has a partial variant without being a
// target of the run.
//
// An implementor is a type of a package the loaded packages import,
// directly or not, whose package declares its partial variant with the
// marker of this generator. The marker is what tells a partial variant.
// The methods of a partial variant sit on the variant, so nothing on the
// type itself tells that it has one, and a type named like a partial
// variant by coincidence must not count.
//
// A type of a loaded package is never an implementor: its marked
// declarations are dropped and generated again, and a type the run leaves
// out, as an ignored one, must not get a partial form from a stale file.
func (s *selection) implementor(t types.Type) (*types.Named, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return nil, false
	}

	partial := named.Obj().Pkg().Scope().Lookup(partialName(named.Obj().Name()))
	if partial == nil {
		return nil, false
	}

	isLoaded := func(pkg *packages.Package) bool { return pkg.Types == named.Obj().Pkg() }
	if slices.ContainsFunc(s.pkgs, isLoaded) {
		return nil, false
	}

	var found bool

	packages.Visit(s.pkgs, nil, func(pkg *packages.Package) {
		if found || pkg.Types != partial.Pkg() {
			return
		}

		found = slices.ContainsFunc(declDocs(pkg, partial), func(doc *ast.CommentGroup) bool {
			name, ok := s.opts.Registry.Generated(doc)
			return ok && name == GeneratorName
		})
	})

	return named, found
}
