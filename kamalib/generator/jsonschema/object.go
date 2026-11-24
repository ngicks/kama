package jsonschema

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// objectSteps returns the declarations of the struct of the object at ptr,
// or of the product of its allOf: the struct, its MarshalJSONTo where the
// default encoding falls short, the wire struct and UnmarshalJSONFrom.
func objectSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	parts, err := productParts(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	lead := name + " is generated from the schema at #" + ptr + "."
	if s.AllOf != nil {
		lead = name + " is all of " + parts + ". It is generated from the schema at #" + ptr + "."
	}

	steps := []genStep{
		lang.Line(docComment(typeDoc(doc, ptr, lead))),
		lang.Stmt("type %s struct {", name),
	}

	for i := range s.AllOf {
		steps = append(steps, lang.Stmt(
			"%s %s `json:\"-\"`",
			memberField(doc, opts, ptr, i),
			goType(doc, opts, child(ptr, "allOf", strconv.Itoa(i))),
		))
	}

	for _, n := range s.Properties {
		pp := child(ptr, "properties", n.Name)

		if a := annotations(n.Schema); len(a) > 0 {
			steps = append(steps, lang.Line(lang.Comment(
				lang.Expr("%s", commentLines(strings.Join(a, "\n\n"))),
			)))
		}

		steps = append(steps, lang.Stmt(
			"%s %s `json:%s`",
			fieldName(pp, n.Name), fieldType(doc, opts, ptr, n.Name), tagFor(doc, ptr, n.Name),
		))
	}

	if extraMembers(s) {
		steps = append(steps, lang.Stmt(
			"AdditionalProperties map[string]%s `json:\",embed\"`",
			elemType(doc, opts, child(ptr, "additionalProperties")),
		))
	}

	steps = append(steps, lang.Stmt("}"))
	steps = append(steps, marshalSteps(doc, opts, ptr)...)
	steps = append(steps, wireSteps(doc, opts, ptr)...)

	return append(steps, unmarshalSteps(doc, opts, ptr)...)
}

// productParts names what the product at ptr is made of: its members
// separated by commas, then "and its own properties" when it declares
// any. It is empty for a struct.
func productParts(doc *jsonschema.Schema, opts Options, ptr string) (string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return "", err
	}

	names, err := opiter.TryCollect(
		opiter.WrapSeq(opiter.Range(0, len(s.AllOf))).
			Disassemble(func(i int) (string, error) { return memberName(doc, opts, ptr, i) }),
	)
	if err != nil {
		return "", err
	}

	text := strings.Join(names, ", ")
	if s.AllOf != nil && s.Properties != nil {
		text += " and its own properties"
	}

	return text, nil
}

// marshalSteps returns MarshalJSONTo of the struct or product at ptr. A
// product writes its members and own properties into one object. A struct
// needs one only to reject a nil pointer of a required member, which a
// product rejects too.
func marshalSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	sig := lang.Expr("func (v %s) MarshalJSONTo(enc *%s) error", name, q(jsontextPath, "Encoder"))
	nilText := "A nil pointer of a required member is an error."
	body := nilChecks(doc, ptr)

	if s.AllOf != nil {
		parts, err := productParts(doc, opts, ptr)
		if err != nil {
			return []genStep{lang.Fail(err)}
		}

		text := "MarshalJSONTo encodes v as one JSON object holding the members of " + parts +
			". The first of them to hold a member name writes it."
		if body != nil {
			text += " " + nilText
		}

		values := opiter.WrapSeq(opiter.Range(0, len(s.AllOf))).
			Map(func(i int) genExpr { return lang.Expr("v.%s", memberField(doc, opts, ptr, i)) }).
			CollectSlice()

		if s.Properties != nil {
			body = append(body, lang.Stmt("type plain %s", name))
			values = append(values, lang.Expr("plain(v)"))
		}

		body = append(body, lang.Stmt(
			"return %s(enc, %s)", q(runtimePath, "MarshalObjects"), lang.Join(", ", values...),
		))

		return []genStep{lang.DocFunc(docComment(text), sig, body...)}
	}

	if body == nil {
		return nil
	}

	body = append(body,
		lang.Stmt("type plain %s", name),
		lang.Stmt("return %s(enc, plain(v))", q(jsonPath, "MarshalEncode")),
	)

	return []genStep{lang.DocFunc(
		docComment("MarshalJSONTo encodes v as one JSON object. "+nilText),
		sig,
		body...,
	)}
}

