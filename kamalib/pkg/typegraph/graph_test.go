package typegraph

import (
	"cmp"
	"errors"
	"go/ast"
	"go/types"
	"iter"
	"maps"
	"slices"
	"testing"

	"github.com/ngicks/opiter"
	"golang.org/x/tools/go/packages"
	"gotest.tools/v3/assert"
)

func loadPackages(t *testing.T, patterns ...string) []*packages.Package {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedDeps |
			packages.NeedImports,
	}, patterns...)
	assert.NilError(t, err)
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			t.Fatalf("package %s has error: %v", pkg.PkgPath, err)
		}
	}
	return pkgs
}

func pkgNamed(pkgs []*packages.Package, name string) *packages.Package {
	hasName := func(pkg *packages.Package) bool { return pkg.Name == name }
	return opiter.WrapSeq(slices.Values(pkgs)).Filter(hasName).First().UnwrapOrDefault()
}

func pkgPathOf(t *testing.T, pkgs []*packages.Package, name string) string {
	t.Helper()
	pkg := pkgNamed(pkgs, name)
	if pkg == nil {
		t.Fatalf("package %s not loaded", name)
	}
	return pkg.PkgPath
}

func findNamed(t *testing.T, pkgs []*packages.Package, pkgName, typeName string) *types.Named {
	t.Helper()
	pkg := pkgNamed(pkgs, pkgName)
	if pkg == nil {
		t.Fatalf("package %s not loaded", pkgName)
	}
	obj := pkg.Types.Scope().Lookup(typeName)
	assert.Assert(t, obj != nil)
	named, ok := obj.Type().(*types.Named)
	assert.Assert(t, ok)
	return named
}

func definedNamed(pkg *packages.Package, name string) *types.Named {
	isNamedType := func(obj types.Object) bool {
		if obj == nil || obj.Name() != name {
			return false
		}
		_, ok := obj.Type().(*types.Named)
		return ok
	}
	defs := maps.Values(pkg.TypesInfo.Defs)
	return opiter.WrapSeq(defs).Filter(isNamedType).Map(objNamed).First().UnwrapOrDefault()
}

func objNamed(obj types.Object) *types.Named { return obj.Type().(*types.Named) }

func compareQualifiedType(a, b QualifiedType) int {
	if c := cmp.Compare(a.PackagePath, b.PackagePath); c != 0 {
		return c
	}
	if c := cmp.Compare(a.TypeName, b.TypeName); c != 0 {
		return c
	}
	return 0
}

func keysSorted[V any](m map[QualifiedType]V) []QualifiedType {
	// Tests compare the keys of an empty map with an empty, non-nil slice.
	keys := make([]QualifiedType, 0, len(m))
	return slices.AppendSeq(
		keys,
		opiter.WrapSeq2(opiter.MapsSortedFunc(m, compareQualifiedType)).
			OmitL().
			Iter(),
	)
}

func collectKeysSorted[V any](seq iter.Seq2[QualifiedType, V]) []QualifiedType {
	return slices.SortedFunc(opiter.WrapSeq2(seq).OmitL().Iter(), compareQualifiedType)
}

func compareEdgeRoutesByFirstPos(a, b *EdgeRoute) int {
	if len(a.Nodes) == 0 || len(b.Nodes) == 0 {
		return 0
	}
	return cmp.Compare(a.Nodes[0].Pos, b.Nodes[0].Pos)
}

func compareEdgeRoutes(a, b *EdgeRoute) int {
	if c := cmp.Compare(len(a.Nodes), len(b.Nodes)); c != 0 {
		return c
	}
	for i := range a.Nodes {
		if i >= len(b.Nodes) {
			return 1
		}
		if c := cmp.Compare(a.Nodes[i].Kind, b.Nodes[i].Kind); c != 0 {
			return c
		}
	}
	return 0
}

