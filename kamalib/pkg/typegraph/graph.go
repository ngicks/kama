// Package typegraph builds a dependency graph of the named types declared
// in Go packages.
//
// A Graph has a Node for each named type, excluding aliases, of the given
// packages. An Edge links a parent type to a named child type that its
// underlying type refers to, through struct fields, elements of slices,
// arrays, pointers and channels, map keys and values, or type arguments.
// Each EdgeRoute of an Edge records one such path.
//
// Graph.Trim selects the nodes of interest and collects every node that
// depends on them into a MatchedGraph.
//
// Lifted from github.com/ngicks/go-codegen/graft/pkg/typegraph.
package typegraph

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"maps"
	"slices"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
)

var (
	// ErrLoad is returned when a package has load errors.
	ErrLoad = errors.New("typegraph: package load error")
	// ErrNoTypesInfo is returned when a package is loaded without
	// packages.NeedTypes or packages.NeedTypesInfo.
	ErrNoTypesInfo = errors.New("typegraph: package has no type info")
)

type QualifiedType struct {
	PackagePath string
	TypeName    string
}

// String returns PackagePath and TypeName joined by a dot.
// A type without a package, such as error, keeps the leading dot.
func (q QualifiedType) String() string {
	return q.PackagePath + "." + q.TypeName
}

// Compare orders q and other by PackagePath and then TypeName. It returns
// -1, 0 or +1 as cmp.Compare does.
func (q QualifiedType) Compare(other QualifiedType) int {
	return cmp.Or(
		cmp.Compare(q.PackagePath, other.PackagePath),
		cmp.Compare(q.TypeName, other.TypeName),
	)
}

func NewQualifiedType(pkgPath, typeName string) QualifiedType {
	return QualifiedType{pkgPath, typeName}
}

// QualifiedTypeFromObject creates a QualifiedType from a types.Object.
func QualifiedTypeFromObject(obj types.Object) QualifiedType {
	var pkgPath string
	if obj.Pkg() != nil {
		pkgPath = obj.Pkg().Path()
	}

	return QualifiedType{
		PackagePath: pkgPath,
		TypeName:    obj.Name(),
	}
}

// Edge is bi-directional linkage between 2 nodes.
//
// Multiple linkages may be coalesced into single Edge;
// multiple fields of struct, key-value of map[K]V and/or type parameters may use target type.
type Edge[T Node | MatchedNode] struct {
	Parent *T
	// non-instantiated child node
	Child  *T
	Routes []*EdgeRoute
}

type TypeDetail struct {
	Pkg  *packages.Package
	File *ast.File
	// nth type spec in the file.
	Pos  int
	Ts   *ast.TypeSpec
	Type *types.Named
}

type Node struct {
	Id       QualifiedType
	Detail   *TypeDetail
	Parent   map[QualifiedType]*Edge[Node]
	Children map[QualifiedType]*Edge[Node]
}

func (n *Node) drawEdge(
	child *Node,
	routes ...*EdgeRoute,
) {
	e, ok := child.Parent[n.Id]
	if !ok {
		e = &Edge[Node]{
			Parent: n,
			Child:  child,
		}
		child.Parent[n.Id] = e
		n.Children[child.Id] = e
	}

	e.Routes = append(e.Routes, routes...)
}

type Graph struct {
	// all named types, excluding type alias
	all map[QualifiedType]*Node
}

// New creates a new Graph from the given packages.
// It enumerates all type declarations and builds edges between types.
//
// pkgs must be loaded with at least packages.NeedSyntax, packages.NeedTypes
// and packages.NeedTypesInfo.
//
// genDeclFilter filters which GenDecl to process (nil means all).
// typeSpecFilter filters which TypeSpec to include (nil means all).
func New(
	pkgs []*packages.Package,
	genDeclFilter func(*ast.GenDecl) (bool, error),
	typeSpecFilter func(*ast.TypeSpec, types.Object) (bool, error),
) (*Graph, error) {
	g := &Graph{
		all: make(map[QualifiedType]*Node),
	}

	if err := listTypes(g, pkgs, genDeclFilter, typeSpecFilter); err != nil {
		return nil, err
	}

	if err := buildEdges(g); err != nil {
		return nil, err
	}

	return g, nil
}

