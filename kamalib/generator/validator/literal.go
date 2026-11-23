package validator

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// comparisonOps are the operators of a comparison arg, two-character ones
// first so that >= is not read as > followed by =.
var comparisonOps = []string{">=", "<=", "==", "!=", ">", "<"}

// comparison splits kv, an arg written <op><operand> as in >=1, into its
// operator and operand.
func comparison(kv dircmnt.KeyValue) (op, operand string, err error) {
	if !kv.HasValue {
		for _, op := range comparisonOps {
			operand, ok := strings.CutPrefix(kv.Key, op)
			// An operand starting like an operator, as in >= read as > and =,
			// is a misspelled operator.
			if ok && operand != "" && !strings.ContainsAny(operand[:1], "<>=!") {
				return op, operand, nil
			}
		}
	}

	return "", "", fmt.Errorf(
		"arg %q is no comparison; want one of %s followed by a value, as in >=1",
		spellArgs([]dircmnt.KeyValue{kv}), strings.Join(comparisonOps, ", "),
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

// comparisons reports why args are not one or more comparisons.
func comparisons(args []dircmnt.KeyValue) error {
	if len(args) == 0 {
		return errors.New("want one or more comparisons, as in >=1 <=10")
	}

	for _, kv := range args {
		if _, _, err := comparison(kv); err != nil {
			return err
		}
	}

	return nil
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

// basicInfo returns the info of the underlying type of t when it is a
// basic type, else 0. A type parameter has none.
func basicInfo(t types.Type) types.BasicInfo {
	if b, ok := t.Underlying().(*types.Basic); ok {
		return b.Info()
	}

	return 0
}
