package rule

import (
	"errors"
	"fmt"
	"go/types"
	"slices"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Prefix is the validator //kama:validate prefix(<validator>, ...[,
// rest(<validator>, ...)]): the element i of an array passes the operand
// i, as JSON Schema prefixItems tests, and each later element passes every
// validator of rest, as items next to prefixItems tests. Write and(...) for
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

// PrefixOptions are the options of //kama:validate prefix(<validator>, ...).
type PrefixOptions struct {
	// Operands are the validators of the elements, in order.
	Operands []Operand
	// Rest are the validators of rest(...). HasRest tells rest() from no
	// rest.
	Rest    []Operand
	HasRest bool
}

// Parse decodes arg into operands and the rest through r.
func (Prefix) Parse(arg string, r Resolver) (Rule, error) {
	args, err := dircmnt.ParseCallArgs(arg)
	if err != nil {
		return nil, argsError(err)
	}

	var o PrefixOptions

	if n := len(args); n > 0 && args[n-1].Call && args[n-1].Key == "rest" {
		o.HasRest = true

		o.Rest, err = ParseOperands(args[n-1].Arg(), r)
		if err != nil {
			return nil, fmt.Errorf("rest: %w", err)
		}

		args = args[:n-1]
	}

	o.Operands, err = ParseOperands(dircmnt.SpellCallArgs(args), r)
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
func (o PrefixOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	return o.Annotate(e, t, v, noRecord)
}

// Annotate calls rawjson.Elements with a function literal per operand and
// one of the rest, or nil without rest.
func (o PrefixOptions) Annotate(e Emitter, t types.Type, v, ev Gen) (Gen, error) {
	rest := lang.Expr("nil")

	if o.HasRest {
		f, err := valueFunc(e, o.Rest, t)
		if err != nil {
			return nil, fmt.Errorf("rest: %w", err)
		}

		rest = f
	}

	fs, err := opiter.TryCollect(
		opiter.WrapSeq(slices.Values(o.Operands)).
			Disassemble(func(op Operand) (Gen, error) {
				return valueFunc(e, []Operand{op}, t)
			}),
	)
	if err != nil {
		return nil, err
	}

	return rawCall(e, "Elements", v, append([]Gen{e.StopOnFirstError(), ev, rest}, fs...)...), nil
}
