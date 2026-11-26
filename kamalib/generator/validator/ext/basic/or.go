package basic

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Or gives the meaning of the operator or: at least one operand holds, as
// JSON Schema anyOf tests. never, the validator without operands, never
// holds, as the JSON Schema false does.
//
// It applies to a type every operand applies to. Its error wraps
// validator.ErrOr and carries the error of every operand as a cause,
// which validator.Causes returns.
func Or(n expr.Node, r ext.Resolver) (ext.Rule, error) {
	return combine(
		r,
		n.(expr.Or).Operands,
		ownErrors,
		func(ops []Operand) ext.Rule { return OrOptions{Operands: ops} },
	)
}

// OrOptions are the operands of a or b.
type OrOptions struct {
	// Operands are the operands, in order.
	Operands []Operand
}

// Never is the validator //kama:validate never, which never holds. It
// applies to every type, and its error wraps validator.ErrOr.
type Never struct{}

// Name returns "never".
func (Never) Name() string { return "never" }

// Parse takes no args.
func (Never) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	return OrOptions{}, noArgs(r, c)
}

// Check accepts a type every operand accepts.
func (o OrOptions) Check(t types.Type) error { return checkOperands(o.Operands, t) }

// Emit passes the errors of the operands to kamaValidateOr.
func (o OrOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	errs, err := emitOperands(e, o.Operands, t, v)
	if err != nil {
		return nil, err
	}

	e.Declare(orName, orDecl)

	return lang.Expr("%s(%s)", orName, lang.Join(", ", errs...)), nil
}

// Annotate is Emit recording into ev what each operand holding evaluated.
func (o OrOptions) Annotate(e ext.Emitter, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	errs, err := branchOperands(e, o.Operands, t, v, ev)
	if err != nil {
		return nil, err
	}

	e.Declare(orName, orDecl)

	return lang.Expr("%s(%s)", orName, lang.Join(", ", errs...)), nil
}

const orName = "kamaValidateOr"

func orDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s ...error) error", orName, shadow("errs")),
		// For renders its clause in the scope of the loop, which the range
		// variable is declared in.
		lang.For(nil, lang.Expr("_, %s := range %s", shadow("err"), ref("errs")), nil,
			lang.If(lang.Expr("%s == nil", ref("err")), lang.Stmt("return nil"), nil),
		),
		lang.Stmt(`return %s.Compose(%s.ErrOr, %s, "none of %%d alternatives holds", len(%s))`,
			v, v, ref("errs"), ref("errs")),
	)
}
