package rule

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

// RequiredOptions are the options of //kama:validate required. Required takes no arg.
type RequiredOptions struct{}

// Parse accepts only the empty arg.
func (Required) Parse(arg string) (Rule, error) {
	return RequiredOptions{}, noArg(arg)
}

// Check accepts the types the doc comment of Required lists.
func (RequiredOptions) Check(t types.Type) error {
	if _, _, ok := requiredCall(t, nil); !ok {
		return fmt.Errorf(
			"%s is not an option type, not nillable, has no method IsZero() bool declared"+
				" on it or promoted without a pointer or an interface, and is not comparable",
			t,
		)
	}

	return nil
}

// Emit calls the helper the test of t needs.
func (RequiredOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	helper, call, ok := requiredCall(t, v)
	if !ok {
		return nil, fmt.Errorf("required: unsupported type %s", t)
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
// it, or false when no test applies.
func requiredCall(t types.Type, v Gen) (helper string, call Gen, ok bool) {
	cond, got, ok := presence(t, v)

	switch {
	case !ok:
		return "", nil, false
	case got == "":
		return requiredComparableName, lang.Expr("%s(%s)", requiredComparableName, v), true
	}

	return requiredHelperName,
		lang.Expr("%s(%s, %s)", requiredHelperName, cond, strconv.Quote(got)),
		true
}

// presence returns the condition of the value v of t holding a value, as
// Required tests it, and what v is when it holds none. got is empty for a
// type compared with its zero value, whose condition calls
// kamaValidateNonZero. ok is false when no test applies. The order matters:
// an option type is comparable and has IsZero, and time.Time is comparable
// but tells its zero value by IsZero.
func presence(t types.Type, v Gen) (cond Gen, got string, ok bool) {
	switch {
	case typequery.IsNamed(t, typequery.OptPath, "Option"):
		return lang.Expr("%s.IsSome()", v), "none", true
	case typequery.IsNamed(t, typequery.UndPath, "Und"),
		typequery.IsNamed(t, typequery.ElasticPath, "Elastic"):
		return lang.Expr("%s.IsDefined()", v), "undefined or null", true
	}

	nonZero := lang.Expr("%s(%s)", nonZeroName, v)

	// The underlying type of a type parameter is its constraint interface,
	// which does not make its values nillable.
	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		return nonZero, "", types.Comparable(t)
	}

	if nillable(t) {
		return lang.Expr("%s != nil", v), "nil", true
	}

	if hasMethod(t, "IsZero", types.Typ[types.Bool]) {
		return lang.Expr("!%s.IsZero()", v), "the zero value", true
	}

	return nonZero, "", types.Comparable(t)
}

// presenceCond returns the condition presence gives for the value v of t,
// declaring the helper it calls.
func presenceCond(e Emitter, t types.Type, v Gen) (Gen, error) {
	cond, got, ok := presence(t, v)
	if !ok {
		return nil, fmt.Errorf("%s has no presence test", t)
	}

	if got == "" {
		e.Declare(nonZeroName, nonZeroDecl)
	}

	return cond, nil
}

const (
	requiredComparableName = "requiredComparable"
	requiredHelperName     = "kamaValidateRequired"
	nonZeroName            = "kamaValidateNonZero"
)

func nonZeroDecl(Importer) Step {
	return lang.Func(
		lang.Expr("func %s[%s comparable](%s %s) bool",
			nonZeroName, shadow("T"), shadow("t"), ref("T")),
		lang.Stmt("var %s %s", shadow("zero"), ref("T")),
		lang.Stmt("return %s != %s", ref("t"), ref("zero")),
	)
}

func requiredComparableDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr("func %s[%s comparable](%s %s) error",
			requiredComparableName, shadow("T"), shadow("t"), ref("T")),
		lang.Stmt("var %s %s", shadow("zero"), ref("T")),
		lang.If(
			lang.Expr("%s == %s", ref("t"), ref("zero")),
			lang.Stmt(`return %s.New(%s.ErrRequired, "want a non-zero value, got %%#v", %s)`,
				v, v, ref("t")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}

func requiredHelperDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s bool, %s string) error",
			requiredHelperName, shadow("ok"), shadow("got")),
		lang.If(
			lang.Expr("!%s", ref("ok")),
			lang.Stmt(`return %s.New(%s.ErrRequired, "want a value, got %%s", %s)`,
				v, v, ref("got")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
