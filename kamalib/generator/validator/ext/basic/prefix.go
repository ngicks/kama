package basic

import (
	"errors"
	"fmt"
	"go/types"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Prefix is the validator //kama:validate prefix(<validator>, ...[,
// rest(<validator>, ...)]): the element i of an array passes the operand
// i, as JSON Schema prefixItems tests, and each later element passes every
// validator of rest, as items next to prefixItems tests. Write a and b for
// an element passing several validators. A last arg rest(...) is the rest,
// so rest names no operand.
//
// It applies to raw JSON text only, as IsRawJson tells, and passes a value
// of another kind. It returns the errors of the operands, each located at
// its element. As an Annotator it records each element an operand or the
// rest applies to.
type Prefix struct{}

// Name returns "prefix".
func (Prefix) Name() string { return "prefix" }

// PrefixArgs are the args of prefix(...), which Parse reads and Call spells.
type PrefixArgs OperandArgs

// Call returns the call of prefix spelling a.
func (a PrefixArgs) Call() (expr.Call, error) { return operandArgs.Call(Prefix{}.Name(), a) }

// PrefixOptions are the options of //kama:validate prefix(<validator>, ...).
type PrefixOptions struct {
	// Operands are the validators of the elements, in order.
	Operands []Operand
	// Rest are the validators of rest(...). HasRest tells rest() from no
	// rest.
	Rest    []Operand
	HasRest bool
}

// Parse resolves the args of c into operands and, from a last arg
// rest(...), the rest, through r.
func (Prefix) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	args, err := operandArgs.Bind(r, c, nil)
	if err != nil {
		return nil, err
	}

	var o PrefixOptions

	if n := len(args); n > 0 {
		if rest, ok := args[n-1].Value.(expr.Call); ok && rest.Name == restEntry {
			o.HasRest = true

			o.Rest, err = parseOperandArgs(r, rest)
			if err != nil {
				return nil, fmt.Errorf("rest: %w", err)
			}

			args = args[:n-1]
		}
	}

	o.Operands, err = ParseOperands(argValues(args), r)
	if err != nil {
		return nil, err
	}

	return o, nil
}

// Check accepts raw JSON text whose elements every validator applies to.
func (o PrefixOptions) Check(t types.Type) error {
	return errors.Join(checkRaw("prefix", o.Operands, t), checkRaw("prefix", o.Rest, t))
}

// Emit calls rawjson.Elements recording nothing.
func (o PrefixOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	return o.Annotate(e, t, v, noRecord)
}

// Annotate calls rawjson.Elements with a function literal per operand and
// one of the rest, or nil without rest.
func (o PrefixOptions) Annotate(e ext.Emitter, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	rest := lang.Expr("nil")

	if o.HasRest {
		f, err := valueFunc(e, o.Rest, t)
		if err != nil {
			return nil, fmt.Errorf("rest: %w", err)
		}

		rest = f
	}

	fs, err := opiter.TryCollect(
		opiter.SlicesValues(o.Operands).
			Disassemble(func(op Operand) (ext.Gen, error) {
				return valueFunc(e, []Operand{op}, t)
			}),
	)
	if err != nil {
		return nil, err
	}

	return rawCall(
		e,
		"Elements",
		v,
		append([]ext.Gen{e.StopOnFirstError(), ev, rest}, fs...)...), nil
}
