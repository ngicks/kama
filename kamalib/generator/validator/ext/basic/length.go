package basic

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
)

// Length is the validator //kama:validate length="<op><n> ...": len of
// the value satisfies every comparison, as in >=1 <=10 or ==3. The operators
// are >, >=, ==, !=, <= and <, and n is a non-negative integer literal.
// n may carry a unary plus.
//
// It applies to types of a string, slice, array, map or channel underlying
// type. The length of a string is its number of bytes. On raw JSON text,
// as IsRawJson tells, it counts the elements of an array and passes a
// value of another kind.
type Length struct{}

// Name returns "length".
func (Length) Name() string { return "length" }

// LengthOptions are the options of //kama:validate length="<op><n> ...".
type LengthOptions struct {
	// Comparisons holds one comparison per token of the arg, in order.
	Comparisons []Comparison
}

// Parse decodes arg into comparisons with non-negative integers.
func (Length) Parse(arg string, _ ext.Resolver) (ext.Rule, error) {
	cs, err := parseLengths(arg)
	if err != nil {
		return nil, err
	}

	return LengthOptions{Comparisons: cs}, nil
}

// Check accepts the types the doc comment of Length lists.
func (LengthOptions) Check(t types.Type) error {
	if !hasLength(t) {
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

	return joinCalls(e, o.Comparisons, func(c Comparison) ext.Gen {
		return lang.Expr("%s(len(%s), %s, %s)", lengthName, v, strconv.Quote(c.Op), c.Operand)
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
