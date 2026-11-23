package rule

import (
	"fmt"
	"go/types"
	"strconv"
)

// Zero is the validator //kama:validate zero: the value is the zero value.
//
// It applies to the types Required applies to. A pointer, slice, map,
// channel, function or interface must be nil. A type with a method
// IsZero() bool, the option types among them, must be zero by it: an
// opt.Option must be none, an und.Und or an elastic.Elastic undefined. Any
// other comparable type must equal its zero value. IsZero counts when the
// type declares it or embedded fields promote it without a pointer or an
// interface on the way.
type Zero struct{}

// Name returns "zero".
func (Zero) Name() string { return "zero" }

// ZeroOptions are the options of //kama:validate zero. Zero takes no arg.
type ZeroOptions struct{}

// Parse accepts only the empty arg.
func (Zero) Parse(arg string) (Rule, error) {
	return ZeroOptions{}, noArg(arg)
}

// Check accepts the types the doc comment of Zero lists.
func (ZeroOptions) Check(t types.Type) error {
	if _, _, ok := zeroCall(t, nil); !ok {
		return fmt.Errorf(
			"%s is not nillable, has no method IsZero() bool declared on it or promoted"+
				" without a pointer or an interface, and is not comparable", t,
		)
	}

	return nil
}

// Emit calls the helper the test of t needs.
func (ZeroOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	helper, call, ok := zeroCall(t, v)
	if !ok {
		return nil, fmt.Errorf("zero: unsupported type %s", t)
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
func zeroCall(t types.Type, v Gen) (helper string, call Gen, ok bool) {
	test := func(cond Gen, got string) (string, Gen, bool) {
		return zeroHelperName,
			lang.Expr("%s(%s, %s)", zeroHelperName, cond, strconv.Quote(got)),
			true
	}

	comparable := func() (string, Gen, bool) {
		return zeroComparableName, lang.Expr("%s(%s)", zeroComparableName, v), true
	}

	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		if types.Comparable(t) {
			return comparable()
		}

		return "", nil, false
	}

	if nillable(t) {
		return test(lang.Expr("%s == nil", v), "a non-nil value")
	}

	if hasMethod(t, "IsZero", types.Typ[types.Bool]) {
		return test(lang.Expr("%s.IsZero()", v), "a non-zero value")
	}

	if types.Comparable(t) {
		return comparable()
	}

	return "", nil, false
}

const (
	zeroComparableName = "kamaValidateZeroComparable"
	zeroHelperName     = "kamaValidateZero"
)

func zeroComparableDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr("func %s[%s comparable](%s %s) error",
			zeroComparableName, shadow("T"), shadow("t"), ref("T")),
		lang.Stmt("var %s %s", shadow("zero"), ref("T")),
		lang.If(
			lang.Expr("%s != %s", ref("t"), ref("zero")),
			lang.Stmt(`return %s.New(%s.ErrZero, "want the zero value, got %%#v", %s)`,
				v, v, ref("t")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}

func zeroHelperDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s bool, %s string) error", zeroHelperName, shadow("ok"), shadow("got")),
		lang.If(
			lang.Expr("!%s", ref("ok")),
			lang.Stmt(`return %s.New(%s.ErrZero, "want the zero value, got %%s", %s)`,
				v, v, ref("got")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
