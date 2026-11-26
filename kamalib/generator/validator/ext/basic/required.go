package basic

import (
	"fmt"
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Required is the validator //kama:validate required: the value is not the
// zero value.
//
// A type an ext.PresenceTester of the registry claims must hold a value as
// it tells. A pointer, slice, map, channel, function or interface must
// not be nil. A type with a method IsZero() bool must not be zero by it.
// Any other comparable type must not equal its zero value. IsZero counts
// when the type declares it or embedded fields promote it without a
// pointer or an interface on the way.
//
// On raw JSON text, as IsRawJson tells, it takes the names of members as
// its arg, as in required="a b", and tests an object holds each of them,
// as JSON Schema required does, passing a value of another kind. Without
// names it tests the text is not nil. Names apply to raw JSON text only.
type Required struct{}

// Name returns "required".
func (Required) Name() string { return "required" }

// RequiredOptions are the options of //kama:validate required.
type RequiredOptions struct {
	// Names are the member names of the arg, in order, for raw JSON text.
	Names []string

	// reg is the registry the options were parsed with, whose type supports
	// tell presence.
	reg ext.Resolver
}

// Parse decodes the args of c, the member names of raw JSON text.
func (Required) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	var args struct {
		Names []string `json:"names"`
	}

	if _, err := (ext.Params{Variadic: "names"}).Bind(r, c, &args); err != nil {
		return nil, err
	}

	return RequiredOptions{Names: args.Names, reg: r}, nil
}

// Check accepts the types the doc comment of Required lists.
func (o RequiredOptions) Check(t types.Type) error {
	if len(o.Names) > 0 {
		if !ext.IsRawJson(t) {
			return fmt.Errorf(
				"%s is no raw JSON text, which the member names of required apply to",
				t,
			)
		}

		return nil
	}

	if _, _, ok := requiredCall(o.reg, t, nil); !ok {
		return fmt.Errorf(
			"%s is not an option type, not nillable, has no method IsZero() bool declared"+
				" on it or promoted without a pointer or an interface, and is not comparable",
			t,
		)
	}

	return nil
}

// Emit calls the helper the test of t needs, rawjson.Required for names.
func (o RequiredOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	if len(o.Names) > 0 {
		names := opiter.WrapSeq(slices.Values(o.Names)).
			Map(func(n string) ext.Gen { return lang.Expr("%s", strconv.Quote(n)) }).
			CollectSlice()

		return rawCall(e, "Required", v, append([]ext.Gen{e.StopOnFirstError()}, names...)...), nil
	}

	helper, call, ok := requiredCall(o.reg, t, v)
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
func requiredCall(
	reg ext.Resolver,
	t types.Type,
	v ext.Gen,
) (helper string, call ext.Gen, ok bool) {
	cond, got, ok := presence(reg, t, v)

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
// a type an ext.PresenceTester of reg claims may be comparable and have
// IsZero, and time.Time is comparable but tells its zero value by IsZero.
func presence(reg ext.Resolver, t types.Type, v ext.Gen) (cond ext.Gen, got string, ok bool) {
	if cond, absent, ok := ext.Presence(reg, t, v); ok {
		return cond, absent, true
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
func presenceCond(e ext.Emitter, reg ext.Resolver, t types.Type, v ext.Gen) (ext.Gen, error) {
	cond, got, ok := presence(reg, t, v)
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

func nonZeroDecl(ext.Importer) ext.Step {
	return lang.Func(
		lang.Expr("func %s[%s comparable](%s %s) bool",
			nonZeroName, shadow("T"), shadow("t"), ref("T")),
		lang.Stmt("var %s %s", shadow("zero"), ref("T")),
		lang.Stmt("return %s != %s", ref("t"), ref("zero")),
	)
}

func requiredComparableDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

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

func requiredHelperDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

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
