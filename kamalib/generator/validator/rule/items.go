package rule

import (
	"go/types"
)

// Items is the validator //kama:validate items(<validator>, ...): each
// element of an array passes every operand, as JSON Schema items tests.
//
// It applies to raw JSON text only, as IsRawJson tells, and passes a value
// of another kind. It returns the errors of the operands, each located at
// its element. As an Annotator it records every element.
type Items struct{}

// Name returns "items".
func (Items) Name() string { return "items" }

// ItemsOptions are the options of //kama:validate items(<validator>, ...).
type ItemsOptions struct {
	// Operands are the validators of the args, in order.
	Operands []Operand
}

// Parse decodes arg into operands through r.
func (Items) Parse(arg string, r Resolver) (Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	return ItemsOptions{Operands: ops}, nil
}

// Check accepts raw JSON text whose elements every operand applies to.
func (o ItemsOptions) Check(t types.Type) error { return checkRaw("items", o.Operands, t) }

// Emit calls rawjson.Elements recording nothing.
func (o ItemsOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	return o.Annotate(e, t, v, noRecord)
}

// Annotate calls rawjson.Elements with a function literal of the operands.
func (o ItemsOptions) Annotate(e Emitter, t types.Type, v, ev Gen) (Gen, error) {
	f, err := valueFunc(e, o.Operands, t)
	if err != nil {
		return nil, err
	}

	return rawCall(e, "Elements", v, e.StopOnFirstError(), ev, f), nil
}
