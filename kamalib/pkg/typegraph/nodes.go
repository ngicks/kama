package typegraph

import (
	"cmp"
	"go/types"
	"iter"
	"slices"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
)

// Filename returns the name of the file declaring n.
func (n *Node) Filename() string {
	return n.Detail.Pkg.Fset.Position(n.Detail.File.FileStart).Filename
}

// SortedNodes returns the nodes of seq ordered by Id.
func SortedNodes(seq iter.Seq[*Node]) []*Node {
	return slices.SortedFunc(seq, func(a, b *Node) int { return a.Id.Compare(b.Id) })
}

// NodesIn returns the nodes of seq the file at filename declares, in the
// order of their declarations.
func NodesIn(seq iter.Seq[*Node], filename string) []*Node {
	byPosition := func(seq iter.Seq[*Node]) []*Node {
		return slices.SortedFunc(seq, func(a, b *Node) int {
			return cmp.Compare(a.Detail.Ts.Pos(), b.Detail.Ts.Pos())
		})
	}

	return opiter.WrapSeq(seq).
		Filter(func(n *Node) bool { return n.Filename() == filename }).
		Collect(byPosition)
}

// LookupType returns the package-level object named id.TypeName in the
// package of pkgs at id.PackagePath. It returns nil when pkgs has no such
// package or the package declares no such name.
func LookupType(pkgs []*packages.Package, id QualifiedType) types.Object {
	pkg := opiter.SlicesValues(pkgs).
		Filter(func(pkg *packages.Package) bool { return pkg.PkgPath == id.PackagePath }).
		First()

	if pkg.IsNone() {
		return nil
	}

	return pkg.Value().Types.Scope().Lookup(id.TypeName)
}