func TestNew_Basic(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/basic")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	basicPkgPath := pkgPathOf(t, pkgs, "basic")

	// All 4 types should exist
	assert.DeepEqual(t,
		[]QualifiedType{
			NewQualifiedType(basicPkgPath, "TypeA"),
			NewQualifiedType(basicPkgPath, "TypeB"),
			NewQualifiedType(basicPkgPath, "TypeC"),
			NewQualifiedType(basicPkgPath, "TypeD"),
		},
		collectKeysSorted(g.All()),
	)

	// TypeA children: TypeB, TypeC
	typeA, ok := g.Get(NewQualifiedType(basicPkgPath, "TypeA"))
	assert.Assert(t, ok)
	assert.DeepEqual(t,
		[]QualifiedType{
			NewQualifiedType(basicPkgPath, "TypeB"),
			NewQualifiedType(basicPkgPath, "TypeC"),
		},
		keysSorted(typeA.Children),
	)

	// TypeA has no parents
	assert.DeepEqual(t, []QualifiedType{}, keysSorted(typeA.Parent))

	// TypeC children: TypeD
	typeC, ok := g.Get(NewQualifiedType(basicPkgPath, "TypeC"))
	assert.Assert(t, ok)
	assert.DeepEqual(t,
		[]QualifiedType{NewQualifiedType(basicPkgPath, "TypeD")},
		keysSorted(typeC.Children),
	)

	// TypeC parent: TypeA
	assert.DeepEqual(t,
		[]QualifiedType{NewQualifiedType(basicPkgPath, "TypeA")},
		keysSorted(typeC.Parent),
	)
}

func TestNew_Edges(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	edgesPkgPath := pkgPathOf(t, pkgs, "edges")

	targetId := NewQualifiedType(edgesPkgPath, "Target")
	genericId := NewQualifiedType(edgesPkgPath, "GenericType")

	// SliceType -> Target
	sliceType, _ := g.Get(NewQualifiedType(edgesPkgPath, "SliceType"))
	assert.DeepEqual(t, []QualifiedType{targetId}, keysSorted(sliceType.Children))

	// ArrayType -> Target
	arrayType, _ := g.Get(NewQualifiedType(edgesPkgPath, "ArrayType"))
	assert.DeepEqual(t, []QualifiedType{targetId}, keysSorted(arrayType.Children))

	// MapType -> Target
	mapType, _ := g.Get(NewQualifiedType(edgesPkgPath, "MapType"))
	assert.DeepEqual(t, []QualifiedType{targetId}, keysSorted(mapType.Children))

	// ChanType -> Target
	chanType, _ := g.Get(NewQualifiedType(edgesPkgPath, "ChanType"))
	assert.DeepEqual(t, []QualifiedType{targetId}, keysSorted(chanType.Children))

	// PointerType -> Target
	pointerType, _ := g.Get(NewQualifiedType(edgesPkgPath, "PointerType"))
	assert.DeepEqual(t, []QualifiedType{targetId}, keysSorted(pointerType.Children))

	// StructType -> Target
	structType, _ := g.Get(NewQualifiedType(edgesPkgPath, "StructType"))
	assert.DeepEqual(t, []QualifiedType{targetId}, keysSorted(structType.Children))

	// NestedType -> Target (through *map[string][]Target)
	nestedType, _ := g.Get(NewQualifiedType(edgesPkgPath, "NestedType"))
	assert.DeepEqual(t, []QualifiedType{targetId}, keysSorted(nestedType.Children))

	// InstantiatedType -> GenericType, Target (through type arg)
	instantiatedType, _ := g.Get(NewQualifiedType(edgesPkgPath, "InstantiatedType"))
	assert.DeepEqual(t,
		[]QualifiedType{genericId, targetId},
		keysSorted(instantiatedType.Children),
	)

	// Target should have many parents
	target, _ := g.Get(targetId)
	assert.DeepEqual(t,
		[]QualifiedType{
			NewQualifiedType(edgesPkgPath, "ArrayType"),
			NewQualifiedType(edgesPkgPath, "ChanType"),
			NewQualifiedType(edgesPkgPath, "InstantiatedType"),
			NewQualifiedType(edgesPkgPath, "MapType"),
			NewQualifiedType(edgesPkgPath, "NestedType"),
			NewQualifiedType(edgesPkgPath, "PointerType"),
			NewQualifiedType(edgesPkgPath, "SliceType"),
			NewQualifiedType(edgesPkgPath, "StructType"),
		},
		keysSorted(target.Parent),
	)
}

func TestNew_WithFilters(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/basic")

	// Filter to only include TypeA and TypeB
	typeSpecFilter := func(ts *ast.TypeSpec, obj types.Object) (bool, error) {
		return ts.Name.Name == "TypeA" || ts.Name.Name == "TypeB", nil
	}

	g, err := New(pkgs, nil, typeSpecFilter)
	assert.NilError(t, err)

	basicPkgPath := pkgPathOf(t, pkgs, "basic")

	// Only TypeA and TypeB should exist
	assert.DeepEqual(t,
		[]QualifiedType{
			NewQualifiedType(basicPkgPath, "TypeA"),
			NewQualifiedType(basicPkgPath, "TypeB"),
		},
		collectKeysSorted(g.All()),
	)

	// TypeA -> TypeB (TypeC filtered out)
	typeA, _ := g.Get(NewQualifiedType(basicPkgPath, "TypeA"))
	assert.DeepEqual(t,
		[]QualifiedType{NewQualifiedType(basicPkgPath, "TypeB")},
		keysSorted(typeA.Children),
	)
}

