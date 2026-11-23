package rule

import (
	"fmt"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// Cmp is the validator //kama:validate cmp="<op><value> ...": the value
// satisfies every comparison, as in >=1 <100 or =="a". The operators are
// >, >=, ==, !=, <= and <, and value is a Go literal of the type of the
// value: a number, a quoted string, true or false.
//
// It applies to types of an integer, float or string underlying type, and
// with == and != only to types of a bool underlying type.
type Cmp struct{}

// Name returns "cmp".
func (Cmp) Name() string { return "cmp" }

// CmpOptions are the options of //kama:validate cmp="<op><value> ...".
type CmpOptions struct {
	// Comparisons holds one comparison per token of the arg, in order.
	Comparisons []Comparison
}

// Parse decodes arg into comparisons. Check tells whether their operands
// are values of the type.
func (Cmp) Parse(arg string) (Rule, error) {
	cs, err := parseComparisons(arg)
	if err != nil {
		return nil, err
	}

	return CmpOptions{Comparisons: cs}, nil
}

// Check accepts comparisons with literals of t, on the types the doc
// comment of Cmp lists.
func (o CmpOptions) Check(t types.Type) error {
	info := typequery.BasicInfo(t)

	switch {
	case info&types.IsOrdered != 0:
	case info&types.IsBoolean != 0:
		for _, c := range o.Comparisons {
			if c.Op != "==" && c.Op != "!=" {
				return fmt.Errorf("%s is a bool type, which takes == and != only; got %s", t, c.Op)
			}
		}
	default:
		return fmt.Errorf("%s is not an integer, float, string or bool type", t)
	}

	for _, c := range o.Comparisons {
		if _, err := literalValue(t, c.Operand); err != nil {
			return err
		}
	}

	return nil
}

// Emit calls kamaValidateCmp, or kamaValidateEqual for a bool, once per
// comparison.
func (o CmpOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	helper := cmpName
	if typequery.BasicInfo(t)&types.IsBoolean != 0 {
		helper = equalName
		e.Declare(equalName, equalDecl)
		e.Declare(spellName, spellDecl)
	} else {
		e.Declare(compareName, compareDecl)
		e.Declare(cmpName, cmpDecl)
		e.Declare(spellName, spellDecl)
	}

	return joinCalls(e, o.Comparisons, func(c Comparison) Gen {
		return lang.Expr("%s(%s, %s, %s)", helper, v, strconv.Quote(c.Op), c.Operand)
	}), nil
}

const (
	cmpName   = "kamaValidateCmp"
	equalName = "kamaValidateEqual"
)

func cmpDecl(imp Importer) Step {
	return lang.Func(
		lang.Expr("func %s[%s %s.Ordered](%s %s, %s string, %s %s) error",
			cmpName, shadow("T"), imp.Import("cmp"),
			shadow("got"), ref("T"), shadow("op"), shadow("want"), ref("T")),
		lang.If(
			lang.Expr("!%s(%s, %s, %s)", compareName, ref("got"), ref("op"), ref("want")),
			compareError(imp),
			nil,
		),
		lang.Stmt("return nil"),
	)
}

func equalDecl(imp Importer) Step {
	return lang.Func(
		lang.Expr(
			"func %s[%s comparable](%s %s, %s string, %s %s) error",
			equalName,
			shadow("T"),
			shadow("got"),
			ref("T"),
			shadow("op"),
			shadow("want"),
			ref("T"),
		),
		lang.If(
			lang.Expr(`(%s == %s) != (%s == "==")`, ref("got"), ref("want"), ref("op")),
			compareError(imp),
			nil,
		),
		lang.Stmt("return nil"),
	)
}

// compareError is the statement of kamaValidateCmp and kamaValidateEqual
// returning the failure of the comparison.
func compareError(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Stmt("return %s.New(%s.ErrCompare, \"want %%s %%s, got %%s\",\n%s, %s(%s), %s(%s))",
		v, v, ref("op"), spellName, ref("want"), spellName, ref("got"))
}
