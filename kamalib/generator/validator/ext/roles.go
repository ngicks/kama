package ext

import "go/types"

// Roles returns the names of the roles of the values a value of type t
// holds: e for the elements of a slice or an array and for the value a
// pointer points to, k and v for the keys and the values of a map, and the
// roles the supports of r give for a type they claim. r may be nil.
//
// A role name is a Go identifier. It must not be hook, pre, post or the
// name of a validator of the registry, so that a directive item can name
// a role.
func Roles(r Resolver, t types.Type) []string {
	switch types.Unalias(t).Underlying().(type) {
	case *types.Slice, *types.Array, *types.Pointer:
		return []string{"e"}
	case *types.Map:
		return []string{"k", "v"}
	}

	var roles []string

	if h, ok := HolderOf(r, t); ok {
		role, _, _, _, _ := h.Held(t, nil)
		roles = append(roles, role)
	}

	if c, ok := ContainerOf(r, t); ok {
		role, _, _, _, _ := c.Elements(t, nil)
		roles = append(roles, role)
	}

	if c, ok := CompositeOf(r, t); ok {
		roles = append(roles, c.Roles(t)...)
	}

	return roles
}

// RoleValue tells how the values of role are reached from the value v of
// type t, for a role Roles returns. ok is false for another role.
//
// held is the type of the values. A role of one value has a nil over:
// guard is the condition of v holding the value, nil when v always holds
// it, value is the expression of it, and segment and label locate it. A
// role of many values, the elements of a slice, an array, a map or a
// Container, has over: the expression a statement for pos, elem := range
// over ranges over. guard is nil, and value and label are expressed with
// pos and elem, the names the caller declares the range variables by. A
// map key is pos itself.
func RoleValue(
	r Resolver,
	t types.Type,
	v Gen,
	role string,
	pos, elem Gen,
) (held types.Type, guard, over, value Gen, segment Segment, label Gen, ok bool) {
	switch u := types.Unalias(t).Underlying().(type) {
	case *types.Pointer:
		if role == "e" {
			return u.Elem(), lang.Expr("%s != nil", v), nil, lang.Expr("(*%s)", v), SegmentNone,
				nil, true
		}

		return nil, nil, nil, nil, SegmentNone, nil, false
	case *types.Slice:
		if role == "e" {
			return u.Elem(), nil, v, elem, SegmentIndex, pos, true
		}

		return nil, nil, nil, nil, SegmentNone, nil, false
	case *types.Array:
		if role == "e" {
			return u.Elem(), nil, v, elem, SegmentIndex, pos, true
		}

		return nil, nil, nil, nil, SegmentNone, nil, false
	case *types.Map:
		switch role {
		case "k":
			return u.Key(), nil, v, pos, SegmentKey, pos, true
		case "v":
			return u.Elem(), nil, v, elem, SegmentKey, pos, true
		}

		return nil, nil, nil, nil, SegmentNone, nil, false
	}

	if h, ok := HolderOf(r, t); ok {
		if name, held, guard, value, _ := h.Held(t, v); name == role {
			return held, guard, nil, value, SegmentNone, nil, true
		}
	}

	if c, ok := ContainerOf(r, t); ok {
		if name, held, over, segment, _ := c.Elements(t, v); name == role {
			return held, nil, over, elem, segment, pos, true
		}
	}

	if c, ok := CompositeOf(r, t); ok {
		if held, guard, value, segment, label, ok := c.Part(t, v, role); ok {
			return held, guard, nil, value, segment, label, true
		}
	}

	return nil, nil, nil, nil, SegmentNone, nil, false
}