func TestTrim_MatchAll(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/basic")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	basicPkgPath := pkgPathOf(t, pkgs, "basic")

	// Match all nodes
	mg := g.Trim(
		func(n *Node) bool { return true },
		func(e *Edge[Node]) bool { return true },
		nil,
	)

	// All nodes should be matched
	assert.DeepEqual(t,
		[]QualifiedType{
			NewQualifiedType(basicPkgPath, "TypeA"),
			NewQualifiedType(basicPkgPath, "TypeB"),
			NewQualifiedType(basicPkgPath, "TypeC"),
			NewQualifiedType(basicPkgPath, "TypeD"),
		},
		collectKeysSorted(mg.Matched()),
	)

	// TypeA and TypeC are dependants because they are parents of matched nodes
	// (markDependants walks upward from matched nodes)
	assert.DeepEqual(t,
		[]QualifiedType{
			NewQualifiedType(basicPkgPath, "TypeA"),
			NewQualifiedType(basicPkgPath, "TypeC"),
		},
		collectKeysSorted(mg.Dependant()),
	)

	// A node both matched and dependant is a single node carrying both kinds and its edges.
	typeAId := NewQualifiedType(basicPkgPath, "TypeA")
	matchedA, _ := mg.Get(typeAId)
	isTypeA := func(id QualifiedType, _ *MatchedNode) bool { return id == typeAId }
	dependants := opiter.WrapSeq2(mg.Dependant()).Filter(isTypeA).OmitF()
	dependantA := dependants.First().UnwrapOrDefault()
	assert.Assert(t, matchedA == dependantA)
	assert.Equal(t, MatchKindMatched|MatchKindDependant, matchedA.MatchKind)
	assert.DeepEqual(t,
		[]QualifiedType{
			NewQualifiedType(basicPkgPath, "TypeB"),
			NewQualifiedType(basicPkgPath, "TypeC"),
		},
		keysSorted(matchedA.Children),
	)
	// TypeA -> TypeC is reachable from both matched TypeC and matched TypeD; its route is not
	// duplicated.
	assert.Equal(t, 1, len(matchedA.Children[NewQualifiedType(basicPkgPath, "TypeC")].Routes))
	typeD, _ := mg.Get(NewQualifiedType(basicPkgPath, "TypeD"))
	assert.Equal(t, MatchKindMatched, typeD.MatchKind)
	assert.Equal(t, 4, len(collectKeysSorted(mg.All())))
}

func TestTrim_MatchLeaf(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/basic")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	basicPkgPath := pkgPathOf(t, pkgs, "basic")

	typeDId := NewQualifiedType(basicPkgPath, "TypeD")

	// Match only TypeD (leaf node)
	mg := g.Trim(
		func(n *Node) bool { return n.Id == typeDId },
		func(e *Edge[Node]) bool { return true },
		nil,
	)

	// Only TypeD should be matched
	assert.DeepEqual(t,
		[]QualifiedType{typeDId},
		collectKeysSorted(mg.Matched()),
	)

	// TypeA and TypeC should be dependants (path: TypeA -> TypeC -> TypeD)
	assert.DeepEqual(t,
		[]QualifiedType{
			NewQualifiedType(basicPkgPath, "TypeA"),
			NewQualifiedType(basicPkgPath, "TypeC"),
		},
		collectKeysSorted(mg.Dependant()),
	)

	// Check MatchKind
	typeD, _ := mg.Get(typeDId)
	assert.Equal(t, MatchKindMatched, typeD.MatchKind)
	typeA, _ := mg.Get(NewQualifiedType(basicPkgPath, "TypeA"))
	assert.Equal(t, MatchKindDependant, typeA.MatchKind)
	typeC, _ := mg.Get(NewQualifiedType(basicPkgPath, "TypeC"))
	assert.Equal(t, MatchKindDependant, typeC.MatchKind)
}

