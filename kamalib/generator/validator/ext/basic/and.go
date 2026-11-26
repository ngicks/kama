package basic

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// And gives the meaning of the operator and: every operand holds, as in
// a and b. always, the validator without operands, always holds, as the
// JSON Schema true does.
//
// It applies to a type every operand applies to. It returns the errors of
// the failing operands joined, or with stopOnFirstError the first of them.
func And(n expr.Node, r ext.Resolver) (ext.Rule, error) {
	return combine(
		r,
		n.(expr.And).Operands,
		operandErrors,
		func(ops []Operand) ext.Rule { return AndOptions{Operands: ops} },
	)
}

// AndOptions are the operands of a and b.
type AndOptions struct {
	// Operands are the operands, in order.
	Operands []Operand
}

// Always is the validator //kama:validate always, which always holds. It
// applies to every type and emits no check.
type Always struct{}

// Name returns "always".
func (Always) Name() string { return "always" }

// Parse takes no args.
func (Always) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	return AndOptions{}, noArgs(r, c)
}

// Check accepts a type every operand accepts.
func (o AndOptions) Check(t types.Type) error { return checkOperands(o.Operands, t) }

// Emit passes the errors of several operands to kamaValidateAll.
func (o AndOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
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

// Annotate is Emit recording what every operand evaluated into ev.
func (o AndOptions) Annotate(e ext.Emitter, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	errs, err := annotateOperands(e, o.Operands, t, v, ev)
	if err != nil {
		return nil, err
	}

	return joinErrors(e, errs), nil
}
