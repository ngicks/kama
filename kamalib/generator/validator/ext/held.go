package ext

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// HeldValue returns the one value a pointer, an opt.Option or an und.Und v
// of type t may hold: its type, the condition of v holding it and the
// expression of it. ok is false for a type of another kind. The generator
// applies a rule to that value when the rule rejects t, as Rule describes.
func HeldValue(t types.Type, v Gen) (held types.Type, guard, value Gen, ok bool) {
	if p, isPtr := types.Unalias(t).(*types.Pointer); isPtr {
		return p.Elem(), lang.Expr("%s != nil", v), lang.Expr("(*%s)", v), true
	}

	var cond string

	switch {
	case typequery.IsNamed(t, typequery.OptPath, "Option"):
		cond = "%s.IsSome()"
	case typequery.IsNamed(t, typequery.UndPath, "Und"):
		cond = "%s.IsDefined()"
	default:
		return nil, nil, nil, false
	}

	return typequery.TypeArg(t), lang.Expr(cond, v), lang.Expr("%s.Value()", v), true
}
