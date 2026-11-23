package validator

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
)

// Length is the validator //kama:validate length="<op><n> ...": len of
// the value satisfies every comparison, as in >=1 <=10 or ==3. The operators
// are >, >=, ==, !=, <= and <, and n is a non-negative integer literal.
// n may carry a unary plus.
//
// It applies to types of a string, slice, array, map or channel underlying
// type. The length of a string is its number of bytes.
type Length struct{}

// Name returns "length".
func (Length) Name() string { return "length" }

// LengthOptions are the options of //kama:validate length="<op><n> ...".
type LengthOptions struct {
	// Comparisons holds one comparison per token of the arg, in order.
	Comparisons []Comparison
}

// Parse decodes arg into comparisons with non-negative integers.
func (Length) Parse(arg string) (Rule, error) {
	cs, err := parseComparisons(arg)
	if err != nil {
		return nil, err
	}

	for _, c := range cs {
		n := c.Operand

		val, err := literalValue(types.Typ[types.Int], n)
		if err != nil {
			return nil, argsError(err)
		}

		if constant.Sign(val) < 0 {
			return nil, argsError(fmt.Errorf("length %s is negative", n))
		}

		// int accepts 1.0 and 'a' as well, which are no lengths as written.
		// literalValue parsed n already. cmp accepts a unary plus, so length
		// does too.
		x, _ := parser.ParseExpr(n)
		if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.ADD {
			x = u.X
		}

		if lit, ok := x.(*ast.BasicLit); !ok || lit.Kind != token.INT {
			return nil, argsError(fmt.Errorf("length %s is not an integer literal", n))
		}
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
func (o LengthOptions) Emit(e Emitter, _ types.Type, v string) (string, error) {
	e.Declare(compareName, compareDecl)
	e.Declare(lengthName, lengthDecl)

	return joinCalls(e, o.Comparisons, func(c Comparison) string {
		return lengthName + "(len(" + v + "), " + strconv.Quote(c.Op) + ", " + c.Operand + ")"
	})
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

func lengthDecl(s Spelling) (string, error) {
	v, err := s.Import(validationPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`func kamaValidateLength(got int, op string, want int) error {
	if !kamaValidateCompare(got, op, want) {
		return %[1]s.New(%[1]s.ErrLength, "want length %%s %%d, got %%d", op, want, got)
	}
	return nil
}`, v), nil
}
