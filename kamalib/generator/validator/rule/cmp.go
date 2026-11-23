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
func (o CmpOptions) Emit(e Emitter, t types.Type, v string) (string, error) {
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

	return joinCalls(e, o.Comparisons, func(c Comparison) string {
		return helper + "(" + v + ", " + strconv.Quote(c.Op) + ", " + c.Operand + ")"
	})
}

const (
	cmpName   = "kamaValidateCmp"
	equalName = "kamaValidateEqual"
)

func cmpDecl(s Spelling) (string, error) {
	v, err := s.Import(ValidationPath)
	if err != nil {
		return "", err
	}

	c, err := s.Import("cmp")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateCmp[T %[2]s.Ordered](got T, op string, want T) error {
	if !kamaValidateCompare(got, op, want) {
		return %[1]s.New(%[1]s.ErrCompare, "want %%s %%s, got %%s",
			op, kamaValidateSpell(want), kamaValidateSpell(got))
	}
	return nil
}`, v, c), nil
}

func equalDecl(s Spelling) (string, error) {
	v, err := s.Import(ValidationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateEqual[T comparable](got T, op string, want T) error {
	if (got == want) != (op == "==") {
		return %[1]s.New(%[1]s.ErrCompare, "want %%s %%s, got %%s",
			op, kamaValidateSpell(want), kamaValidateSpell(got))
	}
	return nil
}`, v), nil
}