// nilChecks returns the statements of MarshalJSONTo of the struct or
// product at ptr that reject a nil pointer of a required member, or nil
// when no required member is a pointer.
func nilChecks(doc *jsonschema.Schema, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	var body []genStep

	for _, n := range s.Properties {
		if !required(doc, ptr, n.Name) || nullable(doc, child(ptr, "properties", n.Name)) ||
			!onCycle(doc, ptr, child(ptr, "properties", n.Name)) {
			continue
		}

		body = append(body, lang.If(
			lang.Expr("v.%s == nil", fieldName(child(ptr, "properties", n.Name), n.Name)),
			lang.Stmt(
				"return %s(%s(%s, \"required member is nil\"), %s)",
				q(validPath, "Field"), q(validPath, "New"), q(validPath, "ErrNil"),
				strconv.Quote(n.Name),
			),
			nil,
		))
	}

	return body
}

// wireSteps returns the struct the object at ptr decodes through: every
// property as an und.Und, or as raw text where its Go type holds null
// itself, then the members the object rejects or keeps.
func wireSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	steps := []genStep{
		lang.Line(docComment(wireName(name) + " keeps a missing member of " + name +
			" apart from null.")),
		lang.Stmt("type %s struct {", wireName(name)),
	}

	for _, n := range s.Properties {
		pp := child(ptr, "properties", n.Name)

		t := lang.Expr("%s[%s]", q(undPath, "Und"), goType(doc, opts, pp))
		if acceptsNull(doc, pp) {
			t = q(jsontextPath, "Value")
		}

		steps = append(steps, lang.Stmt(
			"%s %s `json:%s`", fieldName(pp, n.Name), t, strconv.Quote(n.Name+",omitzero"),
		))
	}

	switch {
	case closed(s):
		steps = append(steps, lang.Stmt(
			"Unknown map[string]%s `json:\",embed\"`", q(jsontextPath, "Value"),
		))
	case extraMembers(s):
		steps = append(steps, lang.Stmt(
			"AdditionalProperties map[string]%s `json:\",embed\"`",
			elemType(doc, opts, child(ptr, "additionalProperties")),
		))
	}

	return append(steps, lang.Stmt("}"))
}

