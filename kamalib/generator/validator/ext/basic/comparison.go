package basic

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// comparisonKeywords are the keywords of a comparison, the operators of
// the parameters eq, ge, gt, le, lt and ne in that order.
var comparisonKeywords = []string{"eq", "ge", "gt", "le", "lt", "ne"}

// comparisonOps are the Go operators of comparisonKeywords, index for
// index.
var comparisonOps = []string{"==", ">=", ">", "<=", "<", "!="}

// compareParams are the parameters of length, runes, cmp and members. One
// positional arg compares with ==.
var compareParams = ext.Params{
	Positional: comparisonKeywords[:1:1],
	Keywords:   comparisonKeywords[1:],
}

// CompareArgs are the args of length, runes, cmp and members, each the
// operand of its keyword as JSON text, nil for none. Ne holds a list of
// operands or one operand.
type CompareArgs struct {
	Eq jsontext.Value `json:"eq"`
	Ge jsontext.Value `json:"ge"`
	Gt jsontext.Value `json:"gt"`
	Le jsontext.Value `json:"le"`
	Lt jsontext.Value `json:"lt"`
	Ne jsontext.Value `json:"ne"`
}

// Comparison is one comparison of the args of length, runes or cmp, as
// ge=1 in length(ge=1).
type Comparison struct {
	// Op is one of >, >=, ==, !=, <= and <.
	Op string
	// Operand is the value compared with, as a Go literal.
	Operand string
}

// parseComparisons returns the comparisons of the args of c in the order
// written, and the positional args. A keyword of comparisonKeywords
// compares with its operator. ne takes a list too, one != per element.
// With eq set, one positional arg compares with ==, as in length(3). A
// keyword of no comparison is left out and reported through r.Warn.
func parseComparisons(r ext.Resolver, c expr.Call, eq bool) ([]Comparison, []expr.Arg, error) {
	var (
		cs         []Comparison
		rest       []expr.Arg
		positional int
	)

	for _, a := range c.Args {
		i := slices.Index(comparisonKeywords, a.Name)

		switch {
		case a.Name == "" && eq:
			positional++
			i = slices.Index(comparisonOps, "==")
		case a.Name == "":
			rest = append(rest, a)
			continue
		case i < 0:
			r.Warn(fmt.Errorf("%s: unknown arg %s; known: %v", c.Name, a, comparisonKeywords))
			continue
		}

		values := []expr.Node{a.Value}
		if l, ok := a.Value.(expr.List); ok && a.Name == "ne" {
			values = l.Elems
		}

		for _, v := range values {
			operand, err := goLiteral(v)
			if err != nil {
				return nil, nil, argsError(fmt.Errorf("%s: %s: %w", c.Name, a, err))
			}

			cs = append(cs, Comparison{Op: comparisonOps[i], Operand: operand})
		}
	}

	if positional > 1 {
		return nil, nil, argsError(fmt.Errorf(
			"%s takes one positional arg, the value compared with ==, got %d", c.Name, positional,
		))
	}

	return cs, rest, nil
}

// goLiteral spells the literal n as a Go literal: a number as written, a
// string quoted and a bool as true or false.
func goLiteral(n expr.Node) (string, error) {
	switch x := n.(type) {
	case expr.Number:
		return x.Text, nil
	case expr.String:
		return strconv.Quote(x.Value), nil
	case expr.Bool:
		return strconv.FormatBool(x.Value), nil
	}

	return "", fmt.Errorf("%s is no number, string or bool", n)
}

// noComparison reports c without comparisons.
func noComparison(c expr.Call) error {
	return argsError(fmt.Errorf(
		"%w, as in %s(ge=1, le=10); the keywords are %s",
		errNoComparison, c.Name, strings.Join(comparisonKeywords, ", "),
	))
}

// parseLengths returns the comparisons of the args of c, which take
// non-negative integer literals, as length and runes take them. One
// positional arg compares with ==. A comparison with an integer above the
// greatest int, which no length reaches, is left out when every length
// satisfies it, and becomes >9223372036854775807 when none does, taking
// int as 64 bits wide.
func parseLengths(r ext.Resolver, c expr.Call) ([]Comparison, error) {
	cs, _, err := parseComparisons(r, c, true)
	if err != nil {
		return nil, err
	}

	if len(cs) == 0 {
		return nil, noComparison(c)
	}

	return lengths(cs)
}

// lengths checks the operands of cs, comparisons of a length, as
// parseLengths does.
func lengths(cs []Comparison) ([]Comparison, error) {
	out := make([]Comparison, 0, len(cs))

	for _, c := range cs {
		n := c.Operand

		val, err := literalValue(types.Typ[types.Int], n)
		if err != nil {
			if !beyondInt(n) {
				return nil, argsError(err)
			}

			if c.Op == ">" || c.Op == ">=" || c.Op == "==" {
				out = append(
					out,
					Comparison{Op: ">", Operand: strconv.FormatInt(math.MaxInt64, 10)},
				)
			}

			continue
		}

		if constant.Sign(val) < 0 {
			return nil, argsError(fmt.Errorf("length %s is negative", n))
		}

		// int accepts 1.0 and "a" as well, which are no lengths as written.
		x, _ := parser.ParseExpr(n)
		if lit, ok := x.(*ast.BasicLit); !ok || lit.Kind != token.INT {
			return nil, argsError(fmt.Errorf("length %s is not an integer literal", n))
		}

		out = append(out, c)
	}

	return out, nil
}

