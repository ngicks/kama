package basic

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Length is the validator //kama:validate length(...): the length of the
// value satisfies every comparison, as in length(ge=1, le=10) or length(3).
// The keywords gt, ge, lt, le, eq and ne compare with >, >=, <, <=, == and
// !=, one positional arg compares with ==, and ne takes a list too. Each
// value is a non-negative integer.
//
// It applies to types of a string, slice, array, map or channel underlying
// type, and to a type an ext.LengthTester of the registry claims, as
// elastic.Elastic. The length of a string is its number of bytes. On raw JSON text,
// as IsRawJson tells, it counts the elements of an array and passes a
// value of another kind.
type Length struct{}

// Name returns "length".
func (Length) Name() string { return "length" }

// LengthOptions are the options of //kama:validate length(...).
type LengthOptions struct {
	// Comparisons holds one comparison per arg, in order.
	Comparisons []Comparison
	// reg is the registry whose length testers tell the length of a type
	// they claim.
	reg ext.Resolver
}

// Parse decodes the args of c into comparisons with non-negative integers.
func (Length) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	cs, err := parseLengths(r, c)
	if err != nil {
		return nil, err
	}

	return LengthOptions{Comparisons: cs, reg: r}, nil
}

// Check accepts the types the doc comment of Length lists.
func (o LengthOptions) Check(t types.Type) error {
	if _, ok := ext.Length(o.reg, t, nil); !ok && !hasLength(t) {
		return fmt.Errorf("%s is not a string, slice, array, map or channel type", t)
	}

	return nil
}

// Emit calls kamaValidateLength once per comparison.
func (o LengthOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	if ext.IsRawJson(t) {
		return rawComparisons(e, "Items", v, o.Comparisons, false), nil
	}

	e.Declare(compareName, compareDecl)
	e.Declare(lengthName, lengthDecl)

	n, ok := ext.Length(o.reg, t, v)
	if !ok {
		n = lang.Expr("len(%s)", v)
	}

	return joinCalls(e, o.Comparisons, func(c Comparison) ext.Gen {
		return lang.Expr("%s(%s, %s, %s)", lengthName, n, strconv.Quote(c.Op), c.Operand)
	}), nil
}

// hasLength reports whether len applies to a value of t.
func hasLength(t types.Type) bool {
	if isStringType(t) {
		return true
	}

	if _, isParam := types.Unalias(t).(*types.TypeParam); isParam {
		return false
	}

	switch t.Underlying().(type) {
	case *types.Slice, *types.Array, *types.Map, *types.Chan:
		return true
	}

	return false
}

const lengthName = "kamaValidateLength"

func lengthDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s int, %s string, %s int) error",
			lengthName, shadow("got"), shadow("op"), shadow("want")),
		lang.If(
			lang.Expr("!%s(%s, %s, %s)", compareName, ref("got"), ref("op"), ref("want")),
			lang.Stmt(`return %s.New(%s.ErrLength, "want length %%s %%d, got %%d", %s, %s, %s)`,
				v, v, ref("op"), ref("want"), ref("got")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
