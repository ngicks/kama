package partial

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
)

// implementors finds the types that already have a partial variant without
// being targets of the run: types of the packages the loaded packages
// import, directly or not, whose package declares their partial variant
// with the marker of this generator.
//
// The marker is what tells a partial variant. The methods of a partial
// variant sit on the variant, so nothing on the type itself tells that it
// has one, and a type named like a partial variant by coincidence must not
// count.
//
// A type of a loaded package is never an implementor: its marked
// declarations are dropped and generated again, and a type the run leaves
// out, as an ignored one, must not get a partial form from a stale file.
type implementors struct {
	reg *genmark.Registry
	// pkgs are the packages outside the run, keyed by path.
	pkgs map[string]*packages.Package
	// marked are the names of the types each package declares with the
	// marker of this generator, filled when the package is first asked for.
	marked map[string]map[string]bool
}

// newImplementors returns the implementors of the run generating into
// loaded.
func newImplementors(loaded []*packages.Package, reg *genmark.Registry) *implementors {
	im := &implementors{
		reg:    reg,
		pkgs:   map[string]*packages.Package{},
		marked: map[string]map[string]bool{},
	}

	run := map[string]bool{}
	for _, pkg := range loaded {
		run[pkg.PkgPath] = true
	}

	packages.Visit(loaded, nil, func(pkg *packages.Package) {
		if !run[pkg.PkgPath] {
			im.pkgs[pkg.PkgPath] = pkg
		}
	})

	return im
}

// has reports whether the type obj declares is an implementor.
func (im *implementors) has(obj *types.TypeName) bool {
	if im == nil || obj.Pkg() == nil {
		return false
	}

	pkg, ok := im.pkgs[obj.Pkg().Path()]
	if !ok {
		return false
	}

	marked, ok := im.marked[pkg.PkgPath]
	if !ok {
		marked = im.scan(pkg)
		im.marked[pkg.PkgPath] = marked
	}

	return marked[partialName(obj.Name())]
}

// scan returns the names of the types pkg declares with the marker of this
// generator.
func (im *implementors) scan(pkg *packages.Package) map[string]bool {
	out := map[string]bool{}
	own := func(doc *ast.CommentGroup) bool {
		name, ok := im.reg.Generated(doc)
		return ok && name == GeneratorName
	}

	for _, f := range pkg.Syntax {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}

			declMarked := own(gd.Doc)
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				if declMarked || own(ts.Doc) {
					out[ts.Name.Name] = true
				}
			}
		}
	}

	return out
}
