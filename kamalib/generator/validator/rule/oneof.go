package rule

import (
	"go/types"
	"strings"
)

// OneOf is the validator //kama:validate oneof(<validator>, ...): exactly
// one operand holds, as JSON Schema oneOf tests. oneof() without operands
// never holds.
//
// It applies to a type every operand applies to. Its error wraps
// validation.ErrOneOf. When no operand holds, it carries the error of
// every operand as a cause, which validation.Causes returns. When several
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

// Parse decodes arg into operands through r.
func (OneOf) Parse(arg string, r Resolver) (Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	return combine(ops, ownErrors, func(ops []Operand) Rule { return OneOfOptions{Operands: ops} })
}

// Check accepts a type every operand accepts.
func (o OneOfOptions) Check(t types.Type) error { return checkOperands(o.Operands, t) }

// Emit passes the spellings and the errors of the operands to
// kamaValidateOneOf.
func (o OneOfOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	errs, err := emitOperands(e, o.Operands, t, v)
	if err != nil {
		return nil, err
	}

	e.Declare(oneOfName, oneOfDecl)

	args := append([]Gen{lang.Expr("[]string{%s}", strings.Join(texts(o.Operands), ", "))}, errs...)

	return lang.Expr("%s(%s)", oneOfName, lang.Join(", ", args...)), nil
}

const oneOfName = "kamaValidateOneOf"

func oneOfDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

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
