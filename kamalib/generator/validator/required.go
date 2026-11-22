package validator

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// Required is the validator //kama:validate:required: the value is not the
// zero value.
//
// An opt.Option must be some, an und.Und or an elastic.Elastic must be
// defined, and a pointer, slice, map, channel, function or interface must
// not be nil. A type with a method IsZero() bool must not be zero by it.
// Any other comparable type must not equal its zero value.
type Required struct{}

// Name returns "required".
func (Required) Name() string { return "required" }

// Check accepts the types the doc comment of Required lists, without args.
func (Required) Check(t types.Type, args []dircmnt.KeyValue) error {
	if len(args) > 0 {
		return argsError(fmt.Errorf("unknown args %q; want none", spellArgs(args)))
	}

	if _, _, ok := requiredCall(t, ""); !ok {
		return fmt.Errorf(
			"%s is not an option type, not nillable, has no method IsZero() bool"+
				" and is not comparable",
			t,
		)
	}

	return nil
}

// Emit calls the helper the test of t needs.
func (Required) Emit(e Emitter, t types.Type, v string, _ []dircmnt.KeyValue) (string, error) {
	helper, call, ok := requiredCall(t, v)
	if !ok {
		return "", fmt.Errorf("required: unsupported type %s", t)
	}

	switch helper {
	case requiredComparableName:
		e.Declare(helper, requiredComparableDecl)
	default:
		e.Declare(helper, requiredHelperDecl)
	}

	return call, nil
}

// requiredCall returns the helper testing the value v of t and the call of
// it, or false when no test applies. The order matters: an option type is
// comparable and has IsZero, and time.Time is comparable but tells its zero
// value by IsZero.
func requiredCall(t types.Type, v string) (helper, call string, ok bool) {
	present := func(cond, got string) (string, string, bool) {
		call := requiredHelperName + "(" + cond + ", " + strconv.Quote(got) + ")"
		return requiredHelperName, call, true
	}

	switch {
	case isNamed(t, optPath, "Option"):
		return present(v+".IsSome()", "none")
	case isNamed(t, undPath, "Und"), isNamed(t, elasticPath, "Elastic"):
		return present(v+".IsDefined()", "undefined or null")
	}

	comparable := func() (string, string, bool) {
		return requiredComparableName, requiredComparableName + "(" + v + ")", true
	}

	// The underlying type of a type parameter is its constraint interface,
	// which does not make its values nillable.
	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		if types.Comparable(t) {
			return comparable()
		}

		return "", "", false
	}

	if nillable(t) {
		return present(v+" != nil", "nil")
	}

	if hasMethod(t, "IsZero", types.Typ[types.Bool]) {
		return present("!"+v+".IsZero()", "the zero value")
	}

	if types.Comparable(t) {
		return comparable()
	}

	return "", "", false
}

// nillable reports whether the values of t can be nil: t is a pointer,
// slice, map, channel, function or interface type. The underlying type of
// a type parameter is its constraint interface, which does not make its
// values nillable.
func nillable(t types.Type) bool {
	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		return false
	}

	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature,
		*types.Interface:
		return true
	}

	return false
}

// hasMethod reports whether the method set of t has the method name taking
// no parameter and returning result.
func hasMethod(t types.Type, name string, result types.Type) bool {
	sel := types.NewMethodSet(t).Lookup(nil, name)
	if sel == nil {
		return false
	}

	sig := sel.Type().(*types.Signature)

	return sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
		types.Identical(sig.Results().At(0).Type(), result)
}

const (
	requiredComparableName = "requiredComparable"
	requiredHelperName     = "kamaValidateRequired"
)

func requiredComparableDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func requiredComparable[T comparable](t T) error {
	var zero T
	if t == zero {
		return %[1]s.New(%[1]s.ErrRequired, "want a non-zero value, got %%#v", t)
	}
	return nil
}`, v), nil
}

func requiredHelperDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateRequired(ok bool, got string) error {
	if !ok {
		return %[1]s.New(%[1]s.ErrRequired, "want a value, got %%s", got)
	}
	return nil
}`, v), nil
}
