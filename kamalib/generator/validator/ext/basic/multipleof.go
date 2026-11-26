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
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// MultipleOf is the validator //kama:validate multipleof(<n>): the value
// divided by n is an integer, as in multipleof=2 or multipleof=0.01. n is a
// positive integer or float literal and may carry a unary plus.
//
// It applies to types of an integer or float underlying type. An integer
// value is tested with %, by the numerator of n in lowest terms: 1.5 is
// 3/2, and a multiple of 3/2 is an integer that 3 divides. A float value
// is tested exactly, as the shortest decimal that reads back as the value:
// float64(0.0075) is a multiple of 0.0001 although its binary value is
// not. NaN and the infinities are multiples of nothing. When n, or the
// numerator of n for an integer type, is above every value of the type,
// such as 1e30 for int64 or 1e400 for float64, only 0 is a multiple.
//
// A type an ext.Comparer of the registry claims is a multiple as it tells,
// as a *big.Int is with the package std.
//
// On raw JSON text, as IsRawJson tells, n must be a JSON number. It tests a
// number exactly and passes a value of another kind.
type MultipleOf struct{}

// Name returns "multipleof".
func (MultipleOf) Name() string { return "multipleof" }

// MultipleOfOptions are the options of //kama:validate multipleof(<n>).
type MultipleOfOptions struct {
	// Divisor is the arg, as written.
	Divisor string
	// reg is the registry the options were parsed with, whose type supports
	// tell the multiples of the types they claim.
	reg ext.Resolver
}

// Parse decodes the arg of c, a positive number.
func (MultipleOf) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	var args struct {
		N jsontext.Value `json:"n"`
	}

	if _, err := (ext.Params{Positional: []string{"n"}}).Bind(r, c, &args); err != nil {
		return nil, err
	}

	if args.N.Kind() != '0' {
		return nil, argsError(errors.New("want one number, as in multipleof(0.01)"))
	}

	if _, err := divisorValue(string(args.N)); err != nil {
		return nil, argsError(err)
	}

	return MultipleOfOptions{Divisor: string(args.N), reg: r}, nil
}

// Check accepts the types the doc comment of MultipleOf lists.
func (o MultipleOfOptions) Check(t types.Type) error {
	if ok, err := ext.Comparable(o.reg, t, ext.OpMultiple, o.Divisor); ok {
		return err
	}

	if ext.IsRawJson(t) {
		return checkRawNumbers([]Comparison{{Operand: o.Divisor}})
	}

	info := typequery.BasicInfo(t)

	if info&(types.IsInteger|types.IsFloat) == 0 {
		return fmt.Errorf("%s is not an integer or float type", t)
	}

	_, err := divisorValue(o.Divisor)

	return err
}

// Emit calls kamaValidateMultipleInt or kamaValidateMultipleFloat.
// multipleComparedName is the helper of multipleof on a type an
// ext.Comparer claims, which reports the condition it gives.
const multipleComparedName = "kamaValidateMultipleCompared"

func multipleComparedDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s(%s bool, %s any, %s string) error",
			multipleComparedName, shadow("ok"), shadow("got"), shadow("want")),
		lang.If(
			lang.Expr("!%s", ref("ok")),
			lang.Stmt(
				`return %s.New(%s.ErrMultiple, "want a multiple of %%s, got %%s", %s, %s(%s))`,
				v,
				v,
				ref("want"),
				spellName,
				ref("got"),
			),
			nil,
		),
		lang.Stmt("return nil"),
	)
}

func (o MultipleOfOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	if cond, ok := ext.Compare(o.reg, e, t, v, ext.OpMultiple, o.Divisor); ok {
		e.Declare(multipleComparedName, multipleComparedDecl)
		e.Declare(spellName, spellDecl)

		return lang.Expr("%s(%s, %s, %s)",
			multipleComparedName, cond, v, strconv.Quote(o.Divisor)), nil
	}

	if ext.IsRawJson(t) {
		return rawCall(e, "MultipleOf", v, lang.Expr("%s", strconv.Quote(o.Divisor))), nil
	}

	d, err := divisorValue(o.Divisor)
	if err != nil {
		return nil, err
	}

	if aboveType(t, d) {
		e.Declare(multipleZeroName, multipleZeroDecl)

		return lang.Expr("%s(%s, %s)", multipleZeroName, v, strconv.Quote(o.Divisor)), nil
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
	if constant.Sign(d) <= 0 {
		return nil, errors.New("divisor " + lit + " is not positive")
	}

	// constant holds a float of a huge exponent inexactly, and Num of it
	// is unknown. Above 1 such a divisor exceeds every Go number, which
	// aboveType tells without its digits.
	if constant.Num(d).Kind() != constant.Int &&
		constant.Compare(d, token.LSS, constant.MakeInt64(1)) {
		return nil, fmt.Errorf("divisor %s is out of range", lit)
	}

	return d, nil
}

// aboveType reports whether no value of t but 0 is a multiple of the
// divisor d: d exceeds the greatest finite value of the float type t, or
// the numerator of d in lowest terms exceeds the greatest value of the
// integer type t.
func aboveType(t types.Type, d constant.Value) bool {
	if constant.Num(d).Kind() != constant.Int {
		return true
	}

	if typequery.BasicInfo(t)&types.IsInteger != 0 {
		_, err := literalValue(t, constant.Num(d).ExactString())
		return err != nil
	}

	m := math.MaxFloat64
	if t.Underlying().(*types.Basic).Kind() == types.Float32 {
		m = math.MaxFloat32
	}

	return constant.Compare(d, token.GTR, constant.MakeFloat64(m))
}

const (
	multipleIntName   = "kamaValidateMultipleInt"
	multipleFloatName = "kamaValidateMultipleFloat"
	multipleZeroName  = "kamaValidateMultipleZero"
)

func multipleZeroDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr(
			"func %s[%s ~int | ~int8 | ~int16 | ~int32 | ~int64 |\n"+
				"~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |\n"+
				"~float32 | ~float64](%s %s, %s string) error",
			multipleZeroName, shadow("T"), shadow("got"), ref("T"), shadow("want"),
		),
		lang.If(
			lang.Expr("%s != 0", ref("got")),
			lang.Stmt(`return %s.New(%s.ErrMultiple, "want a multiple of %%s, got %%v", %s, %s)`,
				v, v, ref("want"), ref("got")),
			nil,
		),
		lang.Stmt("return nil"),
	)
}

func multipleIntDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

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

func multipleFloatDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)
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