func TestTrim_EdgeMatcher(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/basic")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	basicPkgPath := pkgPathOf(t, pkgs, "basic")

	typeDId := NewQualifiedType(basicPkgPath, "TypeD")
	typeCId := NewQualifiedType(basicPkgPath, "TypeC")

	// Match TypeD, but block edge from TypeC (which blocks TypeA from being a dependant)
	mg := g.Trim(
		func(n *Node) bool {
			return n.Id == typeDId
		},
		func(e *Edge[Node]) bool {
			// Block edge from TypeA to TypeC
			return e.Parent.Id != NewQualifiedType(basicPkgPath, "TypeA")
		},
		nil,
	)

	// TypeD should be matched
	assert.DeepEqual(t, []QualifiedType{typeDId}, collectKeysSorted(mg.Matched()))

	// Only TypeC is dependant (TypeA is blocked because edge TypeA->TypeC is filtered)
	assert.DeepEqual(t,
		[]QualifiedType{typeCId},
		collectKeysSorted(mg.Dependant()),
	)
}

func TestMatchKind(t *testing.T) {
	t.Run("Matched", func(t *testing.T) {
		assert.Equal(t, true, MatchKindMatched.IsMatched())
		assert.Equal(t, false, MatchKindMatched.IsDependant())
	})

	t.Run("Dependant", func(t *testing.T) {
		assert.Equal(t, false, MatchKindDependant.IsMatched())
		assert.Equal(t, true, MatchKindDependant.IsDependant())
	})

	t.Run("Combined", func(t *testing.T) {
		combined := MatchKindMatched | MatchKindDependant
		assert.Equal(t, true, combined.IsMatched())
		assert.Equal(t, true, combined.IsDependant())
	})
}

func TestTraverseTypes(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")

	edgesPkg := pkgNamed(pkgs, "edges")

	// Find NestedType: A *map[string][]Target
	nestedType := definedNamed(edgesPkg, "NestedType")
	assert.Assert(t, nestedType != nil)

	// Traverse and collect all named types
	var namedTypes []string
	err := WalkTypesToNamed(
		nil,
		nestedType.Underlying(),
		nil,
		func(named *types.Named, stack []EdgeRouteNode) error {
			namedTypes = append(namedTypes, named.Obj().Name())
			return nil
		},
	)
	assert.NilError(t, err)

	slices.Sort(namedTypes)
	assert.DeepEqual(t, []string{"Target"}, namedTypes)
}

func TestTraverseTypes_InstantiatedGeneric(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")

	edgesPkg := pkgNamed(pkgs, "edges")

	// Find InstantiatedType: G GenericType[Target, map[[2]Target][]Target]
	instantiatedType := definedNamed(edgesPkg, "InstantiatedType")
	assert.Assert(t, instantiatedType != nil)

	// Traverse and collect all named types
	var namedTypes []string
	err := WalkTypesToNamed(
		nil,
		instantiatedType.Underlying(),
		nil,
		func(named *types.Named, stack []EdgeRouteNode) error {
			namedTypes = append(namedTypes, named.Obj().Name())
			return nil
		},
	)
	assert.NilError(t, err)

	slices.Sort(namedTypes)
	// Should find GenericType once, and Target 3 times (from type arguments:
	// first type arg Target, second type arg map[[2]Target][]Target has Target in key and value)
	assert.DeepEqual(t, []string{"GenericType", "Target", "Target", "Target"}, namedTypes)
}

func TestBidirectionalEdges(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/basic")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	basicPkgPath := pkgPathOf(t, pkgs, "basic")

	typeAId := NewQualifiedType(basicPkgPath, "TypeA")
	typeBId := NewQualifiedType(basicPkgPath, "TypeB")

	typeA, _ := g.Get(typeAId)
	typeB, _ := g.Get(typeBId)

	// Get edges from both sides
	edgeFromA := typeA.Children[typeBId]
	edgeFromB := typeB.Parent[typeAId]

	// Both should reference the same edge object
	assert.Assert(t, edgeFromA == edgeFromB)

	// Verify edge structure
	assert.Equal(t, typeAId, edgeFromA.Parent.Id)
	assert.Equal(t, typeBId, edgeFromA.Child.Id)
}

