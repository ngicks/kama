package basic

import (
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Not gives the meaning of the operator not: the one operand fails, as
// JSON Schema not tests.
//
// It applies to a type its operand applies to. Its error wraps
// validator.ErrNot and spells the operand.
func Not(n expr.Node, r ext.Resolver) (ext.Rule, error) {
	return combine(
		r,
		[]expr.Node{n.(expr.Not).Operand},
		ownErrors,
		func(ops []Operand) ext.Rule { return NotOptions{Operand: ops[0]} },
	)
}

// Negate returns the rule not n.
func Negate(n expr.Node) expr.Node { return expr.Not{Operand: n} }

// NotOptions are the operand of not a.
type NotOptions struct {
	// Operand is the operand.
	Operand Operand
}

// Check accepts a type the operand accepts.
func (o NotOptions) Check(t types.Type) error {
	return checkOperands([]Operand{o.Operand}, t)
}

// Emit passes the error of the operand to kamaValidateNot.
func (o NotOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	errs, err := emitOperands(e, []Operand{o.Operand}, t, v)
	if err != nil {
		return nil, err
	}

	e.Declare(notName, notDecl)

	return lang.Expr("%s(%s, %s)", notName, errs[0], strconv.Quote(o.Operand.Text)), nil
}

const notName = "kamaValidateNot"

func notDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s error, %s string) error", notName, shadow("err"), shadow("operand")),
		lang.If(lang.Expr("%s != nil", ref("err")), lang.Stmt("return nil"), nil),
		lang.Stmt(`return %s.New(%s.ErrNot, "want the value to fail %%s", %s)`,
			v, v, ref("operand")),
	)
}
