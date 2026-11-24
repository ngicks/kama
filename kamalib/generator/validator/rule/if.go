package rule

import (
	"fmt"
	"go/types"
)

// If is the validator //kama:validate if(<cond>, <then>[, <else>]): when
// the operand cond holds, the operand then must hold, otherwise the operand
// else must, as JSON Schema if, then and else test. Without else a value
// failing cond passes. The error of cond never surfaces; it only tells
// whether cond holds. Write and() for a branch that always holds and or()
// for one that never does.
//
// It applies to a type every operand applies to. It returns the error of
// the branch taken.
type If struct{}

// Name returns "if".
func (If) Name() string { return "if" }

// IfOptions are the options of //kama:validate if(<cond>, <then>[, <else>]).
type IfOptions struct {
	Cond, Then Operand
	// Else is nil when the args give no else.
	Else *Operand
}

// Parse decodes arg into two or three operands through r.
func (If) Parse(arg string, r Resolver) (Rule, error) {
	ops, err := ParseOperands(arg, r)
	if err != nil {
		return nil, err
	}

	if len(ops) != 2 && len(ops) != 3 {
		return nil, argsError(fmt.Errorf(
			"want the operands cond, then and an optional else, got %d operands", len(ops),
		))
	}

	return combine(ops, operandErrors, func(ops []Operand) Rule {
		o := IfOptions{Cond: ops[0], Then: ops[1]}
		if len(ops) == 3 {
			o.Else = &ops[2]
		}

		return o
	})
}

// Check accepts a type every operand accepts.
func (o IfOptions) Check(t types.Type) error { return checkOperands(o.operands(), t) }

// Emit passes the errors of the operands to kamaValidateIf, nil for an
// absent else.
func (o IfOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
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

// operands returns cond, then and else when o has one.
func (o IfOptions) operands() []Operand {
	if o.Else == nil {
		return []Operand{o.Cond, o.Then}
	}

	return []Operand{o.Cond, o.Then, *o.Else}
}

const ifName = "kamaValidateIf"

func ifDecl(Importer) Step {
	return lang.Func(
		lang.Expr("func %s(%s, %s, %s error) error",
			ifName, shadow("cond"), shadow("then"), shadow("otherwise")),
		lang.If(lang.Expr("%s == nil", ref("cond")), lang.Stmt("return %s", ref("then")), nil),
		lang.Stmt("return %s", ref("otherwise")),
	)
}