func TestNew_Edges_DetailedRoutes(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	edgesPkg := pkgNamed(pkgs, "edges")
	edgesPkgPath := edgesPkg.PkgPath

	targetId := NewQualifiedType(edgesPkgPath, "Target")
	genericId := NewQualifiedType(edgesPkgPath, "GenericType")

	t.Run("SliceType routes", func(t *testing.T) {
		// SliceType = []Target
		// Route: underlying is []Target, so route is just [Slice]
		sliceType, _ := g.Get(NewQualifiedType(edgesPkgPath, "SliceType"))
		edge := sliceType.Children[targetId]
		assert.Assert(t, edge != nil)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindSlice},
		}}, edge.Routes[0])
	})

	t.Run("ArrayType routes", func(t *testing.T) {
		// ArrayType = [5]Target
		// Route: underlying is [5]Target, so route is just [Array]
		arrayType, _ := g.Get(NewQualifiedType(edgesPkgPath, "ArrayType"))
		edge := arrayType.Children[targetId]
		assert.Assert(t, edge != nil)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindArray},
		}}, edge.Routes[0])
	})

	t.Run("MapType routes", func(t *testing.T) {
		// MapType = map[string]Target
		// Route: underlying is map[string]Target, so route is just [MapValue]
		mapType, _ := g.Get(NewQualifiedType(edgesPkgPath, "MapType"))
		edge := mapType.Children[targetId]
		assert.Assert(t, edge != nil)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindMapValue},
		}}, edge.Routes[0])
	})

	t.Run("ChanType routes", func(t *testing.T) {
		// ChanType = chan Target
		// Route: underlying is chan Target, so route is just [Chan]
		chanType, _ := g.Get(NewQualifiedType(edgesPkgPath, "ChanType"))
		edge := chanType.Children[targetId]
		assert.Assert(t, edge != nil)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindChan},
		}}, edge.Routes[0])
	})

	t.Run("PointerType routes", func(t *testing.T) {
		// PointerType = *Target
		// Route: underlying is *Target, so route is just [Pointer]
		pointerType, _ := g.Get(NewQualifiedType(edgesPkgPath, "PointerType"))
		edge := pointerType.Children[targetId]
		assert.Assert(t, edge != nil)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindPointer},
		}}, edge.Routes[0])
	})

	t.Run("StructType routes", func(t *testing.T) {
		// StructType struct { A Target; B *Target; C []Target }
		// Multiple routes to Target
		structType, _ := g.Get(NewQualifiedType(edgesPkgPath, "StructType"))
		edge := structType.Children[targetId]
		assert.Assert(t, edge != nil)
		// Sort routes by first node's pos for stable comparison
		routes := slices.Clone(edge.Routes)
		slices.SortFunc(routes, compareEdgeRoutesByFirstPos)
		// A Target (field 0)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindStruct, Pos: 0, HasPos: true},
		}}, routes[0])
		// B *Target (field 1)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindStruct, Pos: 1, HasPos: true},
			{Kind: EdgeKindPointer},
		}}, routes[1])
		// C []Target (field 2)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindStruct, Pos: 2, HasPos: true},
			{Kind: EdgeKindSlice},
		}}, routes[2])
	})

	t.Run("NestedType routes", func(t *testing.T) {
		// NestedType struct { A *map[string][]Target }
		nestedType, _ := g.Get(NewQualifiedType(edgesPkgPath, "NestedType"))
		edge := nestedType.Children[targetId]
		assert.Assert(t, edge != nil)
		assert.DeepEqual(t, &EdgeRoute{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindStruct, Pos: 0, HasPos: true},
			{Kind: EdgeKindPointer},
			{Kind: EdgeKindMapValue},
			{Kind: EdgeKindSlice},
		}}, edge.Routes[0])
	})

	t.Run("InstantiatedType routes", func(t *testing.T) {
		instantiatedType, _ := g.Get(NewQualifiedType(edgesPkgPath, "InstantiatedType"))

		edgeToGeneric := instantiatedType.Children[genericId]

		assert.Assert(t, edgeToGeneric != nil)
		assert.DeepEqual(
			t,
			&EdgeRoute{
				Nodes: []EdgeRouteNode{
					{Kind: EdgeKindStruct, Pos: 0, HasPos: true},
				},
			},
			edgeToGeneric.Routes[0],
		)

		edgeToTarget := instantiatedType.Children[targetId]

		assert.Assert(t, edgeToTarget != nil)

		routes := slices.Clone(edgeToTarget.Routes)
		slices.SortFunc(routes, compareEdgeRoutes)

		assert.DeepEqual(
			t,
			&EdgeRoute{
				Nodes: []EdgeRouteNode{
					{Kind: EdgeKindStruct, Pos: 0, HasPos: true},
					{Kind: EdgeKindTypeArg, Pos: 0, HasPos: true},
				},
			},
			routes[0],
		)
		assert.DeepEqual(
			t,
			&EdgeRoute{
				Nodes: []EdgeRouteNode{
					{Kind: EdgeKindStruct, Pos: 0, HasPos: true},
					{Kind: EdgeKindTypeArg, Pos: 1, HasPos: true},
					{Kind: EdgeKindMapKey},
					{Kind: EdgeKindArray},
				},
			},
			routes[1],
		)
		assert.DeepEqual(
			t,
			&EdgeRoute{
				Nodes: []EdgeRouteNode{
					{Kind: EdgeKindStruct, Pos: 0, HasPos: true},
					{Kind: EdgeKindTypeArg, Pos: 1, HasPos: true},
					{Kind: EdgeKindMapValue},
					{Kind: EdgeKindSlice},
				},
			},
			routes[2],
		)
	})
}

