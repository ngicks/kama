package validator

import (
	"fmt"
	"go/types"
)

// Nil is the validator //kama:validate nil: the value is nil.
//
// It applies to pointer, slice, map, channel, function and interface
// types.
type Nil struct{}

// Name returns "nil".
func (Nil) Name() string { return "nil" }

// Check accepts nillable types, without an argument.
func (Nil) Check(t types.Type, arg string) error {
	if err := noArg(arg); err != nil {
		return err
	}

	if !nillable(t) {
		return fmt.Errorf("%s is not nillable", t)
	}

	return nil
}

// Emit calls kamaValidateNil.
func (Nil) Emit(e Emitter, _ types.Type, v string, _ string) (string, error) {
	e.Declare(nilHelperName, nilHelperDecl)
	return nilHelperName + "(" + v + " == nil)", nil
}

const nilHelperName = "kamaValidateNil"

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
