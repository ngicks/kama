package std

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/types"
	"math/big"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

const bigPath = "math/big"

// Big is the genext.TypeSupport of *big.Int, *big.Float and *big.Rat. It
// implements ext.Comparer.
//
// A value compares by Cmp with a number literal, read exactly, as in
// cmp(ge=0, lt=1e30). A *big.Int takes an integer, and multipleof takes a
// *big.Int and a positive integer. A *big.Float compares with the exact
// value of the literal, so that cmp(eq=0.1) holds for no finite binary
// value. A nil pointer passes, as the value of any pointer passes a rule
// it does not hold. The value types are not supported: math/big does not
// support copying them.
type Big struct{}

// Name returns "math/big".
func (Big) Name() string { return bigPath }

// Comparable accepts the operands the doc comment of Big lists.
func (Big) Comparable(t types.Type, op, operand string) (bool, error) {
	name, ok := bigName(t)
	if !ok {
		return false, nil
	}

	if op == ext.OpMultiple && name != "Int" {
		return true, fmt.Errorf("a *big.%s has no multiple; multipleof takes a *big.Int", name)
	}

	_, err := bigOperand(name, operand, op == ext.OpMultiple)

	return true, err
}

// Compare compares by Cmp with the operand, which a package-level variable
// holds so that it is not parsed on every call.
func (Big) Compare(e ext.Emitter, t types.Type, v genext.Gen, op, operand string) genext.Gen {
	name, _ := bigName(t)
	r, _ := bigOperand(name, operand, op == ext.OpMultiple)
	operandVar := declareBig(e, name, r)

	switch {
	case op == ext.OpMultiple:
		return lang.Expr("%[1]s == nil || new(%[2]s.Int).Rem(%[1]s, %[3]s).Sign() == 0",
			v, e.Import(bigPath), operandVar)
	case name == "Float":
		e.Declare(bigFloatCmpName, bigFloatCmpDecl)

		return lang.Expr("%[1]s == nil || %[2]s(%[1]s, %[3]s) %[4]s 0",
			v, bigFloatCmpName, operandVar, op)
	}

	return lang.Expr("%[1]s == nil || %[1]s.Cmp(%[2]s) %[3]s 0", v, operandVar, op)
}

// bigName returns the name of the type t points to, Int, Float or Rat of
// math/big, or false for another type.
func bigName(t types.Type) (string, bool) {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return "", false
	}

	for _, name := range []string{"Int", "Float", "Rat"} {
		if typequery.IsNamed(p.Elem(), bigPath, name) {
			return name, true
		}
	}

	return "", false
}

// bigOperand returns the value of operand, a number literal, exactly. A
// *big.Int takes an integer, and a divisor of multipleof is positive.
func bigOperand(name, operand string, divisor bool) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(operand)
	if !ok {
		if _, err := strconv.Unquote(operand); err == nil {
			return nil, fmt.Errorf("%w: a *big.%s compares with a number, got the string %s",
				ext.ErrArgs, name, operand)
		}

		return nil, fmt.Errorf("%w: %s is no number", ext.ErrArgs, operand)
	}

	switch {
	case name == "Int" && !r.IsInt():
		return nil, fmt.Errorf("%w: a *big.Int takes an integer, got %s",
			ext.ErrArgs, operand)
	case divisor && r.Sign() <= 0:
		return nil, fmt.Errorf("%w: %w, got %s", ext.ErrArgs, errNotPositive, operand)
	}

	return r, nil
}

var errNotPositive = errors.New("multipleof takes a positive integer")

// declareBig declares the package-level variable holding r, a *big.Int for
// the type Int and a *big.Rat otherwise, and returns its name. The name
// derives from the value, so that equal operands share one variable.
func declareBig(e ext.Emitter, name string, r *big.Rat) genext.Gen {
	kind, text := "Rat", r.String()
	if name == "Int" {
		kind, text = "Int", r.Num().String()
	}

	sum := sha256.Sum256([]byte(kind + text))
	varName := "kamaValidateBig" + kind + "_" + hex.EncodeToString(sum[:5])
	setArgs := strconv.Quote(text)

	if kind == "Int" {
		setArgs += ", 10"
	}

	e.Declare(varName, func(imp ext.Importer) ext.Step {
		return lang.Stmt(
			"var %s = func() *%s.%s {\nx, _ := new(%s.%s).SetString(%s)\nreturn x\n}()",
			varName,
			imp.Import(bigPath),
			kind,
			imp.Import(bigPath),
			kind,
			setArgs,
		)
	})

	return lang.Expr("%s", varName)
}

// bigFloatCmpName compares a *big.Float with a *big.Rat exactly. An
// infinity is beyond every rational.
const bigFloatCmpName = "kamaValidateBigFloatCmp"

func bigFloatCmpDecl(imp ext.Importer) ext.Step {
	return lang.Func(
		lang.Expr("func %s(%s *%s.Float, %s *%s.Rat) int",
			bigFloatCmpName, shadow("x"), imp.Import(bigPath), shadow("y"), imp.Import(bigPath)),
		lang.If(lang.Expr("%s.IsInf()", ref("x")), lang.Stmt("return %s.Sign()", ref("x")), nil),
		lang.Stmt("%s, _ := %s.Rat(nil)", shadow("r"), ref("x")),
		lang.Stmt("return %s.Cmp(%s)", ref("r"), ref("y")),
	)
}
