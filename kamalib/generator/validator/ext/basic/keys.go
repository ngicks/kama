package basic

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

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

// KeysArgs are the args of keys(...), which Parse reads and Call spells.
type KeysArgs OperandArgs

// Call returns the call of keys spelling a.
func (a KeysArgs) Call() (expr.Call, error) { return operandArgs.Call(Keys{}.Name(), a) }

// KeysOptions are the options of //kama:validate keys(<validator>, ...).
type KeysOptions struct {
	// Operands are the validators of the args, in order.
	Operands []Operand
}

// Parse resolves the args of c, the operands, through r.
func (Keys) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	ops, err := parseOperandArgs(r, c)
	if err != nil {
		return nil, err
	}

	return KeysOptions{Operands: ops}, nil
}

// Check accepts raw JSON text whose members every operand applies to.
func (o KeysOptions) Check(t types.Type) error { return checkRaw("keys", o.Operands, t) }

// Emit calls rawjson.Keys with a function literal of the operands.
func (o KeysOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	f, err := valueFunc(e, o.Operands, t)
	if err != nil {
		return nil, err
	}

	return rawCall(e, "Keys", v, e.StopOnFirstError(), f), nil
}
