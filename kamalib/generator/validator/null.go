package validator

import (
	"fmt"
	"go/types"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Nil is the validator //kama:validate:nil: the value is nil.
//
// It applies to pointer, slice, map, channel, function and interface
// types.
type Nil struct{}

// Name returns "nil".
func (Nil) Name() string { return "nil" }

// Check accepts nillable types, without args.
func (Nil) Check(t types.Type, args []dircmnt.KeyValue) error {
	if len(args) > 0 {
		return argsError(fmt.Errorf("unknown args %q; want none", spellArgs(args)))
	}

	if !nillable(t) {
		return fmt.Errorf("%s is not nillable", t)
	}

	return nil
}

// Emit calls kamaValidateNil.
func (Nil) Emit(e Emitter, _ types.Type, v string, _ []dircmnt.KeyValue) (string, error) {
	e.Declare(nilHelperName, nilHelperDecl)
	return nilHelperName + "(" + v + " == nil)", nil
}

// Null is the validator //kama:validate:null: the value is null by its
// method IsNull() bool, as an und.Und or an elastic.Elastic tells.
//
// It applies to types with that method other than pointer, interface and
// type parameter types, whose nil value cannot call it. A pointer to a type
// with the method gets the validator on the value it points to.
type Null struct{}

// Name returns "null".
func (Null) Name() string { return "null" }

// Check accepts types with a method IsNull() bool that are not nillable,
// without args.
func (Null) Check(t types.Type, args []dircmnt.KeyValue) error {
	if len(args) > 0 {
		return argsError(fmt.Errorf("unknown args %q; want none", spellArgs(args)))
	}

	_, isParam := types.Unalias(t).(*types.TypeParam)

	switch {
	case isParam:
		return fmt.Errorf("%s is a type parameter, which may be nil", t)
	case nillable(t):
		return fmt.Errorf("%s is nillable, and nil cannot tell IsNull", t)
	case !hasMethod(t, "IsNull", types.Typ[types.Bool]):
		return fmt.Errorf("%s has no method IsNull() bool", t)
	}

	return nil
}

// Emit calls kamaValidateNull with the result of IsNull.
func (Null) Emit(e Emitter, _ types.Type, v string, _ []dircmnt.KeyValue) (string, error) {
	e.Declare(nullHelperName, nullHelperDecl)
	return nullHelperName + "(" + v + ".IsNull())", nil
}

const (
	nilHelperName  = "kamaValidateNil"
	nullHelperName = "kamaValidateNull"
)

func nilHelperDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateNil(ok bool) error {
	if !ok {
		return %[1]s.New(%[1]s.ErrNil, "want nil, got a non-nil value")
	}
	return nil
}`, v), nil
}

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
