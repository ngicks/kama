package basic

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// Const is the validator //kama:validate const(<v1>, <v2>, ...): the value
// equals one of the listed values, as in const(1, 2, 3) or
// const("red", "dark blue").
//
// It applies to types of a bool, numeric or string underlying type. Each
// arg is a literal of the type: a number, a string or True or False. A
// string type takes strings only, and any other type takes no string, so
// that const("1") and const(1) do not mean the same.
//
// On raw JSON text, as IsRawJson tells, each arg is a JSON value written
// as a literal: None for null, a list for an array and a dict for an
// object, as in const(1, None, "a", {"b": []}). The value equals one of
// them as JSON values do: 1 equals 1.0, and objects equal regardless of
// member order.
type Const struct{}

// Name returns "const".
func (Const) Name() string { return "const" }

// ConstOptions are the options of //kama:validate const(<v1>, ...).
type ConstOptions struct {
	// Values holds one value per arg, in order.
	Values []ConstValue
}

// ConstValue is one arg of const.
type ConstValue struct {
	// Text is the arg as a Go literal without the quotes of a string: a
	// number as written, true or false. It is empty for None, a list and a
	// dict, which no Go type of const takes.
	Text string
	// Quoted reports whether the arg is a string.
	Quoted bool
	// Json is the arg as JSON text, which raw JSON text compares with.
	Json jsontext.Value
}

// Literal returns the Go literal v spells: Text quoted again when it was
// written quoted.
func (v ConstValue) Literal() string {
	if v.Quoted {
		return strconv.Quote(v.Text)
	}

	return v.Text
}

// Parse decodes the args of c into one or more values. Check tells whether
// they are values of the type.
func (Const) Parse(c expr.Call, r ext.Resolver) (ext.Rule, error) {
	values := make([]ConstValue, 0, len(c.Args))

	for _, a := range c.Args {
		if a.Name != "" {
			r.Warn(fmt.Errorf("const: unknown arg %s; const takes values only", a))
			continue
		}

		j, err := expr.Json(a.Value)
		if err != nil {
			return nil, argsError(fmt.Errorf("const: %s is no literal: %w", a.Value, err))
		}

		v := ConstValue{Json: j}

		switch x := a.Value.(type) {
		case expr.String:
			v.Text, v.Quoted = x.Value, true
		case expr.Number, expr.Bool:
			v.Text, _ = goLiteral(x)
		}

		values = append(values, v)
	}

	if len(values) == 0 {
		return nil, argsError(errors.New("want one or more values, as in const(1, 2)"))
	}

	return ConstOptions{Values: values}, nil
}

// Check accepts values of t, on the types the doc comment of Const lists.
func (o ConstOptions) Check(t types.Type) error {
	if ext.IsRawJson(t) {
		return nil
	}

	info := typequery.BasicInfo(t)
	if info&(types.IsBoolean|types.IsNumeric|types.IsString) == 0 {
		return fmt.Errorf("%s is not a bool, numeric or string type", t)
	}

	for _, c := range o.Values {
		if c.Text == "" && !c.Quoted {
			return fmt.Errorf("%s is no value of %s", c.Json, t)
		}

		if info&types.IsString != 0 && !c.Quoted {
			return fmt.Errorf(
				"%s is no value of %s; want a quoted string, as in %q", c.Text, t, c.Text,
			)
		}

		if _, err := literalValue(t, c.Literal()); err != nil {
			return err
		}
	}

	return nil
}

// Emit calls kamaValidateConst with the values.
func (o ConstOptions) Emit(e ext.Emitter, t types.Type, v ext.Gen) (ext.Gen, error) {
	if ext.IsRawJson(t) {
		texts := opiter.WrapSeq(slices.Values(o.Values)).
			Map(func(c ConstValue) ext.Gen { return lang.Expr("%s", strconv.Quote(string(c.Json))) }).
			CollectSlice()

		return rawCall(e, "Const", v, texts...), nil
	}

	e.Declare(constName, constDecl)
	e.Declare(spellName, spellDecl)

	values := opiter.WrapSeq(slices.Values(o.Values)).
		Map(ConstValue.Literal).
		CollectSlice()

	return lang.Expr("%s(%s, %s)", constName, v, strings.Join(values, ", ")), nil
}

const constName = "kamaValidateConst"

func constDecl(imp ext.Importer) ext.Step {
	v := imp.Import(ext.ValidationPath)

	return lang.Func(
		lang.Expr("func %s[%s comparable](%s %s, %s ...%s) error",
			constName, shadow("T"), shadow("got"), ref("T"), shadow("want"), ref("T")),
		lang.If(
			lang.Expr("!%s.Contains(%s, %s)", imp.Import("slices"), ref("want"), ref("got")),
			lang.Steps(
				lang.Stmt("%s := make([]string, len(%s))", shadow("spelled"), ref("want")),
				// For renders its clause in the scope of the loop, which the
				// range variables are declared in.
				lang.For(nil,
					lang.Expr("%s, %s := range %s", shadow("i"), shadow("w"), ref("want")),
					nil,
					lang.Stmt("%s[%s] = %s(%s)", ref("spelled"), ref("i"), spellName, ref("w")),
				),
				lang.Stmt(
					"return %s.New(%s.ErrConst, \"want one of %%s, got %%s\",\n%s.Join(%s, \", \"), %s(%s))",
					v,
					v,
					imp.Import("strings"),
					ref("spelled"),
					spellName,
					ref("got"),
				),
			),
			nil,
		),
		lang.Stmt("return nil"),
	)
}
