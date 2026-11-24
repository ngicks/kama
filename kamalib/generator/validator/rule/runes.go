package rule

import (
	"fmt"
	"go/types"
	"strconv"
)

// Runes is the validator //kama:validate runes="<op><n> ...": the number of
// runes of the value satisfies every comparison, as in >=1 <=10 or ==3. The
// operators are >, >=, ==, !=, <= and <, and n is a non-negative integer
// literal. n may carry a unary plus.
//
// It applies to types of a string underlying type. utf8.RuneCountInString
// counts the runes, so an invalid UTF-8 byte counts as one rune. A JSON
// string counts its length in code points the same way, while length
// counts bytes. A []byte encodes to a JSON string of another length, so
// runes does not apply to it.
type Runes struct{}

// Name returns "runes".
func (Runes) Name() string { return "runes" }

// RunesOptions are the options of //kama:validate runes="<op><n> ...".
type RunesOptions struct {
	// Comparisons holds one comparison per token of the arg, in order.
	Comparisons []Comparison
}

// Parse decodes arg into comparisons with non-negative integers, as Length
// does.
func (Runes) Parse(arg string, _ Resolver) (Rule, error) {
	cs, err := parseLengths(arg)
	if err != nil {
		return nil, err
	}

	return RunesOptions{Comparisons: cs}, nil
}

// Check accepts the types the doc comment of Runes lists.
func (RunesOptions) Check(t types.Type) error {
	if !isStringType(t) {
		return fmt.Errorf("%s is not a string type", t)
	}

	return nil
}

// Emit calls kamaValidateRunes once per comparison.
func (o RunesOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	e.Declare(compareName, compareDecl)
	e.Declare(runesName, runesDecl)

	count := lang.Expr("%s.RuneCountInString(%s)",
		e.Import("unicode/utf8"), convert(t, types.Typ[types.String], "string", v))

	return joinCalls(e, o.Comparisons, func(c Comparison) Gen {
		return lang.Expr("%s(%s, %s, %s)", runesName, count, strconv.Quote(c.Op), c.Operand)
	}), nil
}

const runesName = "kamaValidateRunes"

func runesDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

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
