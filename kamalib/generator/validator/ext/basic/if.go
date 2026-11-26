package basic

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// If gives the meaning of the conditional then if cond else otherwise:
// when the operand cond holds, the operand then must hold, otherwise the
// operand otherwise must, as JSON Schema if, then and else test. The
// error of cond never surfaces; it only tells whether cond holds. Write
// always for a branch that always holds and never for one that never
// does. A value failing cond passes when otherwise is always.
//
// It applies to a type every operand applies to. It returns the error of
// the branch taken.
func If(n expr.Node, r ext.Resolver) (ext.Rule, error) {
	c := n.(expr.Cond)

	return combine(
		r,
		[]expr.Node{c.If, c.Then, c.Else},
		operandErrors,
		func(ops []Operand) ext.Rule {
			o := IfOptions{Cond: ops[0], Then: ops[1]}
			if !isAlways(ops[2].Rule) {
				o.Else = &ops[2]
			}

			return o
		},
	)
}

// IfOptions are the operands of then if cond else otherwise.
type IfOptions struct {
	Cond, Then Operand
	// Else is nil when otherwise is always, which a value failing cond
	// passes without a check.
	Else *Operand
}

// isAlways reports whether r is the rule of always, which a combinator
// bound to the siblings of a field may have wrapped.
func isAlways(r ext.Rule) bool {
	switch x := r.(type) {
	case atField:
		return isAlways(x.rule)
	case onHeld:
		return isAlways(x.rule)
	case AndOptions:
		return len(x.Operands) == 0
	}

	return false
}

// Check accepts a type every operand accepts.
func (o IfOptions) Check(t types.Type) error { return checkOperands(o.operands(), t) }

// Emit passes the errors of the operands to kamaValidateIf, nil for an
// absent else.
func (o IfOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	errs, err := emitOperands(e, o.operands(), t, v)
	if err != nil {
		return nil, err
	}

	if o.Else == nil {
		errs = append(errs, lang.Expr("nil"))
	}

	e.Declare(ifName, ifDecl)

	return lang.Expr("%s(%s)", ifName, lang.Join(", ", errs...)), nil
}

// Annotate calls rawjson.If, which runs only the branch taken, so that only
// it records into ev, and cond records when it holds.
func (o IfOptions) Annotate(e ext.Emitter, t types.Type, v, ev ext.Gen) (ext.Gen, error) {
	args := []ext.Gen{ev}

	for _, op := range o.operands() {
		f, err := scopeFunc(e, []Operand{op}, t, v)
		if err != nil {
			return nil, err
		}

		args = append(args, f)
	}

	if o.Else == nil {
		args = append(args, lang.Expr("nil"))
	}

	return lang.Expr("%s.If(%s)", e.Import(ext.RawJsonPath), lang.Join(", ", args...)), nil
}

// operands returns cond, then and else when o has one.
func (o IfOptions) operands() []Operand {
	if o.Else == nil {
		return []Operand{o.Cond, o.Then}
	}

	return []Operand{o.Cond, o.Then, *o.Else}
}

const ifName = "kamaValidateIf"

func ifDecl(ext.Importer) ext.Step {
	return lang.Func(
		lang.Expr("func %s(%s, %s, %s error) error",
			ifName, shadow("cond"), shadow("then"), shadow("otherwise")),
		lang.If(lang.Expr("%s == nil", ref("cond")), lang.Stmt("return %s", ref("then")), nil),
		lang.Stmt("return %s", ref("otherwise")),
	)
}
