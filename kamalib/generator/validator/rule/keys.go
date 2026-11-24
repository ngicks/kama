package rule

import "go/types"

// Keys is the validator //kama:validate keys(<validator>, ...): the name
// of each member of an object, as a JSON string, passes every operand, as
// JSON Schema propertyNames tests.
//
// It applies to raw JSON text only, as IsRawJson tells, and passes a value
// of another kind. It returns the errors of the operands, each located at
// its member.
type Keys struct{}

// Name returns "keys".
func (Keys) Name() string { return "keys" }

// KeysOptions are the options of //kama:validate keys(<validator>, ...).
type KeysOptions struct {
	// Operands are the validators of the args, in order.
	Operands []Operand
}

// Parse decodes arg into operands through r.
func (Keys) Parse(arg string, r Resolver) (Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	return KeysOptions{Operands: ops}, nil
}

// Check accepts raw JSON text whose members every operand applies to.
func (o KeysOptions) Check(t types.Type) error { return checkRaw("keys", o.Operands, t) }

// Emit calls rawjson.Keys with a function literal of the operands.
func (o KeysOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	f, err := valueFunc(e, o.Operands, t)
	if err != nil {
		return nil, err
	}

	return rawCall(e, "Keys", v, e.StopOnFirstError(), f), nil
}
