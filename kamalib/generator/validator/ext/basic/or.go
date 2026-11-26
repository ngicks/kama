package basic

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// Or is the validator //kama:validate or(<validator>, ...): at least one
// operand holds, as JSON Schema anyOf tests. or() without operands never
// holds, as the JSON Schema false does.
//
// It applies to a type every operand applies to. Its error wraps
// validator.ErrOr and carries the error of every operand as a cause,
// which validator.Causes returns.
type Or struct{}

// Name returns "or".
func (Or) Name() string { return "or" }

// OrOptions are the options of //kama:validate or(<validator>, ...).
type OrOptions struct {
	// Operands are the validators of the args, in order.
	Operands []Operand
}

// Parse decodes arg into operands through r.
func (Or) Parse(arg string, r ext.Resolver) (ext.Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	return combine(ops, ownErrors, func(ops []Operand) ext.Rule { return OrOptions{Operands: ops} })
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
