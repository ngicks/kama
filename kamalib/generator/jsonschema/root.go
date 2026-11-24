package jsonschema

import (
	"strings"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// rootSteps returns the declarations of the root of doc when the root
// declares no Go type of its own: a type defined over the Go type of the
// root, encoding as it and decoding as it checks the const or the enum of
// the root.
//
// The root needs a name to be decoded into, while a schema elsewhere in
// the document is spelled by a built-in type. The defined type loses the
// methods of the Go type, such as those of opt.Option, so its own methods
// convert to it.
func rootSteps(doc *jsonschema.Schema, opts Options) []genStep {
	name, err := typeName(doc, opts, "")
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	allowed, err := allowedValues(doc, "")
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	underlying := rootType(doc, opts)

	decodeDoc := "UnmarshalJSONFrom decodes v as its underlying type."
	decode := []genStep{
		lang.Stmt("raw, err := %s(dec)", q(runtimePath, "ReadValue")),
		lang.If(lang.Expr("err != nil"), lang.Stmt("return err"), nil),
		lang.Stmt("var out %s", underlying),
		lang.If(
			lang.Expr("err := %s(raw, &out); err != nil", q(runtimePath, "Unmarshal")),
			lang.Stmt("return err"),
			nil,
		),
	}

	if allowed != nil {
		decodeDoc += " It rejects a value the schema does not allow."

		decode = append(decode, lang.If(
			lang.Expr(
				"err := %s(out, %s); err != nil",
				q(runtimePath, "Enum"),
				strings.Join(allowed, ", "),
			),
			lang.Stmt("return err"),
			nil,
		))
	}

	return []genStep{
		lang.Line(docComment(typeDoc(doc, "", name+" is generated from the schema at #."))),
		lang.Stmt("type %s %s", name, underlying),
		lang.DocFunc(
			docComment("MarshalJSONTo encodes v as its underlying type."),
			lang.Expr(
				"func (v %s) MarshalJSONTo(enc *%s) error", name, q(jsontextPath, "Encoder"),
			),
			lang.Stmt("return %s(enc, %s(v))", q(jsonPath, "MarshalEncode"), underlying),
		),
		lang.DocFunc(
			docComment(decodeDoc),
			lang.Expr(
				"func (v *%s) UnmarshalJSONFrom(dec *%s) error", name, q(jsontextPath, "Decoder"),
			),
			append(decode, lang.Stmt("*v = %s(out)", name), lang.Stmt("return nil"))...,
		),
	}
}

// rootType spells the Go type of the root of doc as a value of its own:
// the type goType spells, in an opt.Option when the root is nullable.
func rootType(doc *jsonschema.Schema, opts Options) genExpr {
	return func(c genCtx) (string, error) {
		t, err := goType(doc, opts, "")(c)
		if err != nil || !nullable(doc, "") {
			return t, err
		}

		return lang.Expr("%s[%s]", q(optPath, "Option"), t)(c)
	}
}
