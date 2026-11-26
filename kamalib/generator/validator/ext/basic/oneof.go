package basic

import (
	"go/types"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// OneOf is the validator //kama:validate oneof(<validator>, ...): exactly
// one operand holds, as JSON Schema oneOf tests. oneof() without operands
// never holds.
//
// It applies to a type every operand applies to. Its error wraps
// validator.ErrOneOf. When no operand holds, it carries the error of
// every operand as a cause, which validator.Causes returns. When several
// hold, it spells them.
type OneOf struct{}

// Name returns "oneof". JSON Schema calls the keyword oneOf. xor of more
// than two operands would mean an odd number of them, not exactly one.
func (OneOf) Name() string { return "oneof" }

// OneOfOptions are the options of //kama:validate oneof(<validator>, ...).
type OneOfOptions struct {
	// Operands are the validators of the args, in order.
	Operands []Operand
}

// Parse resolves the args of c, the operands, through r.
func (OneOf) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	ops, err := operandArgs.Bind(r, c, nil)
	if err != nil {
		return nil, err
	}

	return combine(
		r,
		argValues(ops),
		ownErrors,
		func(ops []Operand) ext.Rule { return OneOfOptions{Operands: ops} },
	)
}

// Check accepts a type every operand accepts.
func (o OneOfOptions) Check(t types.Type) error { return checkOperands(o.Operands, t) }

// Emit passes the spellings and the errors of the operands to
// kamaValidateOneOf.
func (o OneOfOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	errs, err := emitOperands(e, o.Operands, t, v)
	if err != nil {
		return nil, err
	}

	e.Declare(oneOfName, oneOfDecl)

	args := append(
		[]ext.Gen{lang.Expr("[]string{%s}", strings.Join(texts(o.Operands), ", "))},
		errs...)

	return lang.Expr("%s(%s)", oneOfName, lang.Join(", ", args...)), nil
}

// Annotate is Emit recording into ev what each operand holding evaluated.
func (o OneOfOptions) Annotate(e ext.Emitter, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	errs, err := branchOperands(e, o.Operands, t, v, ev)
	if err != nil {
		return nil, err
	}

	e.Declare(oneOfName, oneOfDecl)

	args := append(
		[]ext.Gen{lang.Expr("[]string{%s}", strings.Join(texts(o.Operands), ", "))},
		errs...)

	return lang.Expr("%s(%s)", oneOfName, lang.Join(", ", args...)), nil
}

const oneOfName = "kamaValidateOneOf"

func oneOfDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s []string, %s ...error) error",
			oneOfName, shadow("operands"), shadow("errs")),
		lang.Stmt("var %s []string", shadow("held")),
		// For renders its clause in the scope of the loop, which the range
		// variables are declared in.
		lang.For(nil, lang.Expr("%s, %s := range %s", shadow("i"), shadow("err"), ref("errs")), nil,
			lang.If(
				lang.Expr("%s == nil", ref("err")),
				lang.Stmt(
					"%s = append(%s, %s[%s])",
					ref("held"),
					ref("held"),
					ref("operands"),
					ref("i"),
				),
				nil,
			),
		),
		lang.If(lang.Expr("len(%s) == 1", ref("held")), lang.Stmt("return nil"), nil),
		lang.If(
			lang.Expr("len(%s) == 0", ref("held")),
			lang.Stmt(
				`return %s.Compose(%s.ErrOneOf, %s, "none of %%d alternatives holds", len(%s))`,
				v, v, ref("errs"), ref("errs"),
			),
			nil,
		),
		lang.Stmt(
			"return %s.New(%s.ErrOneOf,\n"+
				"\"want exactly one of %%d alternatives to hold, got %%d: %%s\",\n"+
				"len(%s), len(%s), %s.Join(%s, \", \"))",
			v,
			v,
			ref("errs"),
			ref("held"),
			imp.Import("strings"),
			ref("held"),
		),
	)
}