func TestNew_Edges_TypeDetail(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	edgesPkg := pkgNamed(pkgs, "edges")
	edgesPkgPath := edgesPkg.PkgPath

	t.Run("Target TypeDetail", func(t *testing.T) {
		target, ok := g.Get(NewQualifiedType(edgesPkgPath, "Target"))
		assert.Assert(t, ok)
		assert.Assert(t, target.Detail != nil)
		assert.Equal(t, edgesPkg, target.Detail.Pkg)
		assert.Assert(t, target.Detail.File != nil)
		assert.Assert(t, target.Detail.Ts != nil)
		assert.Equal(t, "Target", target.Detail.Ts.Name.Name)
		assert.Assert(t, target.Detail.Type != nil)
		assert.Equal(t, "Target", target.Detail.Type.Obj().Name())
	})

	t.Run("GenericType TypeDetail", func(t *testing.T) {
		genericType, ok := g.Get(NewQualifiedType(edgesPkgPath, "GenericType"))
		assert.Assert(t, ok)
		assert.Assert(t, genericType.Detail != nil)
		assert.Equal(t, edgesPkg, genericType.Detail.Pkg)
		assert.Assert(t, genericType.Detail.Ts != nil)
		assert.Equal(t, "GenericType", genericType.Detail.Ts.Name.Name)
		// Verify it's a generic type with 2 type params
		assert.Assert(t, genericType.Detail.Type != nil)
		assert.Equal(t, 2, genericType.Detail.Type.TypeParams().Len())
	})

	t.Run("Pos ordering for grouped type declarations", func(t *testing.T) {
		// SliceType, ArrayType, MapType, ChanType, PointerType are in the same type block
		sliceType, _ := g.Get(NewQualifiedType(edgesPkgPath, "SliceType"))
		arrayType, _ := g.Get(NewQualifiedType(edgesPkgPath, "ArrayType"))
		mapType, _ := g.Get(NewQualifiedType(edgesPkgPath, "MapType"))
		chanType, _ := g.Get(NewQualifiedType(edgesPkgPath, "ChanType"))
		pointerType, _ := g.Get(NewQualifiedType(edgesPkgPath, "PointerType"))

		// All in same file
		assert.Equal(t, sliceType.Detail.File, arrayType.Detail.File)
		assert.Equal(t, sliceType.Detail.File, mapType.Detail.File)
		assert.Equal(t, sliceType.Detail.File, chanType.Detail.File)
		assert.Equal(t, sliceType.Detail.File, pointerType.Detail.File)

		// Pos should be increasing (they're declared in order)
		assert.Assert(t, sliceType.Detail.Pos < arrayType.Detail.Pos)
		assert.Assert(t, arrayType.Detail.Pos < mapType.Detail.Pos)
		assert.Assert(t, mapType.Detail.Pos < chanType.Detail.Pos)
		assert.Assert(t, chanType.Detail.Pos < pointerType.Detail.Pos)
	})
}

func TestNew_PosCountsFilteredSpecs(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")
	edgesPkgPath := pkgPathOf(t, pkgs, "edges")

	pos := func(t *testing.T, g *Graph, name string) int {
		t.Helper()
		n, ok := g.Get(NewQualifiedType(edgesPkgPath, name))
		assert.Assert(t, ok, "%s", name)
		return n.Detail.Pos
	}

	t.Run("no filter", func(t *testing.T) {
		g, err := New(pkgs, nil, nil)
		assert.NilError(t, err)
		assert.Equal(t, pos(t, g, "Target"), 0)
		assert.Equal(t, pos(t, g, "SliceType"), 1)
		assert.Equal(t, pos(t, g, "PointerType"), 5)
		assert.Equal(t, pos(t, g, "StructType"), 6)
		assert.Equal(t, pos(t, g, "InstantiatedType"), 9)
	})

	t.Run("rejected GenDecl", func(t *testing.T) {
		g, err := New(pkgs, func(gd *ast.GenDecl) (bool, error) {
			return len(gd.Specs) == 1, nil
		}, nil)
		assert.NilError(t, err)
		_, ok := g.Get(NewQualifiedType(edgesPkgPath, "SliceType"))
		assert.Assert(t, !ok)
		assert.Equal(t, pos(t, g, "Target"), 0)
		assert.Equal(t, pos(t, g, "StructType"), 6)
		assert.Equal(t, pos(t, g, "InstantiatedType"), 9)
	})

	t.Run("rejected TypeSpec", func(t *testing.T) {
		g, err := New(pkgs, nil, func(ts *ast.TypeSpec, _ types.Object) (bool, error) {
			return ts.Name.Name != "SliceType", nil
		})
		assert.NilError(t, err)
		_, ok := g.Get(NewQualifiedType(edgesPkgPath, "SliceType"))
		assert.Assert(t, !ok)
		assert.Equal(t, pos(t, g, "ArrayType"), 2)
		assert.Equal(t, pos(t, g, "StructType"), 6)
	})
}

