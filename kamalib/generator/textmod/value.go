package textmod

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"math/big"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/ngicks/go-common/exver"
)

// kindOf returns the kind of lit, whose spec has the type typ or none. A
// typed float spelled as an integer is a float, and a typed integer spelled
// as a float is an integer.
func kindOf(lit literal, typ types.Type) Kind {
	if b, ok := basicOf(typ); ok {
		switch info := b.Info(); {
		case info&types.IsInteger != 0:
			return KindInt
		case info&types.IsFloat != 0:
			return KindFloat
		case info&types.IsString != 0:
			return KindString
		case info&types.IsBoolean != 0:
			return KindBool
		}
	}

	switch {
	case lit.lit == nil:
		return KindBool
	case lit.lit.Kind == token.INT:
		return KindInt
	case lit.lit.Kind == token.FLOAT:
		return KindFloat
	default:
		return KindString
	}
}

func basicOf(typ types.Type) (*types.Basic, bool) {
	if typ == nil {
		return nil, false
	}

	b, ok := typ.Underlying().(*types.Basic)

	return b, ok
}

// modify runs tmpl on data with the current value of lit, and returns the
// literal spelling the result in kind and format. A result equal to the
// current value returns the literal as the source spells it.
func modify(
	tmpl *template.Template,
	lit literal,
	kind Kind,
	typ types.Type,
	format string,
	data Data,
) (string, error) {
	cur, err := current(lit, kind, format)
	if err != nil {
		return "", fmt.Errorf("the current value %s: %w", lit.text, err)
	}

	data.Value = cur.value

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", err
	}

	return spell(b.String(), lit, kind, typ, format, cur)
}

// value is a value of an edited spec: c holds a number or a bool, s a
// string, and value is what a template reads.
type value struct {
	c     constant.Value
	s     string
	value any
}

func current(lit literal, kind Kind, format string) (value, error) {
	switch kind {
	case KindBool:
		if lit.lit != nil {
			return value{}, fmt.Errorf("%s is not a bool", lit.text)
		}

		b := lit.text == "true"

		return value{c: constant.MakeBool(b), value: b}, nil
	case KindString:
		if lit.lit == nil || lit.lit.Kind != token.STRING {
			return value{}, fmt.Errorf("%s is not a string", lit.text)
		}

		s, err := strconv.Unquote(lit.lit.Value)
		if err != nil {
			return value{}, err
		}

		v, err := parseFormatted(s, format)
		if err != nil {
			return value{}, err
		}

		return value{s: s, value: v}, nil
	}

	if lit.lit == nil || lit.lit.Kind == token.STRING {
		return value{}, fmt.Errorf("%s is not a number", lit.text)
	}

	c := constant.MakeFromLiteral(lit.lit.Value, lit.lit.Kind, 0)
	if lit.neg {
		c = constant.UnaryOp(token.SUB, c, 0)
	}

	if kind == KindInt {
		c = constant.ToInt(c)
		if c.Kind() != constant.Int {
			return value{}, fmt.Errorf("%s is not an integer", lit.text)
		}

		if i, ok := constant.Int64Val(c); ok {
			return value{c: c, value: i}, nil
		}

		return value{c: c, value: constant.Val(c).(*big.Int)}, nil
	}

	c = constant.ToFloat(c)
	f, _ := constant.Float64Val(c)

	return value{c: c, value: f}, nil
}

// parseFormatted returns s as a value of format: an exver.Version for ver,
// a time.Time for date-time, s for none.
func parseFormatted(s, format string) (any, error) {
	switch format {
	case FormatVer:
		return exver.Parse(s)
	case FormatDateTime:
		return time.Parse(time.RFC3339Nano, s)
	default:
		return s, nil
	}
}

// spell returns the literal of out, the output of a template, in kind and
// format.
func spell(
	out string,
	lit literal,
	kind Kind,
	typ types.Type,
	format string,
	cur value,
) (string, error) {
	switch kind {
	case KindString:
		s, err := formatted(out, format)
		if err != nil {
			return "", err
		}

		if s == cur.s {
			return lit.text, nil
		}

		// A raw string cannot hold a backquote, and gofmt drops a carriage
		// return from it.
		if strings.HasPrefix(lit.lit.Value, "`") && !strings.ContainsAny(s, "`\r") {
			return "`" + s + "`", nil
		}

		return strconv.Quote(s), nil
	case KindBool:
		s := strings.TrimSpace(out)
		if s != "true" && s != "false" {
			return "", fmt.Errorf("the result %s is not true or false", excerpt(out))
		}

		if s == lit.text {
			return lit.text, nil
		}

		return s, nil
	}

	s := strings.TrimSpace(out)

	c, err := parseNumber(s, kind)
	if err != nil {
		return "", fmt.Errorf("the result %s: %w", excerpt(out), err)
	}

	if err := fits(c, typ); err != nil {
		return "", fmt.Errorf("the result %s: %w", excerpt(out), err)
	}

	if constant.Compare(c, token.EQL, cur.c) {
		return lit.text, nil
	}

	return floatSpelling(s, kind, lit)
}

