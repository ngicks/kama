package validator

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Zero is the validator //kama:validate:zero: the value is the zero value.
//
// It applies to the types Required applies to. A pointer, slice, map,
// channel, function or interface must be nil. A type with a method
// IsZero() bool, the option types among them, must be zero by it: an
// opt.Option must be none, an und.Und or an elastic.Elastic undefined. Any
// other comparable type must equal its zero value.
type Zero struct{}

// Name returns "zero".
func (Zero) Name() string { return "zero" }

// Check accepts the types the doc comment of Zero lists, without args.
func (Zero) Check(t types.Type, args []dircmnt.KeyValue) error {
	if len(args) > 0 {
		return fmt.Errorf("unknown args %q; want none", spellArgs(args))
	}

	if _, _, ok := zeroCall(t, ""); !ok {
		return fmt.Errorf(
			"%s is not nillable, has no method IsZero() bool and is not comparable", t,
		)
	}

	return nil
}

// Emit calls the helper the test of t needs.
func (Zero) Emit(e Emitter, t types.Type, v string, _ []dircmnt.KeyValue) (string, error) {
	helper, call, ok := zeroCall(t, v)
	if !ok {
		return "", fmt.Errorf("zero: unsupported type %s", t)
	}

	switch helper {
	case zeroComparableName:
		e.Declare(helper, zeroComparableDecl)
	default:
		e.Declare(helper, zeroHelperDecl)
	}

	return call, nil
}

// zeroCall returns the helper testing that the value v of t is zero and the
// call of it, or false when no test applies. It tests in the order of
// requiredCall, without the option types, which tell their zero value by
// IsZero.
func zeroCall(t types.Type, v string) (helper, call string, ok bool) {
	test := func(cond, got string) (string, string, bool) {
		return zeroHelperName, zeroHelperName + "(" + cond + ", " + strconv.Quote(got) + ")", true
	}

	comparable := func() (string, string, bool) {
		return zeroComparableName, zeroComparableName + "(" + v + ")", true
	}

	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		if types.Comparable(t) {
			return comparable()
		}

		return "", "", false
	}

	if nillable(t) {
		return test(v+" == nil", "a non-nil value")
	}

	if hasMethod(t, "IsZero", types.Typ[types.Bool]) {
		return test(v+".IsZero()", "a non-zero value")
	}

	if types.Comparable(t) {
		return comparable()
	}

	return "", "", false
}

const (
	zeroComparableName = "kamaValidateZeroComparable"
	zeroHelperName     = "kamaValidateZero"
)

func zeroComparableDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateZeroComparable[T comparable](t T) error {
	var zero T
	if t != zero {
		return %[1]s.New(%[1]s.ErrZero, "want the zero value, got %%#v", t)
	}
	return nil
}`, v), nil
}

func zeroHelperDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateZero(ok bool, got string) error {
	if !ok {
		return %[1]s.New(%[1]s.ErrZero, "want the zero value, got %%s", got)
	}
	return nil
}`, v), nil
}
