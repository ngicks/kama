package basic

import (
	"fmt"
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Null is the validator //kama:validate null: the value is null by its
// method IsNull() bool, or as an ext.NullTester of the registry tells, as
// for an und.Und or an elastic.Elastic.
//
// It applies to types with that method other than type parameters and
// nillable types: pointer, slice, map, channel, function and interface
// types. Their nil value cannot call the method. A pointer to a type with
// the method gets the validator on the value it points to. The method
// must be declared or promoted as for IsZero of Required.
type Null struct{}

// Name returns "null".
func (Null) Name() string { return "null" }

// NullOptions are the options of //kama:validate null. Null takes no arg.
type NullOptions struct {
	// reg is the registry the options were parsed with, whose type supports
	// tell null.
	reg ext.Resolver
}

// Parse takes no args.
func (Null) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	return NullOptions{reg: r}, noArgs(r, c)
}

// Check accepts the types an ext.NullTester claims and types with a method
// IsNull() bool that are not nillable.
func (o NullOptions) Check(t types.Type) error {
	if _, ok := ext.Null(o.reg, t, nil); ok {
		return nil
	}

	_, isParam := types.Unalias(t).(*types.TypeParam)

	switch {
	case isParam:
		return fmt.Errorf("%s is a type parameter, which may be nil", t)
	case nillable(t):
		return fmt.Errorf("%s is nillable, and nil cannot tell IsNull", t)
	case !hasMethod(t, "IsNull", types.Typ[types.Bool]):
		return fmt.Errorf(
			"%s has no method IsNull() bool declared on it or promoted"+
				" without a pointer or an interface", t,
		)
	}

	return nil
}

// Emit calls kamaValidateNull with the condition the ext.NullTester tells,
// else with the result of IsNull.
func (o NullOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	e.Declare(nullHelperName, nullHelperDecl)

	cond, ok := ext.Null(o.reg, t, v)
	if !ok {
		cond = lang.Expr("%s.IsNull()", v)
	}

	return lang.Expr("%s(%s)", nullHelperName, cond), nil
}

const nullHelperName = "kamaValidateNull"

func nullHelperDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s bool) error", nullHelperName, shadow("ok")),
		lang.If(
			lang.Expr("!%s", ref("ok")),
			lang.Stmt(`return %s.New(%s.ErrNull, "want null, got a non-null value")`, v, v),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
