package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
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
		decodeDoc += " The value must pass Validate, which rejects a value the schema does not list."
	}

	steps := []genStep{
		lang.Line(validatedDoc(
			typeDoc(doc, ptr, name+" is generated from the schema at #"+ptr+"."),
			declArgs(doc, ptr),
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
			lang.Stmt("raw, err := %s(dec)", q(runtimePath, "ReadValue")),
			lang.If(lang.Expr("err != nil"), lang.Stmt("return err"), nil),
			lang.Stmt("var s %s", name),
			lang.If(
				lang.Expr("err := %s(raw, (*string)(&s)); err != nil", q(runtimePath, "Unmarshal")),
				lang.Stmt("return err"),
				nil,
			),
			validateStep(opts, lang.Expr("s")),
			lang.Stmt("*v = s"),
			lang.Stmt("return nil"),
		),
	)
}

// enumConst names the constant of the value i of the enum at ptr: the name
// of the type followed by the Go name of the value.
func enumConst(doc *jsonschema.Schema, opts Options, ptr string, i int) (string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return "", err
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return "", err
	}

	v, err := identifier(child(ptr, "enum", strconv.Itoa(i)), "enum value", enumString(s.Enum[i]))

	return name + v, err
}

// enumString decodes v, a JSON string stringEnum has checked.
func enumString(v jsontext.Value) string {
	var s string

	_ = jsonv2.Unmarshal(v, &s)

	return s
}
