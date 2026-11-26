package basic

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Runes is the validator //kama:validate runes(...): the number of
// runes of the value satisfies every comparison, as in runes(ge=1, le=10)
// or runes(3). It takes the args of length.
//
// It applies to types of a string underlying type. utf8.RuneCountInString
// counts the runes, so an invalid UTF-8 byte counts as one rune. A JSON
// string counts its length in code points the same way, while length
// counts bytes. A []byte encodes to a JSON string of another length, so
// runes does not apply to it.
//
// On raw JSON text, as IsRawJson tells, it counts the code points of a
// string and passes a value of another kind.
type Runes struct{}

// Name returns "runes".
func (Runes) Name() string { return "runes" }

// RunesArgs are the args of runes(...), which Parse reads and Call spells.
type RunesArgs CompareArgs

// Call returns the call of runes spelling a.
func (a RunesArgs) Call() (expr.Call, error) { return compareParams.Call(Runes{}.Name(), a) }

// RunesOptions are the options of //kama:validate runes(...).
type RunesOptions struct {
	// Comparisons holds one comparison per arg, in order.
	Comparisons []Comparison
}

// Parse decodes the args of c into comparisons with non-negative integers.
func (Runes) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	cs, err := parseLengths(r, c)
	if err != nil {
		return nil, err
	}

	return RunesOptions{Comparisons: cs}, nil
}

// Check accepts the types the doc comment of Runes lists.
func (RunesOptions) Check(t types.Type) error {
	if !isStringType(t) && !ext.IsRawJson(t) {
		return fmt.Errorf("%s is not a string type", t)
	}

	return nil
}

// Emit calls kamaValidateRunes once per comparison.
func (o RunesOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	if ext.IsRawJson(t) {
		return rawComparisons(e, "Runes", v, o.Comparisons, false), nil
	}

	e.Declare(compareName, compareDecl)
	e.Declare(runesName, runesDecl)

	count := lang.Expr("%s.RuneCountInString(%s)",
		e.Import("unicode/utf8"), convert(t, types.Typ[types.String], "string", v))

	return joinCalls(e, o.Comparisons, func(c Comparison) ext.Gen {
		return lang.Expr("%s(%s, %s, %s)", runesName, count, strconv.Quote(c.Op), c.Operand)
	}), nil
}

const runesName = "kamaValidateRunes"

func runesDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s int, %s string, %s int) error",
			runesName, shadow("got"), shadow("op"), shadow("want")),
		lang.If(
			lang.Expr("!%s(%s, %s, %s)", compareName, ref("got"), ref("op"), ref("want")),
			lang.Stmt(`return %s.New(%s.ErrLength, "want rune count %%s %%d, got %%d", %s, %s, %s)`,
				v, v, ref("op"), ref("want"), ref("got")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
