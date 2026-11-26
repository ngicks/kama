package basic

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// onHeld is rule applied the way the generator applies a rule to a field:
// to the type of the field, else to the value HeldValue gives, which holds
// when there is none. A combinator bound to the siblings of a field applies
// its other operands this way.
type onHeld struct{ rule ext.Rule }

// Check accepts a type rule accepts or holding a value of such a type.
func (h onHeld) Check(t types.Type) error {
	err := h.rule.Check(t)
	if err == nil {
		return nil
	}

	held, _, _, ok := ext.HeldValue(t, nil)
	if !ok {
		return err
	}

	return h.Check(held)
}

// Emit guards the expression of rule on the held value in a function
// literal, since the expression of a nil pointer would dereference it.
func (h onHeld) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	if h.rule.Check(t) == nil {
		return h.rule.Emit(e, t, v)
	}

	held, guard, value, _ := ext.HeldValue(t, v)

	g, err := h.Emit(e, held, value)
	if err != nil {
		return nil, err
	}

	return lang.Expr("func() error {\nif %s {\nreturn %s\n}\nreturn nil\n}()", guard, g), nil
}