// beyondInt reports whether lit is an integer literal, with an optional
// unary plus, above the greatest int64.
func beyondInt(lit string) bool {
	x, err := parser.ParseExpr(lit)
	if err != nil {
		return false
	}

	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.ADD {
		x = u.X
	}

	b, ok := x.(*ast.BasicLit)

	return ok && b.Kind == token.INT &&
		constant.Compare(
			constant.MakeFromLiteral(
				b.Value,
				token.INT,
				0,
			),
			token.GTR,
			constant.MakeInt64(math.MaxInt64),
		)
}

// literalValue returns the value of lit, a Go literal, as a value of t,
// whose underlying type is basic. lit is a basic literal, a signed number
// literal, true or false. It must be representable by t: an integer type
// rejects 1.5 and an overflowing value, an unsigned type a negative value,
// and a string type anything but a string literal.
func literalValue(t types.Type, lit string) (constant.Value, error) {
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return nil, fmt.Errorf("%s is not a basic type", t)
	}

	x, err := parser.ParseExpr(lit)
	if err != nil || !isLiteral(x) {
		return nil, fmt.Errorf("%s is not a Go literal", lit)
	}

	// The conversion to the basic type checks that the value is
	// representable by it, as the compiler would.
	conv := &ast.CallExpr{Fun: ast.NewIdent(b.Name()), Args: []ast.Expr{x}}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}

	if err := types.CheckExpr(token.NewFileSet(), nil, token.NoPos, conv, info); err != nil {
		if te, ok := errors.AsType[types.Error](err); ok {
			err = errors.New(te.Msg)
		}

		return nil, fmt.Errorf("%s is no value of %s: %w", lit, t, err)
	}

	val := info.Types[conv].Value
	// A conversion turns an integer into a string of one rune, which is not
	// what a literal of a string type means.
	if val == nil || b.Info()&types.IsString != 0 && info.Types[x].Value.Kind() != constant.String {
		return nil, fmt.Errorf("%s is no value of %s; want a quoted string", lit, t)
	}

	return val, nil
}

// isLiteral reports whether x is a basic literal, a signed number literal,
// true or false.
func isLiteral(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.BasicLit:
		return true
	case *ast.UnaryExpr:
		lit, ok := x.X.(*ast.BasicLit)
		return ok && (x.Op == token.SUB || x.Op == token.ADD) && lit.Kind != token.STRING
	case *ast.Ident:
		return slices.Contains([]string{"true", "false"}, x.Name)
	}

	return false
}

// joinCalls returns call of each comparison of cs. It passes several
// calls to kamaValidateAll with the stop flag of e. kamaValidateAll returns
// the first error when the flag is true and all of them joined otherwise.
func joinCalls(e ext.Emitter, cs []Comparison, call func(c Comparison) ext.Gen) ext.Gen {
	return joinErrors(e, opiter.SlicesValues(cs).
		Map(call).
		CollectSlice())
}

// joinErrors returns the expression of type error of calls, each of type
// error, as joinCalls does. No call gives a nil error.
func joinErrors(e ext.Emitter, calls []ext.Gen) ext.Gen {
	switch len(calls) {
	case 0:
		return lang.Expr("error(nil)")
	case 1:
		return calls[0]
	}

	e.Declare(allName, allDecl)

	return lang.Expr("%s(%s, %s)", allName, e.StopOnFirstError(), lang.Join(", ", calls...))
}

const (
	compareName = "kamaValidateCompare"
	allName     = "kamaValidateAll"
	// spellName is the helper spelling a value in an error message: a
	// string quoted, as %#v would, and a number in decimal, as %#v would
	// not for an unsigned integer.
	spellName = "kamaValidateSpell"
)

func compareDecl(imp ext.Importer) ext.Step {
	cases := opiter.SlicesValues([]string{">", ">=", "==", "!=", "<=", "<"}).
		Map(func(op string) genhelper.Case[ext.Context] {
			return lang.Case(
				lang.Expr("%s", strconv.Quote(op)),
				lang.Stmt("return %s %s %s", ref("got"), op, ref("want")),
			)
		}).
		CollectSlice()

	return lang.Func(
		lang.Expr("func %s[%s %s.Ordered](%s %s, %s string, %s %s) bool",
			compareName, shadow("T"), imp.Import("cmp"),
			shadow("got"), ref("T"), shadow("op"), shadow("want"), ref("T")),
		lang.Switch(lang.Name("op"), cases...),
		lang.Stmt("return false"),
	)
}

func allDecl(imp ext.Importer) ext.Step {
	return lang.Func(
		lang.Expr("func %s(%s bool, %s ...error) error",
			allName, shadow("stopOnFirstError"), shadow("errs")),
		lang.If(
			lang.Expr("!%s", ref("stopOnFirstError")),
			lang.Stmt("return %s.Join(%s...)", imp.Import(ext.ValidationPath), ref("errs")),
			nil,
		),
		// For renders its clause in the scope of the loop, which the range
		// variable is declared in.
		lang.For(nil, lang.Expr("_, %s := range %s", shadow("err"), ref("errs")), nil,
			lang.If(lang.Expr("%s != nil", ref("err")), lang.Stmt("return %s", ref("err")), nil),
		),
		lang.Stmt("return nil"),
	)
}

func spellDecl(imp ext.Importer) ext.Step {
	rf := imp.Import("reflect")

	return lang.Func(
		lang.Expr("func %s(%s any) string", spellName, shadow("v")),
		lang.If(
			lang.Expr("%s := %s.ValueOf(%s); %s.Kind() == %s.String",
				shadow("rv"), rf, ref("v"), ref("rv"), rf),
			lang.Stmt("return %s.Quote(%s.String())", imp.Import("strconv"), ref("rv")),
			nil,
		),
		lang.Stmt("return %s.Sprint(%s)", imp.Import("fmt"), ref("v")),
	)
}