func loadError(pkg *packages.Package) error {
	if len(pkg.Errors) > 0 {
		errs := opiter.WrapSeq(slices.Values(pkg.Errors)).
			Map(func(e packages.Error) error { return e }).
			CollectSlice()

		return fmt.Errorf("%w: %s: %w", ErrLoad, pkg.PkgPath, errors.Join(errs...))
	}

	if pkg.Types == nil || pkg.TypesInfo == nil {
		return fmt.Errorf("%w: %s", ErrNoTypesInfo, pkg.PkgPath)
	}

	return nil
}

func listTypes(
	g *Graph,
	pkgs []*packages.Package,
	genDeclFilter func(*ast.GenDecl) (bool, error),
	typeSpecFilter func(*ast.TypeSpec, types.Object) (bool, error),
) error {
	if genDeclFilter == nil {
		genDeclFilter = func(gd *ast.GenDecl) (bool, error) { return true, nil }
	}

	if typeSpecFilter == nil {
		typeSpecFilter = func(ts *ast.TypeSpec, o types.Object) (bool, error) { return true, nil }
	}

	for _, pkg := range pkgs {
		if err := loadError(pkg); err != nil {
			return err
		}

		for _, file := range pkg.Syntax {
			var pos int

			for _, decl := range file.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok || genDecl.Tok != token.TYPE {
					continue
				}

				ok, err := genDeclFilter(genDecl)
				if err != nil {
					return err
				}

				if !ok {
					// Pos counts every type spec of the file, filtered or not.
					pos += len(genDecl.Specs)
					continue
				}

				for _, s := range genDecl.Specs {
					currentPos := pos
					pos++
					ts := s.(*ast.TypeSpec)

					obj := pkg.TypesInfo.Defs[ts.Name]
					if obj == nil {
						continue
					}

					ok, err = typeSpecFilter(ts, obj)
					if err != nil {
						return err
					}

					if !ok {
						continue
					}

					named, ok := obj.Type().(*types.Named)
					if !ok {
						continue
					}

					id := QualifiedTypeFromObject(named.Obj())
					if _, ok := g.all[id]; ok {
						continue
					}

					g.all[id] = &Node{
						Id:       id,
						Detail:   &TypeDetail{pkg, file, currentPos, ts, named},
						Parent:   map[QualifiedType]*Edge[Node]{},
						Children: map[QualifiedType]*Edge[Node]{},
					}
				}
			}
		}
	}

	return nil
}

