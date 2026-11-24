package rule

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// MultipleOf is the validator //kama:validate multipleof=<n>: the value
// divided by n is an integer, as in multipleof=2 or multipleof=0.01. n is a
// positive integer or float literal and may carry a unary plus.
//
// It applies to types of an integer or float underlying type. An integer
// value is tested with %, by the numerator of n in lowest terms: 1.5 is
// 3/2, and a multiple of 3/2 is an integer that 3 divides. The numerator
// must be a value of the type. A float value is tested exactly, as the
// shortest decimal that reads back as the value: float64(0.0075) is a
// multiple of 0.0001 although its binary value is not. NaN and the
// infinities are multiples of nothing.
//
// On raw JSON text, as IsRawJson tells, n must be a JSON number. It tests a
// number exactly and passes a value of another kind.
type MultipleOf struct{}

// Name returns "multipleof".
func (MultipleOf) Name() string { return "multipleof" }

// MultipleOfOptions are the options of //kama:validate multipleof=<n>.
type MultipleOfOptions struct {
	// Divisor is the arg, as written.
	Divisor string
}

// Parse decodes arg into a positive number literal.
func (MultipleOf) Parse(arg string, _ Resolver) (Rule, error) {
	if _, err := divisorValue(arg); err != nil {
		return nil, argsError(err)
	}

	return MultipleOfOptions{Divisor: arg}, nil
}

// Check accepts the types the doc comment of MultipleOf lists.
func (o MultipleOfOptions) Check(t types.Type) error {
	if IsRawJson(t) {
		return checkRawNumbers([]Comparison{{Operand: o.Divisor}})
	}

	info := typequery.BasicInfo(t)

	switch {
	case info&types.IsInteger != 0:
		d, err := divisorValue(o.Divisor)
		if err != nil {
			return err
		}

		if _, err := literalValue(t, constant.Num(d).ExactString()); err != nil {
			return fmt.Errorf("divisor %s: %w", o.Divisor, err)
		}
	case info&types.IsFloat != 0:
	default:
		return fmt.Errorf("%s is not an integer or float type", t)
	}

	return nil
}

// Emit calls kamaValidateMultipleInt or kamaValidateMultipleFloat.
func (o MultipleOfOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	if IsRawJson(t) {
		return rawCall(e, "MultipleOf", v, lang.Expr("%s", strconv.Quote(o.Divisor))), nil
	}

	d, err := divisorValue(o.Divisor)
	if err != nil {
		return nil, err
	}

	info := typequery.BasicInfo(t)
	if info&types.IsInteger != 0 {
		e.Declare(multipleIntName, multipleIntDecl)

		return lang.Expr("%s(%s, %s, %s)",
			multipleIntName, v, constant.Num(d).ExactString(), strconv.Quote(o.Divisor),
		), nil
	}

	bitSize := 64
	if t.Underlying().(*types.Basic).Kind() == types.Float32 {
		bitSize = 32
	}

	e.Declare(multipleFloatName, multipleFloatDecl)

	return lang.Expr("%s(%s, %d, %s, %s)",
		multipleFloatName,
		convert(t, types.Typ[types.Float64], "float64", v),
		bitSize,
		strconv.Quote(constant.Num(d).ExactString()+"/"+constant.Denom(d).ExactString()),
		strconv.Quote(o.Divisor),
	), nil
}

// divisorValue returns the exact value of lit, a positive integer or float
// literal.
func divisorValue(lit string) (constant.Value, error) {
	x, err := parser.ParseExpr(lit)
	if err != nil {
		return nil, fmt.Errorf("divisor %q is not a Go literal", lit)
	}

	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.ADD {
		x = u.X
	}

	b, ok := x.(*ast.BasicLit)
	if !ok || b.Kind != token.INT && b.Kind != token.FLOAT {
		return nil, fmt.Errorf("divisor %s is not an integer or float literal", lit)
	}

	d := constant.MakeFromLiteral(b.Value, b.Kind, 0)
	// constant holds a float of a huge exponent inexactly, and Num of it
	// is unknown.
	if constant.Num(d).Kind() != constant.Int {
		return nil, fmt.Errorf("divisor %s is out of range", lit)
	}

	if constant.Sign(d) <= 0 {
		return nil, errors.New("divisor " + lit + " is not positive")
	}

	return d, nil
}

const (
	multipleIntName   = "kamaValidateMultipleInt"
	multipleFloatName = "kamaValidateMultipleFloat"
)

func multipleIntDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

	return lang.Func(
		lang.Expr(
			"func %s[%s ~int | ~int8 | ~int16 | ~int32 | ~int64 |\n"+
				"~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr](%s, %s %s, %s string) error",
			multipleIntName, shadow("T"), shadow("got"), shadow("step"), ref("T"), shadow("want"),
		),
		lang.If(
			lang.Expr("%s%%%s != 0", ref("got"), ref("step")),
			lang.Stmt(`return %s.New(%s.ErrMultiple, "want a multiple of %%s, got %%d", %s, %s)`,
				v, v, ref("want"), ref("got")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}

func multipleFloatDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)
	big := imp.Import("math/big")

	return lang.Func(
		lang.Expr("func %s(%s float64, %s int, %s, %s string) error",
			multipleFloatName, shadow("got"), shadow("bitSize"), shadow("step"), shadow("want")),
		// The shortest decimal of an infinity or NaN does not parse as a
		// big.Rat, so they are no multiples.
		lang.Stmt("%s := %s.FormatFloat(%s, 'g', -1, %s)",
			shadow("spelled"), imp.Import("strconv"), ref("got"), ref("bitSize")),
		lang.Stmt("%s, %s := new(%s.Rat).SetString(%s)",
			shadow("r"), shadow("ok"), big, ref("spelled")),
		lang.If(
			lang.Expr("%s", ref("ok")),
			lang.Steps(
				lang.Stmt("%s, _ := new(%s.Rat).SetString(%s)", shadow("s"), big, ref("step")),
				lang.Stmt("%s = %s.Quo(%s, %s).IsInt()", ref("ok"), ref("r"), ref("r"), ref("s")),
			),
			nil,
		),
		lang.If(
			lang.Expr("!%s", ref("ok")),
			lang.Stmt(`return %s.New(%s.ErrMultiple, "want a multiple of %%s, got %%s", %s, %s)`,
				v, v, ref("want"), ref("spelled")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
