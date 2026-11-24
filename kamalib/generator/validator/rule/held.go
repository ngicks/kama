package rule

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

// onHeld is rule applied the way the generator applies a rule to a field:
// to the type of the field, else to the value HeldValue gives, which holds
// when there is none. A combinator bound to the siblings of a field applies
// its other operands this way.
type onHeld struct{ rule Rule }

// Check accepts a type rule accepts or holding a value of such a type.
func (h onHeld) Check(t types.Type) error {
	err := h.rule.Check(t)
	if err == nil {
		return nil
	}

	held, _, _, ok := HeldValue(t, nil)
	if !ok {
		return err
	}

	return h.Check(held)
}

// Emit guards the expression of rule on the held value in a function
// literal, since the expression of a nil pointer would dereference it.
func (h onHeld) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	if h.rule.Check(t) == nil {
		return h.rule.Emit(e, t, v)
	}

	held, guard, value, _ := HeldValue(t, v)

	g, err := h.Emit(e, held, value)
	if err != nil {
		return nil, err
	}

	return lang.Expr("func() error {\nif %s {\nreturn %s\n}\nreturn nil\n}()", guard, g), nil
}
