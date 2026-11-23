package rule

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// comparisonOps are the operators of a comparison arg, two-character ones
// first so that >= is not read as > followed by =.
var comparisonOps = []string{">=", "<=", "==", "!=", ">", "<"}

// Comparison is one comparison of the arg of length or cmp, written
// <Op><Operand> as in >=1.
type Comparison struct {
	// Op is one of >, >=, ==, !=, <= and <.
	Op string
	// Operand is the value compared with, as written.
	Operand string
}

// parseComparisons decodes arg, one or more comparisons separated by
// spaces.
func parseComparisons(arg string) ([]Comparison, error) {
	args, err := argTokens(arg)
	if err != nil {
		return nil, err
	}

	if len(args) == 0 {
		return nil, argsError(errors.New("want one or more comparisons, as in >=1 <=10"))
	}

	cs := make([]Comparison, 0, len(args))

	for _, kv := range args {
		c, err := comparison(kv)
		if err != nil {
			return nil, argsError(err)
		}

		cs = append(cs, c)
	}

	return cs, nil
}

// comparison splits kv, an arg written <op><operand> as in >=1, into its
// operator and operand.
func comparison(kv dircmnt.KeyValue) (Comparison, error) {
	if !kv.HasValue {
		for _, op := range comparisonOps {
			operand, ok := strings.CutPrefix(kv.Key, op)
			// An operand starting like an operator, as in >= read as > and =,
			// is a misspelled operator.
			if ok && operand != "" && !strings.ContainsAny(operand[:1], "<>=!") {
				return Comparison{Op: op, Operand: operand}, nil
			}
		}
	}

	return Comparison{}, fmt.Errorf(
		"arg %q is no comparison; want one of %s followed by a value, as in >=1",
		kv.String(), strings.Join(comparisonOps, ", "),
	)
}

// argTokens splits arg, the argument of a validator taking several
// tokens, as dircmnt.ParseArgsKeyValue does.
func argTokens(arg string) ([]dircmnt.KeyValue, error) {
	args, err := dircmnt.ParseArgsKeyValue(arg)
	if err != nil {
		return nil, argsError(err)
	}

	return args, nil
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
func joinCalls(e Emitter, cs []Comparison, call func(c Comparison) Gen) Gen {
	calls := opiter.WrapSeq(slices.Values(cs)).
		Map(call).
		CollectSlice()

	if len(calls) == 1 {
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

func compareDecl(imp Importer) Step {
	cases := opiter.WrapSeq(slices.Values([]string{">", ">=", "==", "!=", "<=", "<"})).
		Map(func(op string) genhelper.Case[Context] {
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

func allDecl(imp Importer) Step {
	return lang.Func(
		lang.Expr("func %s(%s bool, %s ...error) error",
			allName, shadow("stopOnFirstError"), shadow("errs")),
		lang.If(
			lang.Expr("!%s", ref("stopOnFirstError")),
			lang.Stmt("return %s.Join(%s...)", imp.Import(ValidationPath), ref("errs")),
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

func spellDecl(imp Importer) Step {
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
