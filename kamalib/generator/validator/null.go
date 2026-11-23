package validator

import (
	"fmt"
	"go/types"
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

// Check parses arg and checks t with the options.
func (r Null) Check(t types.Type, arg string) error {
	o, err := r.Parse(arg)
	if err != nil {
		return err
	}

	return o.Check(t)
}

// Emit parses arg and emits with the options.
func (r Null) Emit(e Emitter, t types.Type, v string, arg string) (string, error) {
	o, err := r.Parse(arg)
	if err != nil {
		return "", err
	}

	return o.Emit(e, t, v)
}

// NullOptions are the options of //kama:validate null. Null takes no arg.
type NullOptions struct{}

// Parse accepts only the empty arg.
func (Null) Parse(arg string) (NullOptions, error) {
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
func (NullOptions) Emit(e Emitter, _ types.Type, v string) (string, error) {
	e.Declare(nullHelperName, nullHelperDecl)
	return nullHelperName + "(" + v + ".IsNull())", nil
}

const nullHelperName = "kamaValidateNull"

func nullHelperDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateNull(ok bool) error {
	if !ok {
		return %[1]s.New(%[1]s.ErrNull, "want null, got a non-null value")
	}
	return nil
}`, v), nil
}