func TestNew_NamedCollection(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/collection")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	pkgPath := pkgPathOf(t, pkgs, "collection")
	elemId := NewQualifiedType(pkgPath, "Elem")
	keyId := NewQualifiedType(pkgPath, "Key")
	sId := NewQualifiedType(pkgPath, "S")
	mId := NewQualifiedType(pkgPath, "M")

	assert.DeepEqual(t,
		[]QualifiedType{elemId, keyId, mId, sId},
		collectKeysSorted(g.All()),
	)

	t.Run("S []Elem", func(t *testing.T) {
		s, _ := g.Get(sId)
		assert.DeepEqual(t, []QualifiedType{elemId}, keysSorted(s.Children))
		assert.DeepEqual(t,
			[]*EdgeRoute{{Nodes: []EdgeRouteNode{{Kind: EdgeKindSlice}}}},
			s.Children[elemId].Routes,
		)
	})

	t.Run("M map[Key]Elem", func(t *testing.T) {
		m, _ := g.Get(mId)
		assert.DeepEqual(t, []QualifiedType{elemId, keyId}, keysSorted(m.Children))
		assert.DeepEqual(t,
			[]*EdgeRoute{{Nodes: []EdgeRouteNode{{Kind: EdgeKindMapKey}}}},
			m.Children[keyId].Routes,
		)
		assert.DeepEqual(t,
			[]*EdgeRoute{{Nodes: []EdgeRouteNode{{Kind: EdgeKindMapValue}}}},
			m.Children[elemId].Routes,
		)
	})

	t.Run("parents", func(t *testing.T) {
		elem, _ := g.Get(elemId)
		assert.DeepEqual(t, []QualifiedType{mId, sId}, keysSorted(elem.Parent))
		key, _ := g.Get(keyId)
		assert.DeepEqual(t, []QualifiedType{mId}, keysSorted(key.Parent))
	})

	t.Run("Trim from Elem", func(t *testing.T) {
		mg := g.Trim(
			func(n *Node) bool { return n.Id == elemId },
			func(e *Edge[Node]) bool { return true },
			nil,
		)
		assert.DeepEqual(t, []QualifiedType{mId, sId}, collectKeysSorted(mg.Dependant()))
		elem, _ := mg.Get(elemId)
		assert.DeepEqual(t, []QualifiedType{mId, sId}, keysSorted(elem.Parent))
	})
}

func TestTrim_Diamond(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/diamond")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	pkgPath := pkgPathOf(t, pkgs, "diamond")
	aId := NewQualifiedType(pkgPath, "A")
	bId := NewQualifiedType(pkgPath, "B")
	cId := NewQualifiedType(pkgPath, "C")
	dId := NewQualifiedType(pkgPath, "D")

	mg := g.Trim(
		func(n *Node) bool { return n.Id == dId },
		func(e *Edge[Node]) bool { return true },
		nil,
	)

	assert.DeepEqual(t, []QualifiedType{aId, bId, cId}, collectKeysSorted(mg.Dependant()))

	a, _ := mg.Get(aId)
	assert.DeepEqual(t, []QualifiedType{bId, cId}, keysSorted(a.Children))
	b, _ := mg.Get(bId)
	assert.DeepEqual(t, []QualifiedType{aId}, keysSorted(b.Parent))
	c, _ := mg.Get(cId)
	assert.DeepEqual(t, []QualifiedType{aId}, keysSorted(c.Parent))
	d, _ := mg.Get(dId)
	assert.DeepEqual(t, []QualifiedType{bId, cId}, keysSorted(d.Parent))

	for _, n := range mg.All() {
		for _, e := range n.Children {
			assert.Equal(
				t,
				1,
				len(e.Routes),
				"%s -> %s",
				n.Node.Id.TypeName,
				e.Child.Node.Id.TypeName,
			)
		}
	}
}

func isPointerNode(n EdgeRouteNode) bool { return n.Kind == EdgeKindPointer }

