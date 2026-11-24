package rule

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

// Const is the validator //kama:validate const="<v1> <v2> ...": the value
// equals one of the listed values, as in const="1 2 3" or
// const=`"red" "dark blue"`.
//
// It applies to types of a bool, numeric or string underlying type. Each
// token of the argument is a Go literal of the type. A string type takes
// quoted strings only, and any other type takes bare literals only, so
// that const=`"1"` and const=1 do not mean the same.
//
// On raw JSON text, as IsRawJson tells, each token is the text of a JSON
// value, quoted or not, as in const="1 null `\"a\"` `{\"b\":[]}`". The value
// equals one of them as JSON values do: 1 equals 1.0, and objects equal
// regardless of member order.
type Const struct{}

// Name returns "const".
func (Const) Name() string { return "const" }

// ConstOptions are the options of //kama:validate const="<v1> <v2> ...".
type ConstOptions struct {
	// Values holds one value per token of the arg, in order.
	Values []ConstValue
}

// ConstValue is one token of the arg of const.
type ConstValue struct {
	// Text is the token without the quotes it was written with.
	Text string
	// Quoted reports whether the token was written quoted.
	Quoted bool
}

// Literal returns the Go literal v spells: Text quoted again when it was
// written quoted.
func (v ConstValue) Literal() string {
	if v.Quoted {
		return strconv.Quote(v.Text)
	}

	return v.Text
}

// Parse decodes arg into one or more values. Check tells whether they are
// values of the type.
func (Const) Parse(arg string, _ Resolver) (Rule, error) {
	args, err := argTokens(arg)
	if err != nil {
		return nil, err
	}

	if len(args) == 0 {
		return nil, argsError(errors.New("want one or more values"))
	}

	values := make([]ConstValue, 0, len(args))

	for _, kv := range args {
		if kv.HasValue {
			return nil, argsError(fmt.Errorf(
				"arg %q is a key=value pair; quote a value holding =",
				kv.String(),
			))
		}

		values = append(values, ConstValue{Text: kv.Key, Quoted: kv.KeyQuoted})
	}

	return ConstOptions{Values: values}, nil
}

// Check accepts values of t, on the types the doc comment of Const lists.
func (o ConstOptions) Check(t types.Type) error {
	if IsRawJson(t) {
		for _, c := range o.Values {
			if !jsontext.Value(c.Text).IsValid() {
				return fmt.Errorf("%s is no JSON value, which raw JSON text compares with", c.Text)
			}
		}

		return nil
	}

	info := typequery.BasicInfo(t)
	if info&(types.IsBoolean|types.IsNumeric|types.IsString) == 0 {
		return fmt.Errorf("%s is not a bool, numeric or string type", t)
	}

	for _, c := range o.Values {
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
func (o ConstOptions) Emit(e Emitter, t types.Type, v Gen) (Gen, error) {
	if IsRawJson(t) {
		texts := opiter.WrapSeq(slices.Values(o.Values)).
			Map(func(c ConstValue) Gen { return lang.Expr("%s", strconv.Quote(c.Text)) }).
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

func constDecl(imp Importer) Step {
	v := imp.Import(ValidationPath)

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
