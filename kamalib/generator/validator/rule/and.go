package rule

import "go/types"

// And is the validator //kama:validate and(<validator>, ...): every
// operand holds. and() without operands always holds, as the JSON Schema
// true does.
//
// It applies to a type every operand applies to. It returns the errors of
// the failing operands joined, or with stopOnFirstError the first of them.
type And struct{}

// Name returns "and".
func (And) Name() string { return "and" }

// AndOptions are the options of //kama:validate and(<validator>, ...).
type AndOptions struct {
	// Operands are the validators of the args, in order.
	Operands []Operand
}

// Parse decodes arg into operands through r.
func (And) Parse(arg string, r Resolver) (Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	return combine(
		ops,
		operandErrors,
		func(ops []Operand) Rule { return AndOptions{Operands: ops} },
	)
}

// Check accepts a type every operand accepts.
func (o AndOptions) Check(t types.Type) error { return checkOperands(o.Operands, t) }

// Emit passes the errors of several operands to kamaValidateAll.
func (o AndOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	errs, err := emitOperands(e, o.Operands, t, v)
	if err != nil {
		return nil, err
	}

	switch len(errs) {
	case 0:
		return lang.Expr("error(nil)"), nil
	case 1:
		return errs[0], nil
	}

	e.Declare(allName, allDecl)

	return lang.Expr("%s(%s, %s)", allName, e.StopOnFirstError(), lang.Join(", ", errs...)), nil
}