func TestTrim_RouteFilter(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	pkgPath := pkgPathOf(t, pkgs, "edges")
	targetId := NewQualifiedType(pkgPath, "Target")
	structId := NewQualifiedType(pkgPath, "StructType")

	mg := g.Trim(
		func(n *Node) bool { return n.Id == targetId },
		func(e *Edge[Node]) bool { return e.Parent.Id == structId },
		func(r *EdgeRoute) bool {
			return opiter.WrapSeq(slices.Values(r.Nodes)).ContainsFunc(isPointerNode)
		},
	)

	assert.DeepEqual(t, []QualifiedType{structId}, collectKeysSorted(mg.Dependant()))
	s, _ := mg.Get(structId)
	assert.DeepEqual(t,
		[]*EdgeRoute{{Nodes: []EdgeRouteNode{
			{Kind: EdgeKindStruct, Pos: 1, HasPos: true},
			{Kind: EdgeKindPointer},
		}}},
		s.Children[targetId].Routes,
	)

	// The filter must not alter the routes of the origin graph.
	origin, _ := g.Get(structId)
	assert.Equal(t, 3, len(origin.Children[targetId].Routes))
}

func TestNew_Cyclic(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/cyclic", "./testdata/target")

	g, err := New(pkgs, nil, nil)
	assert.NilError(t, err)

	cyclicPath := pkgPathOf(t, pkgs, "cyclic")
	targetPath := pkgPathOf(t, pkgs, "target")
	treeId := NewQualifiedType(cyclicPath, "Tree")
	embeddedId := NewQualifiedType(cyclicPath, "CyclicEmbedded")
	r1Id := NewQualifiedType(cyclicPath, "recursion1")
	r2Id := NewQualifiedType(cyclicPath, "recursion2")
	targetId := NewQualifiedType(targetPath, "Target")

	tree, _ := g.Get(treeId)
	assert.DeepEqual(t, []QualifiedType{treeId, targetId}, keysSorted(tree.Children))
	assert.DeepEqual(t,
		[]*EdgeRoute{
			{
				Nodes: []EdgeRouteNode{
					{Kind: EdgeKindStruct, Pos: 0, HasPos: true},
					{Kind: EdgeKindPointer},
				},
			},
			{
				Nodes: []EdgeRouteNode{
					{Kind: EdgeKindStruct, Pos: 1, HasPos: true},
					{Kind: EdgeKindPointer},
				},
			},
		},
		tree.Children[treeId].Routes,
	)
	assert.Assert(t, tree.Children[treeId] == tree.Parent[treeId])

	r2, _ := g.Get(r2Id)
	assert.DeepEqual(t, []QualifiedType{embeddedId}, keysSorted(r2.Children))

	mg := g.Trim(
		func(n *Node) bool { return n.Id == targetId },
		func(e *Edge[Node]) bool { return true },
		nil,
	)
	assert.DeepEqual(t, []QualifiedType{targetId}, collectKeysSorted(mg.Matched()))
	assert.DeepEqual(t,
		[]QualifiedType{embeddedId, treeId, r1Id, r2Id},
		collectKeysSorted(mg.Dependant()),
	)
	mTree, _ := mg.Get(treeId)
	assert.DeepEqual(t, []QualifiedType{treeId, targetId}, keysSorted(mTree.Children))
	mEmbedded, _ := mg.Get(embeddedId)
	assert.DeepEqual(t, []QualifiedType{r2Id}, keysSorted(mEmbedded.Parent))
}

func TestNew_Errors(t *testing.T) {
	t.Run("load error", func(t *testing.T) {
		_, err := New(
			[]*packages.Package{
				{PkgPath: "example.com/broken", Errors: []packages.Error{{Msg: "boom"}}},
			},
			nil,
			nil,
		)
		assert.ErrorIs(t, err, ErrLoad)
		assert.ErrorContains(t, err, "boom")
	})

	t.Run("no types info", func(t *testing.T) {
		_, err := New([]*packages.Package{{PkgPath: "example.com/notypes"}}, nil, nil)
		assert.ErrorIs(t, err, ErrNoTypesInfo)
	})

	t.Run("filter error", func(t *testing.T) {
		pkgs := loadPackages(t, "./testdata/basic")
		errFilter := errors.New("filter")
		_, err := New(
			pkgs,
			nil,
			func(*ast.TypeSpec, types.Object) (bool, error) { return false, errFilter },
		)
		assert.ErrorIs(t, err, errFilter)
		_, err = New(pkgs, func(*ast.GenDecl) (bool, error) { return false, errFilter }, nil)
		assert.ErrorIs(t, err, errFilter)
	})
}
