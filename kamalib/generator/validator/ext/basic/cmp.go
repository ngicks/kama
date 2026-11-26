package basic

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"math"
	"math/big"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// Cmp is the validator //kama:validate cmp(...): the value satisfies every
// comparison, as in cmp(ge=1, lt=100) or cmp("a"). The keywords gt, ge,
// lt, le, eq and ne compare with >, >=, <, <=, == and !=, one positional
// arg compares with ==, and each value is a number, a string, True or
// False.
//
// It applies to types of an integer, float or string underlying type, and
// with == and != only to types of a bool underlying type. On raw JSON text,
// as IsRawJson tells, it takes JSON numbers only and compares numbers
// exactly, passing a value of another kind. On an integer
// type a number literal the type cannot represent, such as 1.5 or 1e30,
// compares exactly: >=1.5 holds for the values >=2, ==1.5 for none and
// <=1e30 for every value of int64. On a float type a number literal beyond
// the greatest finite value of the type compares exactly too: <=1e400
// holds for every value of float64 but +Inf and NaN, >=1e400 for +Inf
// only.
type Cmp struct{}

// Name returns "cmp".
func (Cmp) Name() string { return "cmp" }

// CmpOptions are the options of //kama:validate cmp(...).
type CmpOptions struct {
	// Comparisons holds one comparison per arg, in order.
	Comparisons []Comparison
}

// Parse decodes the args of c into comparisons.
func (Cmp) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	cs, _, err := parseComparisons(r, c, true)
	if err != nil {
		return nil, err
	}

	if len(cs) == 0 {
		return nil, noComparison(c)
	}

	return CmpOptions{Comparisons: cs}, nil
}

// Check accepts comparisons with literals of t, on the types the doc
// comment of Cmp lists.
func (o CmpOptions) Check(t types.Type) error {
	if ext.IsRawJson(t) {
		return checkRawNumbers(o.Comparisons)
	}

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
func (o CmpOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	if ext.IsRawJson(t) {
		return rawComparisons(e, "Compare", v, o.Comparisons, true), nil
	}

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

	var calls []ext.Gen

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
// c. On a float type, an operand beyond the finite values of t becomes the
// greatest or the least of them, as beyondFloat tells. Another operand
// must be a value of t.
func onType(t types.Type, c Comparison) (Comparison, string, error) {
	_, err := literalValue(t, c.Operand)
	info := typequery.BasicInfo(t)

	if err == nil || info&(types.IsInteger|types.IsFloat) == 0 {
		return c, "", err
	}

	r, ok := exactNumber(c.Operand)
	if !ok {
		return c, "", err
	}

	if info&types.IsFloat != 0 {
		return beyondFloat(t, c, r, err)
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

// beyondFloat returns c, whose operand r the float type t cannot
// represent, as onType does: an operand above the greatest finite value m
// of t gives <=m or >m, one below -m gives >=-m or <-m, so that only an
// infinity satisfies what no finite value does. err is the error of an
// operand that is not beyond the finite values, which t rounds to 0.
func beyondFloat(t types.Type, c Comparison, r *big.Rat, err error) (Comparison, string, error) {
	m := math.MaxFloat64
	if t.Underlying().(*types.Basic).Kind() == types.Float32 {
		m = math.MaxFloat32
	}

	limit := new(big.Rat).SetFloat64(m)
	above := r.Cmp(limit) > 0

	if !above && r.Cmp(new(big.Rat).Neg(limit)) >= 0 {
		return c, "", err
	}

	switch c.Op {
	case "==":
		return c, holdsNever, nil
	case "!=":
		return c, holdsAlways, nil
	}

	text := strconv.FormatFloat(m, 'g', -1, 64)
	below := c.Op == "<" || c.Op == "<="

	switch {
	case above && below:
		return Comparison{Op: "<=", Operand: text}, "", nil
	case above:
		return Comparison{Op: ">", Operand: text}, "", nil
	case below:
		return Comparison{Op: "<", Operand: "-" + text}, "", nil
	}

	return Comparison{Op: ">=", Operand: "-" + text}, "", nil
}

// exactNumber returns the value of lit, a Go number literal with an
// optional sign, exactly.
func exactNumber(lit string) (*big.Rat, bool) {
	x, err := parser.ParseExpr(lit)
	if err != nil {
		return nil, false
	}

	neg := false
	if u, ok := x.(*ast.UnaryExpr); ok && (u.Op == token.SUB || u.Op == token.ADD) {
		neg, x = u.Op == token.SUB, u.X
	}

	// go/types holds no integer constant above 512 bits, so the literal is
	// read with go/constant, which holds any.
	b, ok := x.(*ast.BasicLit)
	if !ok || b.Kind != token.INT && b.Kind != token.FLOAT {
		return nil, false
	}

	v := constant.MakeFromLiteral(b.Value, b.Kind, 0)
	if v.Kind() == constant.Unknown {
		return nil, false
	}

	r, ok := new(big.Rat).SetString(v.ExactString())
	if ok && neg {
		r.Neg(r)
	}

	return r, ok
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

func neverDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s any, %s, %s string) error",
			neverName, shadow("got"), shadow("op"), shadow("want")),
		lang.Stmt("return %s.New(%s.ErrCompare, \"want %%s %%s, got %%s\",\n%s, %s, %s(%s))",
			v, v, ref("op"), ref("want"), spellName, ref("got")),
	)
}

func cmpDecl(imp ext.Importer) ext.Step {
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

func equalDecl(imp ext.Importer) ext.Step {
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
func compareError(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Stmt("return %s.New(%s.ErrCompare, \"want %%s %%s, got %%s\",\n%s, %s(%s), %s(%s))",
		v, v, ref("op"), spellName, ref("want"), spellName, ref("got"))
}
