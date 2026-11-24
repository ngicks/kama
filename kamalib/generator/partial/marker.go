package partial

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
)

// generated reports whether the declaration of obj, declared in a loaded
// package, carries the marker of a registered generator. Such a
// declaration is dropped and generated again.
func (s *selection) generated(obj types.Object) bool {
	return base.Generated(s.pkgs, s.opts.Registry, obj)
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

	// A marked variable or constant of that name is no partial variant.
	partial, ok := named.Obj().Pkg().Scope().Lookup(partialName(named.Obj().Name())).(*types.TypeName)
	if !ok {
		return nil, false
	}

	if base.DeclaringPackage(s.pkgs, named.Obj()) != nil {
		return nil, false
	}

	var found bool

	packages.Visit(s.pkgs, nil, func(pkg *packages.Package) {
		if found || pkg.Types != partial.Pkg() {
			return
		}

		found = slices.ContainsFunc(base.DeclDocs(pkg, partial), func(doc *ast.CommentGroup) bool {
			name, ok := s.opts.Registry.Generated(doc)
			return ok && name == GeneratorName
		})
	})

	return named, found
}
