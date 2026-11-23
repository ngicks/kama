package validator

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// Required is the validator //kama:validate required: the value is not the
// zero value.
//
// An opt.Option must be some, an und.Und or an elastic.Elastic must be
// defined, and a pointer, slice, map, channel, function or interface must
// not be nil. A type with a method IsZero() bool must not be zero by it.
// Any other comparable type must not equal its zero value. IsZero counts
// when the type declares it or embedded fields promote it without a
// pointer or an interface on the way.
type Required struct{}

// Name returns "required".
func (Required) Name() string { return "required" }

// Check parses arg and checks t with the options.
func (r Required) Check(t types.Type, arg string) error {
	o, err := r.Parse(arg)
	if err != nil {
		return err
	}

	return o.Check(t)
}

// Emit parses arg and emits with the options.
func (r Required) Emit(e Emitter, t types.Type, v string, arg string) (string, error) {
	o, err := r.Parse(arg)
	if err != nil {
		return "", err
	}

	return o.Emit(e, t, v)
}

// RequiredOptions are the options of //kama:validate required. Required takes no arg.
type RequiredOptions struct{}

// Parse accepts only the empty arg.
func (Required) Parse(arg string) (RequiredOptions, error) {
	return RequiredOptions{}, noArg(arg)
}

// Check accepts the types the doc comment of Required lists.
func (RequiredOptions) Check(t types.Type) error {
	if _, _, ok := requiredCall(t, ""); !ok {
		return fmt.Errorf(
			"%s is not an option type, not nillable, has no method IsZero() bool declared"+
				" on it or promoted without a pointer or an interface, and is not comparable",
			t,
		)
	}

	return nil
}

// Emit calls the helper the test of t needs.
func (RequiredOptions) Emit(e Emitter, t types.Type, v string) (string, error) {
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
	case typequery.IsNamed(t, typequery.OptPath, "Option"):
		return present(v+".IsSome()", "none")
	case typequery.IsNamed(t, typequery.UndPath, "Und"),
		typequery.IsNamed(t, typequery.ElasticPath, "Elastic"):
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
