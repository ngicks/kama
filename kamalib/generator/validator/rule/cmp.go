package rule

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"math/big"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// Cmp is the validator //kama:validate cmp="<op><value> ...": the value
// satisfies every comparison, as in >=1 <100 or =="a". The operators are
// >, >=, ==, !=, <= and <, and value is a Go literal of the type of the
// value: a number, a quoted string, true or false.
//
// It applies to types of an integer, float or string underlying type, and
// with == and != only to types of a bool underlying type. On an integer
// type a number literal the type cannot represent, such as 1.5 or 1e30,
// compares exactly: >=1.5 holds for the values >=2, ==1.5 for none and
// <=1e30 for every value of int64.
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
func (Cmp) Parse(arg string, _ Resolver) (Rule, error) {
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
		if _, _, err := onType(t, c); err != nil {
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

	var calls []Gen

	for _, c := range o.Comparisons {
		on, holds, err := onType(t, c)
		if err != nil {
			return nil, err
		}

		switch holds {
		case holdsAlways:
		case holdsNever:
			e.Declare(neverName, neverDecl)
			calls = append(calls, lang.Expr(
				"%s(%s, %s, %s)", neverName, v, strconv.Quote(c.Op), strconv.Quote(c.Operand),
			))
		default:
			calls = append(calls, lang.Expr(
				"%s(%s, %s, %s)", helper, v, strconv.Quote(on.Op), on.Operand,
			))
		}
	}

	return joinErrors(e, calls), nil
}

// The verdicts of onType on a comparison that needs no code.
const (
	holdsAlways = "always"
	holdsNever  = "never"
)

// onType returns c as a comparison with an operand of t holding for the
// same values of t. On an integer type, an operand that is a number t
// cannot represent is rounded to the bound of the values satisfying c, and
// holds tells holdsAlways or holdsNever when every value or none satisfies
// c. Another operand must be a value of t.
func onType(t types.Type, c Comparison) (Comparison, string, error) {
	_, err := literalValue(t, c.Operand)
	if err == nil || typequery.BasicInfo(t)&types.IsInteger == 0 {
		return c, "", err
	}

	r, ok := exactNumber(c.Operand)
	if !ok {
		return c, "", err
	}

	lo, hi := integerRange(t)

	floor := new(big.Int).Div(r.Num(), r.Denom())
	ceil := new(big.Int).Neg(new(big.Int).Div(new(big.Int).Neg(r.Num()), r.Denom()))
	one := big.NewInt(1)

	var bound *big.Int

	op := c.Op

	switch op {
	case "==":
		return c, holdsNever, nil
	case "!=":
		return c, holdsAlways, nil
	case ">":
		op, bound = ">=", floor.Add(floor, one)
	case ">=":
		bound = ceil
	case "<":
		op, bound = "<=", ceil.Sub(ceil, one)
	case "<=":
		bound = floor
	}

	switch {
	case op == ">=" && bound.Cmp(hi) > 0, op == "<=" && bound.Cmp(lo) < 0:
		return c, holdsNever, nil
	case op == ">=" && bound.Cmp(lo) <= 0, op == "<=" && bound.Cmp(hi) >= 0:
		return c, holdsAlways, nil
	}

	return Comparison{Op: op, Operand: bound.String()}, "", nil
}

// exactNumber returns the value of lit, a Go number literal with an
// optional sign, exactly.
func exactNumber(lit string) (*big.Rat, bool) {
	x, err := parser.ParseExpr(lit)
	if err != nil || !isLiteral(x) {
		return nil, false
	}

	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if types.CheckExpr(token.NewFileSet(), nil, token.NoPos, x, info) != nil {
		return nil, false
	}

	v := info.Types[x].Value
	if v == nil || (v.Kind() != constant.Int && v.Kind() != constant.Float) {
		return nil, false
	}

	return new(big.Rat).SetString(v.ExactString())
}

// integerRange returns the least and the greatest value of the integer
// type t, taking int, uint and uintptr as 64 bits wide.
func integerRange(t types.Type) (*big.Int, *big.Int) {
	bits := map[types.BasicKind]uint{
		types.Int8: 8, types.Int16: 16, types.Int32: 32,
		types.Uint8: 8, types.Uint16: 16, types.Uint32: 32,
	}[t.Underlying().(*types.Basic).Kind()]
	if bits == 0 {
		bits = 64
	}

	if typequery.BasicInfo(t)&types.IsUnsigned != 0 {
		return new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
	}

	hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))

	return new(big.Int).Neg(new(big.Int).Add(hi, big.NewInt(1))), hi
}

const (
	cmpName   = "kamaValidateCmp"
	equalName = "kamaValidateEqual"
	// neverName is the helper of a comparison no value of the type
	// satisfies, which spells the operand as written.
	neverName = "kamaValidateNever"
)

func neverDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s any, %s, %s string) error",
			neverName, shadow("got"), shadow("op"), shadow("want")),
		lang.Stmt("return %s.New(%s.ErrCompare, \"want %%s %%s, got %%s\",\n%s, %s, %s(%s))",
			v, v, ref("op"), ref("want"), spellName, ref("got")),
	)
}

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
