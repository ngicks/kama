package typegraph

import (
	"go/types"
)

// EdgeRoute is the path from a parent type to a child named type.
type EdgeRoute struct {
	Nodes []EdgeRouteNode
}

// EdgeRouteNode is a single step of an EdgeRoute.
type EdgeRouteNode struct {
	Kind EdgeKind
	// Pos is the position of the struct field, type argument or type
	// parameter. HasPos reports whether Pos is meaningful for Kind.
	Pos    int
	HasPos bool
}

func posNode(kind EdgeKind, pos int) EdgeRouteNode {
	return EdgeRouteNode{Kind: kind, Pos: pos, HasPos: true}
}

type EdgeKind uint64

const (
	EdgeKindAlias = EdgeKind(1 << iota)
	EdgeKindArray
	EdgeKindChan
	EdgeKindInterface
	EdgeKindMapKey
	EdgeKindMapValue
	EdgeKindNamed
	EdgeKindPointer
	EdgeKindSlice
	EdgeKindStruct
	EdgeKindTypeParam
	EdgeKindTypeArg
)

// WalkTypesToNamed is WalkTypes that calls cb only for *types.Named.
func WalkTypesToNamed(
	stack []EdgeRouteNode,
	ty types.Type,
	stop func(ty types.Type, currentStack []EdgeRouteNode) bool,
	cb func(named *types.Named, stack []EdgeRouteNode) error,
) error {
	return WalkTypes(
		stack,
		ty,
		stop,
		func(ty types.Type, stack []EdgeRouteNode) error {
			named, ok := ty.(*types.Named)
			if !ok {
				return nil
			}

			return cb(named, stack)
		},
	)
}

// WalkTypes walks ty depth-first and calls cb for every leaf type,
// *types.Named and *types.TypeParam, with the route from ty to it.
// It does not descend into the underlying type of *types.Named,
// interfaces or function signatures.
//
// If stop reports true for a type, WalkTypes calls cb for it without descending.
//
// stack shares its backing array across calls; cb must clone it to retain it.
func WalkTypes(
	stack []EdgeRouteNode,
	ty types.Type,
	stop func(ty types.Type, currentStack []EdgeRouteNode) bool,
	cb func(ty types.Type, stack []EdgeRouteNode) error,
) error {
	if stop != nil && stop(ty, stack) {
		return cb(ty, stack)
	}
	// A recursive type must go through a named type,
	// and WalkTypes does not descend into one, so the walk terminates.
	switch x := ty.(type) {
	default:
		return cb(x, stack)
	case *types.Alias:
		if err := walkTypeParam(stack, x, stop, cb); err != nil {
			return err
		}

		return WalkTypes(append(stack, EdgeRouteNode{Kind: EdgeKindAlias}), x.Rhs(), stop, cb)
	case *types.Array:
		return WalkTypes(append(stack, EdgeRouteNode{Kind: EdgeKindArray}), x.Elem(), stop, cb)
	case *types.Chan:
		return WalkTypes(append(stack, EdgeRouteNode{Kind: EdgeKindChan}), x.Elem(), stop, cb)
	case *types.Map:
		err := WalkTypes(append(stack, EdgeRouteNode{Kind: EdgeKindMapKey}), x.Key(), stop, cb)
		if err != nil {
			return err
		}

		return WalkTypes(append(stack, EdgeRouteNode{Kind: EdgeKindMapValue}), x.Elem(), stop, cb)
	case *types.Named:
		if err := walkTypeParam(stack, x, stop, cb); err != nil {
			return err
		}

		return cb(x, stack)
	case *types.Pointer:
		return WalkTypes(append(stack, EdgeRouteNode{Kind: EdgeKindPointer}), x.Elem(), stop, cb)
	case *types.Slice:
		return WalkTypes(append(stack, EdgeRouteNode{Kind: EdgeKindSlice}), x.Elem(), stop, cb)
	case *types.Struct:
		for i := range x.NumFields() {
			f := x.Field(i)

			err := WalkTypes(
				append(stack, posNode(EdgeKindStruct, i)),
				f.Type(),
				stop,
				cb,
			)
			if err != nil {
				return err
			}
		}

		return nil
	case *types.TypeParam:
		return cb(x, stack)
	}
}

func walkTypeParam[Type interface {
	TypeArgs() *types.TypeList
	TypeParams() *types.TypeParamList
}](
	stack []EdgeRouteNode,
	ty Type,
	stopper func(ty types.Type, currentStack []EdgeRouteNode) bool,
	cb func(ty types.Type, stack []EdgeRouteNode) error,
) error {
	if ty.TypeArgs() != nil {
		for i := range ty.TypeArgs().Len() {
			arg := ty.TypeArgs().At(i)

			err := WalkTypes(
				append(stack, posNode(EdgeKindTypeArg, i)),
				arg,
				stopper,
				cb,
			)
			if err != nil {
				return err
			}
		}
	} else if ty.TypeParams() != nil {
		for i := range ty.TypeParams().Len() {
			param := ty.TypeParams().At(i)

			err := WalkTypes(
				append(stack, posNode(EdgeKindTypeParam, i)),
				param,
				stopper,
				cb,
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