// floatSpelling returns s, a number of kind, spelled so that the spec of lit
// keeps its kind. An integer literal on a spec spelling no type would make
// the constant an integer, or the var an int, so a decimal one takes .0.
func floatSpelling(s string, kind Kind, lit literal) (string, error) {
	if kind != KindFloat || lit.typed || strings.ContainsAny(s, ".eEpP") {
		return s, nil
	}

	if strings.ContainsAny(strings.TrimLeft(s, "+-"), "xXoObB") {
		return "", fmt.Errorf(
			"the result %s is an integer literal on a float spec spelling no type; spell a float",
			excerpt(s),
		)
	}

	return s + ".0", nil
}

// formatted returns out as a string of format: the String of the version
// for ver, the time spelled in time.RFC3339Nano for date-time, out itself
// for none.
func formatted(out, format string) (string, error) {
	switch format {
	case FormatVer:
		v, err := exver.Parse(strings.TrimSpace(out))
		if err != nil {
			return "", fmt.Errorf("the result %s is not a version: %w", excerpt(out), err)
		}

		return v.String(), nil
	case FormatDateTime:
		t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(out))
		if err != nil {
			return "", fmt.Errorf(
				"the result %s is not an RFC 3339 date-time; end the template with rfc3339: %w",
				excerpt(out), err,
			)
		}

		return t.Format(time.RFC3339Nano), nil
	default:
		return out, nil
	}
}

// parseNumber parses s, a Go integer literal for KindInt, a Go integer or
// float literal for KindFloat, with an optional sign.
func parseNumber(s string, kind Kind) (constant.Value, error) {
	body := strings.TrimLeft(s, "+-")
	if len(s)-len(body) > 1 || body == "" || !strings.ContainsAny(body[:1], "0123456789.") {
		return nil, fmt.Errorf("not a Go %s literal", kind)
	}

	c := constant.MakeFromLiteral(body, token.INT, 0)
	if c.Kind() == constant.Unknown && kind == KindFloat {
		c = constant.MakeFromLiteral(body, token.FLOAT, 0)
	}

	if c.Kind() == constant.Unknown {
		return nil, fmt.Errorf("not a Go %s literal", kind)
	}

	if strings.HasPrefix(s, "-") {
		c = constant.UnaryOp(token.SUB, c, 0)
	}

	return c, nil
}

// fits reports a value c that the type typ cannot hold. An untyped
// constant holds any value. int, uint and uintptr are taken as 64 bits
// wide.
func fits(c constant.Value, typ types.Type) error {
	b, ok := basicOf(typ)
	if !ok || b.Info()&types.IsUntyped != 0 {
		return nil
	}

	switch b.Kind() {
	case types.Float32:
		if f, _ := constant.Float32Val(c); math.IsInf(float64(f), 0) {
			return fmt.Errorf("overflows %s", typ)
		}

		return nil
	case types.Float64:
		if f, _ := constant.Float64Val(c); math.IsInf(f, 0) {
			return fmt.Errorf("overflows %s", typ)
		}

		return nil
	}

	bits, signed := intBits(b.Kind())
	if bits == 0 {
		return nil
	}

	lo, hi := new(big.Int), new(big.Int).Lsh(big.NewInt(1), uint(bits))

	if signed {
		half := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
		lo.Neg(half)
		hi = half
	}

	hi.Sub(hi, big.NewInt(1))

	v, ok := constant.Val(constant.ToInt(c)).(*big.Int)
	if !ok {
		i, _ := constant.Int64Val(c)
		v = big.NewInt(i)
	}

	if v.Cmp(lo) < 0 || v.Cmp(hi) > 0 {
		return fmt.Errorf("overflows %s", typ)
	}

	return nil
}

func intBits(k types.BasicKind) (int, bool) {
	switch k {
	case types.Int8:
		return 8, true
	case types.Int16:
		return 16, true
	case types.Int32:
		return 32, true
	case types.Int, types.Int64:
		return 64, true
	case types.Uint8:
		return 8, false
	case types.Uint16:
		return 16, false
	case types.Uint32:
		return 32, false
	case types.Uint, types.Uint64, types.Uintptr:
		return 64, false
	default:
		return 0, false
	}
}

// excerpt quotes at most 40 bytes of s.
func excerpt(s string) string {
	const limit = 40
	if len(s) > limit {
		return strconv.Quote(s[:limit]) + "..."
	}

	return strconv.Quote(s)
}