// unmarshalSteps returns UnmarshalJSONFrom of the struct or product at ptr.
func unmarshalSteps(doc *jsonschema.Schema, opts Options, ptr string) []genStep {
	s, err := at(doc, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	name, err := typeName(doc, opts, ptr)
	if err != nil {
		return []genStep{lang.Fail(err)}
	}

	text := "UnmarshalJSONFrom decodes v from one JSON object. "

	if s.AllOf != nil {
		parts, err := productParts(doc, opts, ptr)
		if err != nil {
			return []genStep{lang.Fail(err)}
		}

		text += strings.Replace(parts, "its own", "the own", 1) + " decode from the same object. "
	}

	text += "A member that fails to decode stops it. Otherwise it reports every member the " +
		"schema rejects."

	body := []genStep{
		lang.Stmt("raw, err := %s(dec)", q(runtimePath, "ReadValue")),
		lang.If(lang.Expr("err != nil"), lang.Stmt("return err"), nil),
		lang.Stmt("var w %s", wireName(name)),
		lang.If(
			lang.Expr("err := %s(raw, &w); err != nil", q(runtimePath, "UnmarshalObject")),
			lang.Stmt("return err"),
			nil,
		),
		lang.Stmt("var out %s", name),
		lang.Stmt("var errs []error"),
	}

	for i := range s.AllOf {
		body = append(body, lang.Stmt(
			"errs = append(errs, %s(raw, &out.%s))",
			q(runtimePath, "Unmarshal"), memberField(doc, opts, ptr, i),
		))
	}

	for _, n := range s.Properties {
		body = append(body, decodeSteps(doc, opts, ptr, n.Name)...)
	}

	switch {
	case closed(s):
		body = append(body, lang.Stmt(
			"errs = append(errs, %s(w.Unknown))", q(runtimePath, "RejectUnknown"),
		))
	case extraMembers(s):
		body = append(body, lang.Stmt("out.AdditionalProperties = w.AdditionalProperties"))
	}

	body = append(body,
		lang.If(
			lang.Expr("err := %s(errs...); err != nil", q(validPath, "Join")),
			lang.Stmt("return err"),
			nil,
		),
		lang.Stmt("*v = out"),
		lang.Stmt("return nil"),
	)

	return []genStep{
		lang.DocFunc(
			docComment(text),
			lang.Expr(
				"func (v *%s) UnmarshalJSONFrom(dec *%s) error",
				name,
				q(jsontextPath, "Decoder"),
			),
			body...,
		),
	}
}

// decodeSteps returns the statements converting the wire field of the
// property name of the object at ptr into its field, then checking the
// value against an enum or a const of scalars.
func decodeSteps(doc *jsonschema.Schema, opts Options, ptr, name string) []genStep {
	pp := child(ptr, "properties", name)
	f := fieldName(pp, name)
	req, null := required(doc, ptr, name), nullable(doc, pp)

	collect := lang.Stmt(
		"errs = append(errs, %s(err, %s))",
		q(validPath, "Field"),
		strconv.Quote(name),
	)
	conv := func(fn string) []genStep {
		return []genStep{
			lang.Stmt("out.%s, err = %s(w.%s)", f, q(runtimePath, fn), f),
			collect,
		}
	}

	var steps []genStep

	switch {
	case acceptsNull(doc, pp):
		fn := "DecodeOptional"
		if req {
			fn = "DecodeRequired"
		}

		steps = []genStep{
			lang.Stmt(
				"out.%s, err = %s[%s](w.%s)",
				f,
				q(runtimePath, fn),
				goType(doc, opts, pp),
				f,
			),
			collect,
		}
	case onCycle(doc, ptr, pp):
		switch {
		case req && null:
			steps = conv("RequiredNullablePointer")
		case req:
			steps = conv("RequiredPointer")
		case null:
			steps = []genStep{lang.Stmt(
				"out.%s = %s(w.%s)", f, q(runtimePath, "OptionalNullablePointer"), f,
			)}
		default:
			steps = conv("OptionalPointer")
		}
	case req && null:
		steps = conv("RequiredNullable")
	case req:
		steps = conv("Required")
	case null:
		steps = []genStep{lang.Stmt("out.%s = w.%s", f, f)}
	default:
		steps = conv("Optional")
	}

	enum, err := enumCheck(
		doc,
		pp,
		lang.Expr("w.%s.Value()", f),
		lang.Expr("%s(raw, %s)", q(runtimePath, "Member"), strconv.Quote(name)),
	)
	if err != nil {
		return append(steps, lang.Fail(err))
	}

	if enum == nil {
		return steps
	}

	// IsDefined skips null, which a nullable value admits besides the enum.
	return append(steps, lang.If(
		lang.Expr("w.%s.IsDefined()", f),
		lang.Stmt(
			"errs = append(errs, %s(%s, %s))",
			q(validPath, "Field"),
			enum,
			strconv.Quote(name),
		),
		nil,
	))
}

// enumCheck returns the call checking a value of the schema at ptr against
// its const or its enum of scalars, or nil when it has neither. value
// spells the Go value and raw its JSON text. A number compares by raw,
// since its float64 rounds a value beyond 2^53.
func enumCheck(doc *jsonschema.Schema, ptr string, value, raw genExpr) (genExpr, error) {
	allowed, err := allowedValues(doc, ptr)
	if err != nil || allowed == nil {
		return nil, err
	}

	r, _, err := resolvedValue(doc, ptr)
	if err != nil {
		return nil, err
	}

	k, err := kinds(doc, r)
	if err != nil {
		return nil, err
	}

	if slices.Equal(k, []string{"number"}) {
		return lang.Expr(
			"%s(%s, %s)", q(runtimePath, "NumberEnum"), raw, joinComma(quoteAll(allowed)),
		), nil
	}

	return lang.Expr("%s(%s, %s)", q(runtimePath, "Enum"), value, joinComma(allowed)), nil
}

// allowedValues returns the Go literals of the const or the enum of scalars
// the schema at ptr restricts its value to, or nil when it has neither. It
// reads the value of a nullable union. An enum of strings declares a type
// that checks itself.
func allowedValues(doc *jsonschema.Schema, ptr string) ([]string, error) {
	r, s, err := resolvedValue(doc, ptr)
	if err != nil {
		return nil, err
	}

	values := s.Enum
	if s.Const != nil {
		values = []jsontext.Value{s.Const}
	}

	if values == nil || (s.Const == nil && stringEnum(s)) {
		return nil, nil
	}

	k, err := kinds(doc, r)
	if err != nil {
		return nil, err
	}

	if len(k) != 1 {
		return nil, unsupported(r, "enum or const of more than one kind")
	}

	return opiter.TryCollect(
		opiter.WrapSeq(slices.Values(values)).
			Disassemble(func(v jsontext.Value) (string, error) { return goLiteral(r, k[0], v) }),
	)
}

// goLiteral spells v as a Go constant of the Go type of the JSON type kind.
func goLiteral(ptr, kind string, v jsontext.Value) (string, error) {
	text := string(v)
	ok := false

	switch kind {
	case "string":
		var s string
		if v.Kind() == '"' && jsonv2.Unmarshal(v, &s) == nil {
			return strconv.Quote(s), nil
		}
	case "integer":
		_, err := strconv.ParseInt(text, 10, 64)
		ok = v.Kind() == '0' && err == nil
	case "number":
		_, err := strconv.ParseFloat(text, 64)
		ok = v.Kind() == '0' && err == nil
	case "boolean":
		ok = v.Kind() == 't' || v.Kind() == 'f'
	}

	if !ok {
		return "", unsupported(ptr, "value %s is no Go constant for the JSON type %s", text, kind)
	}

	return text, nil
}

// fieldName returns the Go name of the property name at ptr. check has
// rejected a name giving none.
func fieldName(ptr, name string) genExpr {
	return func(genCtx) (string, error) { return identifier(ptr, "property", name) }
}

// memberField returns the name of the field of the member i of the allOf
// at ptr.
func memberField(doc *jsonschema.Schema, opts Options, ptr string, i int) genExpr {
	return func(genCtx) (string, error) { return memberName(doc, opts, ptr, i) }
}

// fieldType spells the type of the field of the property name of the
// object at ptr: the required x nullable matrix, a pointer on a cycle, and
// the Go type itself where it holds null.
func fieldType(doc *jsonschema.Schema, opts Options, ptr, name string) genExpr {
	pp := child(ptr, "properties", name)
	t := goType(doc, opts, pp)
	req := required(doc, ptr, name)

	switch {
	case acceptsNull(doc, pp):
		if req {
			return t
		}
	case onCycle(doc, ptr, pp):
		return lang.Expr("*%s", t)
	case !req && nullable(doc, pp):
		return lang.Expr("%s[%s]", q(undPath, "Und"), t)
	case req && !nullable(doc, pp):
		return t
	}

	return lang.Expr("%s[%s]", q(optPath, "Option"), t)
}

// tagFor returns the json tag of the field of the property name of the
// object at ptr. An optional member is omitted when zero.
func tagFor(doc *jsonschema.Schema, ptr, name string) string {
	if required(doc, ptr, name) {
		return strconv.Quote(name)
	}

	return strconv.Quote(name + ",omitzero")
}