func buildEdges(g *Graph) error {
	for _, node := range g.all {
		// Underlying matches what of go spec.
		// It means what follows type idents like below:
		//
		// type Foo struct {Foo string; Bar int}
		//          ^^^^^^^^^^^^^^^^^^^^^^^^^^^^ this part is underlying
		err := visitTypes(
			g,
			node,
			node.Detail.Type.Underlying(),
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func visitTypes(
	g *Graph,
	parentNode *Node,
	ty types.Type,
) error {
	return WalkTypesToNamed(
		nil,
		ty,
		nil,
		func(named *types.Named, stack []EdgeRouteNode) error {
			node, ok := g.all[QualifiedTypeFromObject(named.Obj())]
			if ok {
				parentNode.drawEdge(node, &EdgeRoute{slices.Clone(stack)})
			}

			return nil
		},
	)
}

// Get returns the node for the given id.
func (g *Graph) Get(id QualifiedType) (*Node, bool) {
	n, ok := g.all[id]
	return n, ok
}

// All returns all nodes in the graph.
func (g *Graph) All() iter.Seq2[QualifiedType, *Node] {
	return maps.All(g.all)
}

// Trim creates a MatchedGraph by applying matchers to filter nodes and edges.
// nodeMatcher determines which nodes are "matched" (primary targets).
// edgeMatcher determines which edges to follow when marking dependants.
// routeFilter, if non-nil, determines which routes of a followed edge are kept.
func (g *Graph) Trim(
	nodeMatcher func(n *Node) bool,
	edgeMatcher func(e *Edge[Node]) bool,
	routeFilter func(r *EdgeRoute) bool,
) *MatchedGraph {
	mg := &MatchedGraph{
		origin:    g,
		matched:   make(map[QualifiedType]*MatchedNode),
		dependant: make(map[QualifiedType]*MatchedNode),
	}

	// First pass: identify matched nodes
	maps.Insert(
		mg.matched,
		opiter.WrapSeq2(maps.All(g.all)).
			Filter(func(_ QualifiedType, node *Node) bool { return nodeMatcher(node) }).
			Map(newMatchedNode).
			Iter2(),
	)

	markDependants(mg, edgeMatcher, routeFilter)

	return mg
}

func newMatchedNode(id QualifiedType, node *Node) (QualifiedType, *MatchedNode) {
	return id, &MatchedNode{
		MatchKind: MatchKindMatched,
		Parent:    make(map[QualifiedType]*Edge[MatchedNode]),
		Children:  make(map[QualifiedType]*Edge[MatchedNode]),
		Node:      node,
	}
}

// markDependants walks upward from every matched node.
// Each node is expanded at most once, so each edge is drawn exactly once.
func markDependants(
	mg *MatchedGraph,
	edgeMatcher func(e *Edge[Node]) bool,
	routeFilter func(r *EdgeRoute) bool,
) {
	expanded := make(map[QualifiedType]bool)

	var expand func(child *MatchedNode)

	expand = func(child *MatchedNode) {
		if expanded[child.Node.Id] {
			return
		}

		expanded[child.Node.Id] = true

		for parentId, edge := range child.Node.Parent {
			if !edgeMatcher(edge) {
				continue
			}

			routes := edge.Routes
			if routeFilter != nil {
				routes = opiter.WrapSeq(slices.Values(routes)).
					Filter(routeFilter).
					CollectSlice()
			}

			parent := mg.dependantNode(parentId, edge.Parent)
			parent.drawEdge(child, routes...)

			expand(parent)
		}
	}

	for _, mn := range mg.matched {
		expand(mn)
	}
}

type MatchKind uint64

const (
	MatchKindMatched = MatchKind(1 << iota)
	MatchKindDependant
)

func (k MatchKind) IsMatched() bool {
	return k&MatchKindMatched > 0
}

func (k MatchKind) IsDependant() bool {
	return k&MatchKindDependant > 0
}

type MatchedNode struct {
	MatchKind MatchKind
	Parent    map[QualifiedType]*Edge[MatchedNode]
	Children  map[QualifiedType]*Edge[MatchedNode]
	Node      *Node
}

func (n *MatchedNode) drawEdge(
	child *MatchedNode,
	routes ...*EdgeRoute,
) {
	e, ok := child.Parent[n.Node.Id]
	if !ok {
		e = &Edge[MatchedNode]{
			Parent: n,
			Child:  child,
		}
		child.Parent[n.Node.Id] = e
		n.Children[child.Node.Id] = e
	}

	e.Routes = append(e.Routes, routes...)
}

type MatchedGraph struct {
	origin *Graph
	// matched types.
	matched map[QualifiedType]*MatchedNode
	// dependant to types listed in matched or external.
	// A node both matched and dependant is the same *MatchedNode in both maps.
	dependant map[QualifiedType]*MatchedNode
}

func (g *MatchedGraph) dependantNode(id QualifiedType, node *Node) *MatchedNode {
	n, ok := g.dependant[id]
	if ok {
		return n
	}

	n, ok = g.matched[id]
	if !ok {
		n = &MatchedNode{
			Node:     node,
			Parent:   map[QualifiedType]*Edge[MatchedNode]{},
			Children: map[QualifiedType]*Edge[MatchedNode]{},
		}
	}

	n.MatchKind |= MatchKindDependant
	g.dependant[id] = n

	return n
}

func (g *MatchedGraph) Get(id QualifiedType) (*MatchedNode, bool) {
	if n, ok := g.matched[id]; ok {
		return n, true
	}

	if n, ok := g.dependant[id]; ok {
		return n, true
	}

	return nil, false
}

// All returns matched and dependant nodes, each once.
func (g *MatchedGraph) All() iter.Seq2[QualifiedType, *MatchedNode] {
	notMatched := func(id QualifiedType, _ *MatchedNode) bool {
		_, matched := g.matched[id]
		return !matched
	}

	return opiter.WrapSeq2(maps.All(g.matched)).
		Concat(
			opiter.WrapSeq2(maps.All(g.dependant)).
				Filter(notMatched).
				Iter2(),
		).
		Iter2()
}

func (g *MatchedGraph) Matched() iter.Seq2[QualifiedType, *MatchedNode] {
	return maps.All(g.matched)
}

func (g *MatchedGraph) Dependant() iter.Seq2[QualifiedType, *MatchedNode] {
	return maps.All(g.dependant)
}

func (g *MatchedGraph) Origin() *Graph {
	return g.origin
}
