package typegraph

import (
	"go/types"
	"slices"
	"testing"

	"gotest.tools/v3/assert"
)

type visited struct {
	Name  string
	Stack []EdgeRouteNode
}

func TestWalkTypes_TypeParam(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")
	generic := findNamed(t, pkgs, "edges", "GenericType")
	assert.Assert(t, generic.TypeArgs() == nil)

	var got []visited

	err := WalkTypes(nil, generic, nil, func(ty types.Type, stack []EdgeRouteNode) error {
		switch x := ty.(type) {
		case *types.TypeParam:
			got = append(got, visited{x.Obj().Name(), slices.Clone(stack)})
		case *types.Named:
			got = append(got, visited{x.Obj().Name(), slices.Clone(stack)})
		}

		return nil
	})
	assert.NilError(t, err)

	assert.DeepEqual(t,
		[]visited{
			{"T", []EdgeRouteNode{{Kind: EdgeKindTypeParam, Pos: 0, HasPos: true}}},
			{"U", []EdgeRouteNode{{Kind: EdgeKindTypeParam, Pos: 1, HasPos: true}}},
			{"GenericType", nil},
		},
		got,
	)
}

func TestWalkTypesToNamed_Stop(t *testing.T) {
	pkgs := loadPackages(t, "./testdata/edges")
	// NestedType struct { A *map[string][]Target }
	nested := findNamed(t, pkgs, "edges", "NestedType")

	var (
		stoppedAt [][]EdgeRouteNode
		named     []string
	)

	err := WalkTypesToNamed(
		nil,
		nested.Underlying(),
		func(ty types.Type, currentStack []EdgeRouteNode) bool {
			if _, ok := ty.(*types.Map); ok {
				stoppedAt = append(stoppedAt, slices.Clone(currentStack))
				return true
			}

			return false
		},
		func(n *types.Named, stack []EdgeRouteNode) error {
			named = append(named, n.Obj().Name())
			return nil
		},
	)
	assert.NilError(t, err)

	assert.DeepEqual(t, []string(nil), named)
	assert.DeepEqual(t,
		[][]EdgeRouteNode{
			{
				{Kind: EdgeKindStruct, Pos: 0, HasPos: true},
				{Kind: EdgeKindPointer},
			},
		},
		stoppedAt,
	)
}
