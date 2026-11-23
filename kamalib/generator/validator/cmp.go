package validator

import (
	"fmt"
	"go/types"
	"strconv"
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

// Check accepts comparisons with literals of t, on the types the doc
// comment of Cmp lists.
func (Cmp) Check(t types.Type, arg string) error {
	args, err := argTokens(arg)
	if err != nil {
		return err
	}

	if err := comparisons(args); err != nil {
		return argsError(err)
	}

	info := basicInfo(t)

	switch {
	case info&types.IsOrdered != 0:
	case info&types.IsBoolean != 0:
		for _, kv := range args {
			if op, _, _ := comparison(kv); op != "==" && op != "!=" {
				return fmt.Errorf("%s is a bool type, which takes == and != only; got %s", t, op)
			}
		}
	default:
		return fmt.Errorf("%s is not an integer, float, string or bool type", t)
	}

	for _, kv := range args {
		_, lit, _ := comparison(kv)
		if _, err := literalValue(t, lit); err != nil {
			return err
		}
	}

	return nil
}

// Emit calls kamaValidateCmp, or kamaValidateEqual for a bool, once per
// comparison.
func (Cmp) Emit(e Emitter, t types.Type, v string, arg string) (string, error) {
	args, err := argTokens(arg)
	if err != nil {
		return "", err
	}

	helper := cmpName
	if basicInfo(t)&types.IsBoolean != 0 {
		helper = equalName
		e.Declare(equalName, equalDecl)
		e.Declare(spellName, spellDecl)
	} else {
		e.Declare(compareName, compareDecl)
		e.Declare(cmpName, cmpDecl)
		e.Declare(spellName, spellDecl)
	}

	return joinCalls(e, args, func(op, lit string) string {
		return helper + "(" + v + ", " + strconv.Quote(op) + ", " + lit + ")"
	})
}

const (
	cmpName   = "kamaValidateCmp"
	equalName = "kamaValidateEqual"
)

func cmpDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
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
	v, err := s.Import(validationPath)
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
