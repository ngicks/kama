package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// enumSteps returns the declarations of the enum of strings at ptr: the
// string type, whose directive checks the values and the other keywords of
// a string, a constant per value and UnmarshalJSONFrom running Validate.
func enumSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	consts, err := opiter.TryCollect(
		opiter.WrapSeq(opiter.Range(0, len(s.Enum))).
			Disassemble(func(i int) (string, error) { return enumConst(doc, opts, ptr, i) }),
	)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	decodeDoc := "UnmarshalJSONFrom decodes v from one JSON string."
	if opts.Validate {
		decodeDoc += " The value must pass ValidateShallow, which rejects a value the schema " +
			"does not list."
	}

	steps := []genStep{
		lang.Line(validatedDoc(
			typeDoc(doc, ptr, name+" is generated from the schema at #"+ptr+"."),
			decodedArgs(opts, declArgs(doc, opts, ptr)),
		)),
		lang.Stmt("type %s string", name),
		lang.Line(docComment(name + " values the schema allows.")),
		lang.Stmt("const ("),
	}

	for i, v := range s.Enum {
		steps = append(
			steps,
			lang.Stmt("%s %s = %s", consts[i], name, strconv.Quote(enumString(v))),
		)
	}

	return append(steps,
		lang.Stmt(")"),
		lang.DocFunc(
			docComment(decodeDoc),
			lang.Expr(
				"func (v *%s) UnmarshalJSONFrom(dec *%s) error",
				name,
				q(jsontextPath, "Decoder"),
			),
			lang.Stmt("var w %s[string]", q(wirePath, "NonNull")),
			lang.If(
				lang.Expr("err := kamaJsonschemaDecode(dec, &w); err != nil"),
				lang.Stmt("return err"),
				nil,
			),
			lang.Stmt("str, err := w.Value()"),
			lang.If(lang.Expr("err != nil"), lang.Stmt("return err"), nil),
			lang.Stmt("s := %s(str)", name),
			validateStep(opts, lang.Expr("s")),
			lang.Stmt("*v = s"),
			lang.Stmt("return nil"),
		),
	)
}

// enumConst names the constant of the value i of the enum at ptr: the name
// of the type followed by the Go name of the value. A value giving no Go
// name takes "Value" followed by its 1-based index. A name an earlier value
// of the enum already takes is followed by the least number from 2 up that
// makes it distinct, so names follow the source order.
func enumConst(doc *jsonschema.Schema, opts Options, ptr string, i int) (string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return "", err
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return "", err
	}

	var taken []string

	for j := 0; j <= i; j++ {
		v, ok := goName(enumString(s.Enum[j]))
		if !ok {
			v = "Value" + strconv.Itoa(j+1)
		}

		unique := v
		for n := 2; slices.Contains(taken, unique); n++ {
			unique = v + strconv.Itoa(n)
		}

		taken = append(taken, unique)
	}

	return name + taken[i], nil
}

// enumString decodes v, a JSON string stringEnum has checked.
func enumString(v jsontext.Value) string {
	var s string

	_ = jsonv2.Unmarshal(v, &s)

	return s
}
