package rule

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

// NilOptions are the options of //kama:validate nil. Nil takes no arg.
type NilOptions struct{}

// Parse accepts only the empty arg.
func (Nil) Parse(arg string) (Rule, error) {
	return NilOptions{}, noArg(arg)
}

// Check accepts nillable types.
func (NilOptions) Check(t types.Type) error {
	if !nillable(t) {
		return fmt.Errorf("%s is not nillable", t)
	}

	return nil
}

// Emit calls kamaValidateNil.
func (NilOptions) Emit(e Emitter, _ types.Type, v string) (string, error) {
	e.Declare(nilHelperName, nilHelperDecl)
	return nilHelperName + "(" + v + " == nil)", nil
}

const nilHelperName = "kamaValidateNil"

func nilHelperDecl(s Spelling) (string, error) {
	v, err := s.Import(ValidationPath)
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
