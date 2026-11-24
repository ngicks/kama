package rule

import (
	"fmt"
	"go/types"
	"strconv"
)

// Not is the validator //kama:validate not(<validator>): the one operand
// fails, as JSON Schema not tests.
//
// It applies to a type its operand applies to. Its error wraps
// validation.ErrNot and spells the operand.
type Not struct{}

// Name returns "not".
func (Not) Name() string { return "not" }

// NotOptions are the options of //kama:validate not(<validator>).
type NotOptions struct {
	// Operand is the validator of the arg.
	Operand Operand
}

// Parse decodes arg into one operand through r.
func (Not) Parse(arg string, r Resolver) (Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	if len(ops) != 1 {
		return nil, argsError(fmt.Errorf("want one operand, got %d", len(ops)))
	}

	return combine(ops, ownErrors, func(ops []Operand) Rule { return NotOptions{Operand: ops[0]} })
}

// Check accepts a type the operand accepts.
func (o NotOptions) Check(t types.Type) error {
	return checkOperands([]Operand{o.Operand}, t)
}

// Emit passes the error of the operand to kamaValidateNot.
func (o NotOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	errs, err := emitOperands(e, []Operand{o.Operand}, t, v)
	if err != nil {
		return nil, err
	}

	e.Declare(notName, notDecl)

	return lang.Expr("%s(%s, %s)", notName, errs[0], strconv.Quote(o.Operand.Text)), nil
}

const notName = "kamaValidateNot"

func notDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s error, %s string) error", notName, shadow("err"), shadow("operand")),
		lang.If(lang.Expr("%s != nil", ref("err")), lang.Stmt("return nil"), nil),
		lang.Stmt(`return %s.New(%s.ErrNot, "want the value to fail %%s", %s)`,
			v, v, ref("operand")),
	)
}
