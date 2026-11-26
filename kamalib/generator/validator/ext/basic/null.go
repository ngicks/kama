package basic

import (
	"fmt"
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// Null is the validator //kama:validate null: the value is null by its
// method IsNull() bool, as an und.Und or an elastic.Elastic tells.
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
type NullOptions struct{}

// Parse accepts only the empty arg.
func (Null) Parse(arg string, _ ext.Resolver) (ext.Rule, error) {
	return NullOptions{}, noArg(arg)
}

// Check accepts types with a method IsNull() bool that are not nillable.
func (NullOptions) Check(t types.Type) error {
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

// Emit calls kamaValidateNull with the result of IsNull.
func (NullOptions) Emit(e ext.Emitter, _ types.Type, v ext.Gen) (ext.Gen, error) {
	e.Declare(nullHelperName, nullHelperDecl)
	return lang.Expr("%s(%s.IsNull())", nullHelperName, v), nil
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
