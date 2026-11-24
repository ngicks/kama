package rule

import (
	"fmt"
	"go/types"

	"github.com/ngicks/opiter"
)

// Unique is the validator //kama:validate unique: no two elements of the
// value are equal.
//
// It applies to types of a slice or array underlying type. Elements
// compare by value. Elements of a comparable type holding no pointer,
// interface or type parameter compare with == through a map, with NaN
// unequal to itself. Other elements compare pairwise with
// reflect.DeepEqual in quadratic time, which follows pointers. A map would
// compare pointers by address, and it would panic on an interface holding
// an incomparable value, as an element of []any may.
type Unique struct{}

// Name returns "unique".
func (Unique) Name() string { return "unique" }

// UniqueOptions are the options of //kama:validate unique. Unique takes no arg.
type UniqueOptions struct{}

// Parse accepts only the empty arg.
func (Unique) Parse(arg string) (Rule, error) {
	return UniqueOptions{}, noArg(arg)
}

// Check accepts the types the doc comment of Unique lists.
func (UniqueOptions) Check(t types.Type) error {
	if _, ok := uniqueHelper(t); !ok {
		return fmt.Errorf("%s is not a slice or array type", t)
	}

	return nil
}

// Emit calls kamaValidateUnique or kamaValidateUniqueDeep with the
// elements.
func (UniqueOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	helper, ok := uniqueHelper(t)
	if !ok {
		return nil, fmt.Errorf("unique: unsupported type %s", t)
	}

	switch helper {
	case uniqueName:
		e.Declare(helper, uniqueDecl)
	default:
		e.Declare(helper, uniqueDeepDecl)
	}

	if _, isArray := t.Underlying().(*types.Array); isArray {
		// An array expression such as o.Value() is not addressable, so it
		// is sliced through a pointer to a copy.
		e.Declare(copyName, copyDecl)

		return lang.Expr("%s(%s(%s)[:])", helper, copyName, v), nil
	}

	return lang.Expr("%s(%s)", helper, v), nil
}

// uniqueHelper returns the helper testing the elements of t, or false when
// t is not a slice or array type.
func uniqueHelper(t types.Type) (string, bool) {
	var elem types.Type

	switch u := t.Underlying().(type) {
	case *types.Slice:
		elem = u.Elem()
	case *types.Array:
		elem = u.Elem()
	default:
		return "", false
	}

	if valueComparable(elem) {
		return uniqueName, true
	}

	return uniqueDeepName, true
}

// valueComparable reports whether == compares values of t by value and
// never panics: t is comparable and holds no pointer, interface or type
// parameter.
func valueComparable(t types.Type) bool {
	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		return false
	}

	switch u := t.Underlying().(type) {
	case *types.Basic, *types.Chan:
		return true
	case *types.Array:
		return valueComparable(u.Elem())
	case *types.Struct:
		return opiter.WrapSeq(u.Fields()).
			Every(func(v *types.Var) bool { return valueComparable(v.Type()) })
	}

	return false
}

const (
	uniqueName     = "kamaValidateUnique"
	uniqueDeepName = "kamaValidateUniqueDeep"
	copyName       = "kamaValidateCopy"
)

func uniqueDecl(imp Importer) Step {
	return lang.Func(
		lang.Expr("func %s[%s comparable](%s []%s) error",
			uniqueName, shadow("T"), shadow("got"), ref("T")),
		lang.Stmt("%s := make(map[%s]int, len(%s))", shadow("seen"), ref("T"), ref("got")),
		// For renders its clause in the scope of the loop, which the range
		// variables are declared in.
		lang.For(nil, lang.Expr("%s, %s := range %s", shadow("i"), shadow("x"), ref("got")), nil,
			lang.Steps(
				lang.If(
					lang.Expr("%s, %s := %s[%s]; %s",
						shadow("j"), shadow("ok"), ref("seen"), ref("x"), ref("ok")),
					uniqueError(imp),
					nil,
				),
				lang.Stmt("%s[%s] = %s", ref("seen"), ref("x"), ref("i")),
			),
		),
		lang.Stmt("return nil"),
	)
}

func uniqueDeepDecl(imp Importer) Step {
	return lang.Func(
		lang.Expr("func %s[%s any](%s []%s) error",
			uniqueDeepName, shadow("T"), shadow("got"), ref("T")),
		lang.For(nil, lang.Expr("%s := range %s", shadow("i"), ref("got")), nil,
			lang.For(nil, lang.Expr("%s := range %s", shadow("j"), ref("i")), nil,
				lang.If(
					lang.Expr("%s.DeepEqual(%s[%s], %s[%s])",
						imp.Import("reflect"), ref("got"), ref("j"), ref("got"), ref("i")),
					uniqueError(imp),
					nil,
				),
			),
		),
		lang.Stmt("return nil"),
	)
}

// uniqueError is the statement of kamaValidateUnique and
// kamaValidateUniqueDeep returning the failure for the equal elements j
// and i.
func uniqueError(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Stmt(
		`return %s.New(%s.ErrUnique, "want unique elements, got equal elements at %%d and %%d",`+
			` %s, %s)`,
		v,
		v,
		ref("j"),
		ref("i"),
	)
}

func copyDecl(Importer) Step {
	return lang.Func(
		lang.Expr(
			"func %s[%s any](%s %s) *%s",
			copyName,
			shadow("T"),
			shadow("v"),
			ref("T"),
			ref("T"),
		),
		lang.Stmt("return &%s", ref("v")),
	)
}
